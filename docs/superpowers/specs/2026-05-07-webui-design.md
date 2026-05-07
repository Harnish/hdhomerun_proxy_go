# Web UI Design Spec

**Date:** 2026-05-07
**Scope:** Add an opt-in embedded web UI (`-webui` flag) for configuration and live monitoring.

---

## Overview

Running `./hdhomerun_proxy -webui :8080 -webui-user admin -webui-pass secret app` (or `tuner`) starts the proxy with an embedded HTTP server. The web UI provides two tabs: **Status** (live stats + log) and **Config** (editable config form with live reload). Without `-webui`, behavior is unchanged.

---

## Flags

| Flag | Required | Description |
|------|----------|-------------|
| `-webui <addr>` | opt-in | Bind address for the web UI (e.g. `:8080`) |
| `-webui-user <user>` | if `-webui` set | HTTP Basic Auth username |
| `-webui-pass <pass>` | if `-webui` set | HTTP Basic Auth password |

If `-webui` is set without both `-webui-user` and `-webui-pass`, the proxy prints an error and exits at startup.

---

## Architecture

### New files

**`webui.go`** (`package main`)

Starts and manages the embedded HTTP server. Registers all routes. Implements Basic Auth middleware. Implements JSON handlers.

```go
type webServer struct {
    store  *configStore
    router statsProvider
}

func newWebServer(store *configStore, router statsProvider) *webServer

func (ws *webServer) start(ctx context.Context, addr, user, pass string) error
func (ws *webServer) basicAuth(user, pass string, next http.HandlerFunc) http.HandlerFunc

// Handlers
func (ws *webServer) handleStats(w http.ResponseWriter, r *http.Request)   // GET /api/stats
func (ws *webServer) handleLogs(w http.ResponseWriter, r *http.Request)    // GET /api/logs
func (ws *webServer) handleGetConfig(w http.ResponseWriter, r *http.Request)  // GET /api/config
func (ws *webServer) handlePostConfig(w http.ResponseWriter, r *http.Request) // POST /api/config
```

**`web/index.html`** (embedded via `//go:embed web/index.html`)

Single-page HTML with vanilla JS. Two tabs rendered client-side. No build step.

### Modified files

**`log_handler.go`**

Move the log ring buffer out of `tuiModel` into a package-level `logRingBuf` (mutex-guarded):

```go
const logRingBufCap = 200

var (
    logRingMu  sync.Mutex
    logRingBuf []logEntry // cap logRingBufCap
)

func appendLogEntry(e logEntry) {
    logRingMu.Lock()
    defer logRingMu.Unlock()
    logRingBuf = append(logRingBuf, e)
    if len(logRingBuf) > logRingBufCap {
        logRingBuf = logRingBuf[len(logRingBuf)-logRingBufCap:]
    }
}

func getLogEntries() []logEntry {
    logRingMu.Lock()
    defer logRingMu.Unlock()
    out := make([]logEntry, len(logRingBuf))
    copy(out, logRingBuf)
    return out
}
```

`tuiHandler.Handle()` calls `appendLogEntry(e)` in addition to sending `logMsg` to Bubble Tea.
`tui.go`'s `tuiModel` no longer maintains its own `logBuf`; it reads from `getLogEntries()` on each `logMsg` or `tickMsg` instead.

**`config.go`**

Add `configStore` — a mutex-protected live config with file path:

```go
type configStore struct {
    mu       sync.RWMutex
    cfg      *Config
    filePath string // empty string if no file was specified at startup
}

func newConfigStore(cfg *Config, filePath string) *configStore

func (cs *configStore) Get() *Config
func (cs *configStore) Set(newCfg *Config) error  // writes to disk (if filePath != ""), then updates in-memory
func (cs *configStore) ApplyLive(newCfg *Config)  // applies immediately-applicable fields
```

`ApplyLive` applies these fields without restart: `Debug` (updates slog default level), `LogActiveConnectionsInterval`, `Tunarr.HttpTimeout`, `Tunarr.Host`, `Tunarr.Port`, `Tunarr.Enabled`, `Tunarr.UseTunarrOnly`.

Fields that require restart: `HDHomeRunPort`, `TCPPort`, `UDPReadTimeout`, `UDPReadBuffSize`, `ReconnectInterval`, `App.BindAddress`, `App.DirectHDHRIP`, `Tuner.ProxyHost`, `Tuner.DirectMode`, `Tuner.DirectHDHRIP`.

