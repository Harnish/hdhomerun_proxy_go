# TUI Design Spec

**Date:** 2026-05-06
**Scope:** Add an opt-in terminal UI (`-tui` flag) that displays proxy stats and a scrolling log pane using Bubble Tea.

---

## Overview

Running `./hdhomerun_proxy -tui app` (or `tuner`) launches the proxy with a full-terminal dashboard instead of raw log output. Without `-tui`, behavior is unchanged.

---

## Layout

Sidebar (fixed ~24 chars wide) on the left, scrolling log pane on the right — both full terminal height, separated by a lipgloss border.

### Sidebar sections (top to bottom)

1. **Header** — "HDHomeRun Proxy"
2. **MODE** — proxy type (AppProxy / TunerProxy), port, operating sub-mode (Direct / TunerProxy / Direct+Tunarr / etc.)
3. **CONNECTIONS** — UDP count, Dial count, Total (read from `backendRouter.Stats()`, refreshed every second)
4. **BACKENDS** — one line per *configured* backend only (unconfigured backends are hidden):
   - `● HDHR  <ip>` when `directHDHRIP` is set
   - `● Tunarr  :<port>` when `tunarr != nil`
   - Green `●` when configured (reflects startup state; no real-time reachability polling)
5. **Key hints** — `[q] quit` / `[d] toggle debug` at bottom of sidebar

### Log pane

- Scrolling ring buffer, 200 entries max
- Each line: `HH:MM:SS  LEVEL  message  key=value …`
- Level color: INFO=green, DEBUG=blue, WARN=yellow, ERROR=red
- `[d]` toggles DEBUG lines visible/hidden (INFO/WARN/ERROR always shown)
- New entries auto-scroll to bottom; Bubble Tea's `viewport` component handles scrolling

---

## Architecture

### New files

**`tui.go`** (`package main`)

Bubble Tea model:

```go
type tuiModel struct {
    proxy      statsProvider   // interface: Stats() ProxyStats
    logBuf     []logEntry      // ring buffer (cap 200)
    showDebug  bool
    viewport   viewport.Model  // github.com/charmbracelet/bubbles/viewport
    ready      bool
    width      int
    height     int
}
```

Messages:
- `tickMsg` — fired every second by `tea.Tick`, triggers `proxy.Stats()` call
- `logMsg{entry logEntry}` — fired by the custom slog handler on every log write

`Update()` handles `tickMsg`, `logMsg`, `tea.KeyMsg` (`q`=quit, `d`=toggle debug), `tea.WindowSizeMsg`.

`View()` renders sidebar (lipgloss column, fixed width) + log pane (viewport, remaining width) joined horizontally with `lipgloss.JoinHorizontal`.

**`log_handler.go`** (`package main`)

Custom `slog.Handler`:

```go
type tuiHandler struct {
    program *tea.Program
    mu      sync.Mutex
}

func (h *tuiHandler) Handle(_ context.Context, r slog.Record) error {
    h.program.Send(logMsg{
        time:  r.Time,
        level: r.Level,
        msg:   r.Message,
        attrs: attrsFromRecord(r),
    })
    return nil
}
```

Implements `Enabled`, `WithAttrs`, `WithGroup`, `Handle`. `Enabled` returns true for all levels (filtering is done in the TUI model, not the handler).

### Modified files

**`backend_router.go`**

Add `ProxyStats` struct and `Stats()` getter:

```go
type ProxyStats struct {
    Name             string
    DirectHDHRIP     string
    TunarrPort       int
    TunarrConfigured bool // true if tunarr != nil (configured at startup)
    ActiveUDP        int
    ActiveDial       int
}

func (br *backendRouter) Stats() ProxyStats {
    br.activeConnectionsMutex.Lock()
    defer br.activeConnectionsMutex.Unlock()
    s := ProxyStats{
        Name:         br.name,
        DirectHDHRIP: br.directHDHRIP,
        ActiveUDP:    br.activeUDPConnections,
        ActiveDial:   br.activeDialConnections,
    }
    if br.tunarr != nil {
        s.TunarrPort       = br.tunarr.port
        s.TunarrConfigured = true
    }
    return s
}
```

`statsProvider` interface (in `tui.go`):

```go
type statsProvider interface {
    Stats() ProxyStats
}
```

This keeps `tui.go` decoupled from the concrete proxy types.

**`main.go`**

- Add `-tui` bool flag
- When `-tui` is set:
  1. Create `tea.NewProgram(newTuiModel(proxy), tea.WithAltScreen())`
  2. Replace default slog handler: `slog.SetDefault(slog.New(newTuiHandler(p)))`
  3. Start proxy `Run()` in a goroutine (passing the existing context)
  4. Call `p.Run()` — blocks until user quits
  5. Cancel context on return → proxy shuts down

- When `-tui` is not set: existing behavior unchanged

---

## Dependencies

```
charmbracelet/bubbletea   v1.x   (TUI framework)
charmbracelet/lipgloss    v1.x   (layout and styling)
charmbracelet/bubbles     v0.x   (viewport component for log pane)
```

All three are standard charmbracelet packages with no transitive surprises.

---

## Constraints

- `-tui` is incompatible with non-interactive terminals (Docker, systemd). No guard needed — if the terminal doesn't support alt-screen, Bubble Tea degrades gracefully.
- No changes to `AppProxy.Run()`, `TunerProxy.Run()`, or any proxy logic
- `backendRouter` gains `Stats()` — the only proxy-side change
- Existing tests must continue to pass; no TUI code runs in tests

---

## File Map

| File | Change |
|------|--------|
| `tui.go` | **Create** — Bubble Tea model + Update + View + `statsProvider` interface |
| `log_handler.go` | **Create** — custom `slog.Handler` that sends `logMsg` to `tea.Program` |
| `backend_router.go` | **Modify** — add `ProxyStats` struct + `Stats()` method |
| `main.go` | **Modify** — add `-tui` flag, wire TUI program and handler |
| `go.mod` / `go.sum` | **Modify** — add three charmbracelet dependencies |
