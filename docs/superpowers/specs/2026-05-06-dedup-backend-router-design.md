# Deduplicate Backend Routing — Design Spec

**Date:** 2026-05-06  
**Scope:** Eliminate duplicated backend-forwarding logic between `AppProxy` and `TunerProxy` by extracting a shared embedded struct.

---

## Problem

`AppProxy` (`app_proxy.go`) and `TunerProxy` (`tuner_proxy.go`) each contain four nearly identical methods:

| Method | Only real difference |
|--------|---------------------|
| `forwardToBackend` | Receiver name only |
| `forwardToDirectHDHR` | Receiver name + one log string |
| `logActiveConnections` | Receiver name + one log string |
| `forwardToTunarr` | Receiver name + localIP resolution strategy |

Each struct also independently declares the same fields: `tunarr`, `useTunarrOnly`, `directHDHRIP`, `activeDialConnections`, `activeUDPConnections`, `activeConnectionsMutex`.

---

## Solution

### New file: `backend_router.go`

Defines a `backendRouter` struct that owns the shared fields and implements the four shared methods.

```go
type backendRouter struct {
    tunarr                 *TunarrBackend
    useTunarrOnly          bool
    directHDHRIP           string
    activeConnectionsMutex sync.Mutex
    activeUDPConnections   int
    activeDialConnections  int
    name                   string
    resolveLocalIP         func(*net.UDPAddr) string
}
```

**`name`** — holds `"AppProxy"` or `"TunerProxy"`, used in `logActiveConnections` log output.

**`resolveLocalIP`** — a function field set at construction time that encapsulates the one behavioral difference in `forwardToTunarr`:
- `AppProxy`: calls `GetLocalIPForConnection(appAddr.IP.String() + ":65001")` with `"127.0.0.1"` fallback
- `TunerProxy`: returns `appAddr.IP.String()` directly

Methods on `backendRouter`:
- `forwardToBackend(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn, ctx context.Context)`
- `forwardToTunarr(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn, ctx context.Context) bool`
- `forwardToDirectHDHR(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn)`
- `logActiveConnections(ctx context.Context, intervalSeconds int)`

---

### Updated `AppProxy` (`app_proxy.go`)

Remove the four shared fields and four shared methods. Embed `backendRouter`:

```go
type AppProxy struct {
    codec        *MessageCodec
    tcpTransport net.Conn
    tcpMutex     sync.Mutex
    backendRouter
}

func NewAppProxy() *AppProxy {
    return &AppProxy{
        codec: NewMessageCodec(),
        backendRouter: backendRouter{
            name: "AppProxy",
            resolveLocalIP: func(appAddr *net.UDPAddr) string {
                ip, err := GetLocalIPForConnection(appAddr.IP.String() + ":65001")
                if err != nil {
                    return "127.0.0.1"
                }
                return ip
            },
        },
    }
}
```

---

### Updated `TunerProxy` (`tuner_proxy.go`)

Remove the four shared fields and four shared methods. Embed `backendRouter`:

```go
type TunerProxy struct {
    codec        *MessageCodec
    tcpTransport net.Conn
    tcpMutex     sync.Mutex
    udpTransport *net.UDPConn
    udpMutex     sync.Mutex
    backendRouter
}

func NewTunerProxy() *TunerProxy {
    return &TunerProxy{
        codec: NewMessageCodec(),
        backendRouter: backendRouter{
            name: "TunerProxy",
            resolveLocalIP: func(appAddr *net.UDPAddr) string {
                return appAddr.IP.String()
            },
        },
    }
}
```

---

## File Map

| File | Change |
|------|--------|
| `backend_router.go` | **Create** — `backendRouter` struct + 4 methods |
| `app_proxy.go` | **Modify** — remove 4 shared fields, remove 4 methods, update struct + constructor |
| `tuner_proxy.go` | **Modify** — same as above |

---

## Constraints

- No changes to `Run()` call sites in `main.go`
- No changes to `tunarr_backend.go` or `message_codec.go`
- Existing tests (`tunarr_backend_test.go`) must continue to pass
- No new public API surface — `backendRouter` is unexported