**`main.go`**

- Add three flags: `-webui`, `-webui-user`, `-webui-pass`
- Validate: if `-webui` set, both `-webui-user` and `-webui-pass` must be non-empty
- Pass `configStore` and proxy (as `statsProvider`) to `runAppProxy` / `runTunerProxy`
- Start web server in a goroutine after proxy is initialized (before `proxy.Run()`)

**`app_proxy.go` / `tuner_proxy.go`**

Replace direct `*Config` usage with `*configStore`. Each call that reads config calls `store.Get()`.

---

## API Endpoints

All endpoints require HTTP Basic Auth.

| Method | Path | Request | Response |
|--------|------|---------|----------|
| GET | `/` | — | `index.html` |
| GET | `/api/stats` | — | `ProxyStats` JSON |
| GET | `/api/logs` | — | `[]logEntry` JSON (last 200) |
| GET | `/api/config` | — | `Config` JSON |
| POST | `/api/config` | `Config` JSON body | `{"ok":true}` or `{"error":"..."}` |

`logEntry` JSON shape:
```json
{ "time": "15:04:05", "level": "INFO", "msg": "...", "attrs": "key=value ..." }
```

Note: `logEntry`'s fields are currently unexported. They must be exported (with `json:` tags) or a separate `logEntryJSON` DTO used for serialization. Either approach is acceptable; exporting the fields is simpler.

---

## Web UI Pages

### Status Tab

- **CONNECTIONS** panel: UDP, Dial, Total counts. Polled every 1s via `GET /api/stats`.
- **BACKENDS** panel: one row per configured backend (green dot + label + address). Same visibility rules as TUI (hidden if not configured).
- **LOG** panel: scrollable table of last 200 entries. Columns: timestamp, level (color-coded), message + attrs. "Show DEBUG" checkbox filters debug lines client-side. Polled every 1s via `GET /api/logs`.

### Config Tab

Five collapsible sections matching the `Config` struct:

| Section | Fields |
|---------|--------|
| Network | `hdhomerun_port`, `tcp_port`, `udp_read_timeout_ms`, `udp_read_buffer_size`, `reconnect_interval_seconds` |
| Logging | `debug` (checkbox), `log_active_connections_interval_seconds` |
| App Proxy | `app.bind_address`, `app.direct_hdhomerun_ip` |
| Tuner Proxy | `tuner.app_proxy_host`, `tuner.direct_mode` (checkbox), `tuner.direct_hdhomerun_ip` |
| Tunarr | `tunarr.enabled` (checkbox), `tunarr.host`, `tunarr.port`, `tunarr.use_tunarr_only` (checkbox), `tunarr.http_timeout_seconds` |

Fields marked `⚠ restart required` are those not in the live-reload list above.

A **Save** button POSTs the full config JSON. On success: green toast "Config saved". On error: red toast with the error message. If the config file path was not set at startup, a yellow warning banner reads "No config file path set — changes apply in-memory only and will be lost on restart."

---

## Authentication

HTTP Basic Auth on every request (including `/`). The browser's native auth dialog handles credential entry. No session tokens or cookies — stateless per-request auth.

Password comparison uses `subtle.ConstantTimeCompare` to prevent timing attacks.

---

## File Map

| File | Change |
|------|--------|
| `webui.go` | **Create** — HTTP server, routes, Basic Auth, JSON handlers |
| `web/index.html` | **Create** — single-page tabbed UI with vanilla JS |
| `log_handler.go` | **Modify** — add package-level ring buffer (`logRingBuf`) + `appendLogEntry` / `getLogEntries` |
| `tui.go` | **Modify** — remove `logBuf` field; read from `getLogEntries()` |
| `config.go` | **Modify** — add `configStore` struct with `Get` / `Set` / `ApplyLive` |
| `app_proxy.go` | **Modify** — accept `*configStore`, call `store.Get()` |
| `tuner_proxy.go` | **Modify** — accept `*configStore`, call `store.Get()` |
| `main.go` | **Modify** — add three flags, validate, wire `configStore` + web server |

---

## Constraints

- No new Go dependencies — `net/http`, `encoding/json`, `crypto/subtle` are all stdlib
- No JS build step — vanilla JS only in `web/index.html`
- `-webui` is independent of `-tui`; both can run simultaneously
- Existing proxy behavior is completely unchanged without `-webui`
