# Deduplicate Backend Router Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate duplicated backend-forwarding code between `AppProxy` and `TunerProxy` by extracting a shared `backendRouter` embedded struct.

**Architecture:** A new `backend_router.go` file defines `backendRouter` with the shared fields and four shared methods. `AppProxy` and `TunerProxy` each embed `backendRouter`, removing their own copies of those fields and methods. The one behavioral difference (`forwardToTunarr`'s IP resolution strategy) is handled by a `resolveLocalIP` function field set at construction time.

**Tech Stack:** Go 1.25, stdlib only. No new dependencies.

---

## File Map

| File | Change |
|------|--------|
| `backend_router.go` | **Create** — `backendRouter` struct + 4 methods |
| `app_proxy.go` | **Modify** — embed `backendRouter`, remove 6 shared fields, remove 4 methods, update constructor |
| `tuner_proxy.go` | **Modify** — embed `backendRouter`, remove 6 shared fields, remove 4 methods, update constructor |

---

## Task 1: Create `backend_router.go`

**Files:**
- Create: `backend_router.go`

- [ ] **Step 1: Create `backend_router.go`**

Create `/home/jharnish/Work/hdhomerun_proxy_go/backend_router.go` with this exact content:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// backendRouter holds the shared fields and methods for routing discovery
// queries to HDHomeRun devices and/or a Tunarr backend. It is embedded by
// both AppProxy and TunerProxy.
type backendRouter struct {
	tunarr                 *TunarrBackend
	useTunarrOnly          bool
	directHDHRIP           string
	activeConnectionsMutex sync.Mutex
	activeUDPConnections   int
	activeDialConnections  int
	name                   string                 // "AppProxy" or "TunerProxy", used in log output
	resolveLocalIP         func(*net.UDPAddr) string // IP resolution strategy for Tunarr responses
}

// forwardToBackend dispatches a query to Tunarr and/or a direct HDHomeRun.
func (br *backendRouter) forwardToBackend(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn, ctx context.Context) {
	if br.tunarr != nil {
		if br.forwardToTunarr(queryData, appAddr, replyConn, ctx) {
			return
		}
		if br.useTunarrOnly {
			slog.Warn("Tunarr-only mode but Tunarr request failed")
			return
		}
	}

	if br.directHDHRIP != "" {
		br.forwardToDirectHDHR(queryData, appAddr, replyConn)
	}
}

// forwardToTunarr handles a discovery request via the Tunarr HTTP backend.
func (br *backendRouter) forwardToTunarr(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn, ctx context.Context) bool {
	queryStr := string(queryData)
	if queryStr == "TYPE: discover\r\n" || queryStr == "discover" {
		info, err := br.tunarr.GetDiscoverInfo(ctx)
		if err != nil {
			slog.Error("Error getting Tunarr discovery info", "err", err)
			return false
		}

		localIP := br.resolveLocalIP(appAddr)
		response := BuildHDHRDiscoveryPacket(info, br.tunarr.port, localIP)
		_, err = replyConn.WriteToUDP(response, appAddr)
		if err != nil {
			slog.Error("Error sending Tunarr discovery response to app", "err", err)
			return false
		}

		slog.Debug("Tunarr discovery response sent", "bytes", len(response))
		return true
	}

	return false
}

// forwardToDirectHDHR dials the HDHomeRun device, sends the query, and
// writes the response back to the app.
func (br *backendRouter) forwardToDirectHDHR(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn) {
	br.activeConnectionsMutex.Lock()
	br.activeDialConnections++
	br.activeConnectionsMutex.Unlock()
	defer func() {
		br.activeConnectionsMutex.Lock()
		br.activeDialConnections--
		br.activeConnectionsMutex.Unlock()
	}()

	hdhrAddr := net.JoinHostPort(br.directHDHRIP, fmt.Sprintf("%d", HDHomeRunDiscoveryUDPPort))
	hdhrUDPAddr, err := net.ResolveUDPAddr("udp", hdhrAddr)
	if err != nil {
		slog.Error("Error resolving HDHomeRun address", "addr", hdhrAddr, "err", err)
		return
	}

	conn, err := net.DialUDP("udp", nil, hdhrUDPAddr)
	if err != nil {
		slog.Error("Error connecting to HDHomeRun", "addr", hdhrAddr, "err", err)
		return
	}
	defer conn.Close()

	_, err = conn.Write(queryData)
	if err != nil {
		slog.Error("Error sending query to HDHomeRun", "err", err)
		return
	}

	conn.SetReadDeadline(time.Now().Add(time.Duration(UDPReadTimeout) * time.Millisecond))
	respBuf := make([]byte, UDPReadBufferSize)
	n, err := conn.Read(respBuf)
	if err != nil {
		if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
			slog.Error("Error reading response from HDHomeRun", "err", err)
		}
		return
	}

	if n > 0 {
		slog.Debug("Response received from HDHomeRun", "bytes", n)
		_, err := replyConn.WriteToUDP(respBuf[:n], appAddr)
		if err != nil {
			slog.Error("Error sending response to app", "err", err)
		}
	}
}

// logActiveConnections periodically logs dial connection counts.
func (br *backendRouter) logActiveConnections(ctx context.Context, intervalSeconds int) {
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			br.activeConnectionsMutex.Lock()
			udpCount := br.activeUDPConnections
			dialCount := br.activeDialConnections
			br.activeConnectionsMutex.Unlock()

			slog.Info(br.name+" active connections", "udp", udpCount, "dial", dialCount, "total", udpCount+dialCount)
		}
	}
}
```

- [ ] **Step 2: Build to verify**

```bash
cd /home/jharnish/Work/hdhomerun_proxy_go && go build ./...
```

Expected: no output (success). The new file compiles alongside the existing code.

- [ ] **Step 3: Commit**

```bash
cd /home/jharnish/Work/hdhomerun_proxy_go && git add backend_router.go && git commit -m "feat: add backendRouter shared struct"
```

---

## Task 2: Update `AppProxy` to embed `backendRouter`

**Files:**
- Modify: `app_proxy.go`

Three changes in this file:
1. Replace the struct definition (lines 14–24) to remove the 6 shared fields and embed `backendRouter`
2. Replace `NewAppProxy` (lines 27–31) to initialize `backendRouter` with `name` and `resolveLocalIP`
3. Delete the 4 methods that are now provided by the embedded struct

- [ ] **Step 1: Replace the `AppProxy` struct definition**

In `app_proxy.go`, replace:

```go
// AppProxy acts like an HDHomeRun app
type AppProxy struct {
	codec                  *MessageCodec
	tcpTransport           net.Conn
	tcpMutex               sync.Mutex
	directHDHRIP           string         // If set, listen for UDP broadcasts and proxy directly to this IP
	tunarr                 *TunarrBackend // Optional Tunarr backend
	useTunarrOnly          bool           // If true, ignore HDHR and only use Tunarr
	activeConnectionsMutex sync.Mutex
	activeUDPConnections   int // Number of active UDP connections
	activeDialConnections  int // Number of active dial connections to HDHR/Tunarr
}
```

With:

```go
// AppProxy acts like an HDHomeRun app
type AppProxy struct {
	codec        *MessageCodec
	tcpTransport net.Conn
	tcpMutex     sync.Mutex
	backendRouter
}
```

- [ ] **Step 2: Replace `NewAppProxy`**

In `app_proxy.go`, replace:

```go
// NewAppProxy creates a new AppProxy
func NewAppProxy() *AppProxy {
	return &AppProxy{
		codec: NewMessageCodec(),
	}
}
```

With:

```go
// NewAppProxy creates a new AppProxy
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

- [ ] **Step 3: Delete `forwardToBackend` from `app_proxy.go`**

Remove the entire function (currently lines 121–136):

```go
// forwardToBackend sends a query to the HDHR or Tunarr backend and replies back to the app
func (ap *AppProxy) forwardToBackend(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn, ctx context.Context) {
	if ap.tunarr != nil {
		if ap.forwardToTunarr(queryData, appAddr, replyConn, ctx) {
			return
		}
		if ap.useTunarrOnly {
			slog.Warn("Tunarr-only mode but Tunarr request failed")
			return
		}
	}

	if ap.directHDHRIP != "" {
		ap.forwardToDirectHDHR(queryData, appAddr, replyConn)
	}
}
```

- [ ] **Step 4: Delete `forwardToTunarr` from `app_proxy.go`**

Remove the entire function (currently lines 138–168):

```go
// forwardToTunarr sends a request to Tunarr backend
func (ap *AppProxy) forwardToTunarr(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn, ctx context.Context) bool {
	// Check if this is a discovery request
	queryStr := string(queryData)
	if queryStr == "TYPE: discover\r\n" || queryStr == "discover" {
		// Get Tunarr device info
		info, err := ap.tunarr.GetDiscoverInfo(ctx)
		if err != nil {
			slog.Error("Error getting Tunarr discovery info", "err", err)
			return false
		}

		// Build HDHR-like discovery response from Tunarr
		localIP, err := GetLocalIPForConnection(appAddr.IP.String() + ":65001")
		if err != nil {
			localIP = "127.0.0.1"
		}

		response := BuildHDHRDiscoveryPacket(info, ap.tunarr.port, localIP)
		_, err = replyConn.WriteToUDP(response, appAddr)
		if err != nil {
			slog.Error("Error sending Tunarr discovery response to app", "err", err)
			return false
		}

		slog.Debug("Tunarr discovery response sent", "bytes", len(response))
		return true
	}

	return false
}
```

- [ ] **Step 5: Delete `forwardToDirectHDHR` from `app_proxy.go`**

Remove the entire function (currently lines 170–218):

```go
// forwardToDirectHDHR sends a query to the HDHomeRun and replies back to the app
func (ap *AppProxy) forwardToDirectHDHR(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn) {
	ap.activeConnectionsMutex.Lock()
	ap.activeDialConnections++
	ap.activeConnectionsMutex.Unlock()
	defer func() {
		ap.activeConnectionsMutex.Lock()
		ap.activeDialConnections--
		ap.activeConnectionsMutex.Unlock()
	}()

	hdhrAddr := net.JoinHostPort(ap.directHDHRIP, fmt.Sprintf("%d", HDHomeRunDiscoveryUDPPort))
	hdhrUDPAddr, err := net.ResolveUDPAddr("udp", hdhrAddr)
	if err != nil {
		slog.Error("Error resolving HDHomeRun address", "addr", hdhrAddr, "err", err)
		return
	}

	conn, err := net.DialUDP("udp", nil, hdhrUDPAddr)
	if err != nil {
		slog.Error("Error connecting to HDHomeRun", "addr", hdhrAddr, "err", err)
		return
	}
	defer conn.Close()

	_, err = conn.Write(queryData)
	if err != nil {
		slog.Error("Error sending query to HDHomeRun", "err", err)
		return
	}

	conn.SetReadDeadline(time.Now().Add(time.Duration(UDPReadTimeout) * time.Millisecond))
	respBuf := make([]byte, UDPReadBufferSize)
	n, err := conn.Read(respBuf)
	if err != nil {
		if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
			slog.Error("Error reading response from HDHomeRun", "err", err)
		}
		return
	}

	if n > 0 {
		slog.Debug("Response received from HDHomeRun", "bytes", n)
		_, err := replyConn.WriteToUDP(respBuf[:n], appAddr)
		if err != nil {
			slog.Error("Error sending response to app", "err", err)
		}
	}
}
```

- [ ] **Step 6: Delete `logActiveConnections` from `app_proxy.go`**

Remove the entire function (currently lines 389–407):

```go
// logActiveConnections periodically logs the number of active connections
func (ap *AppProxy) logActiveConnections(ctx context.Context, intervalSeconds int) {
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ap.activeConnectionsMutex.Lock()
			udpCount := ap.activeUDPConnections
			dialCount := ap.activeDialConnections
			ap.activeConnectionsMutex.Unlock()

			slog.Info("AppProxy active connections", "udp", udpCount, "dial", dialCount, "total", udpCount+dialCount)
		}
	}
}
```

- [ ] **Step 7: Build and test**

```bash
cd /home/jharnish/Work/hdhomerun_proxy_go && go build ./... && go test ./... -v
```

Expected: clean build, all 4 tests pass.

- [ ] **Step 8: Commit**

```bash
cd /home/jharnish/Work/hdhomerun_proxy_go && git add app_proxy.go && git commit -m "refactor: AppProxy embeds backendRouter"
```

---

## Task 3: Update `TunerProxy` to embed `backendRouter`

**Files:**
- Modify: `tuner_proxy.go`

Same three changes as Task 2, applied to `TunerProxy`.

- [ ] **Step 1: Replace the `TunerProxy` struct definition**

In `tuner_proxy.go`, replace:

```go
// TunerProxy acts like an HDHomeRun tuner
type TunerProxy struct {
	codec                  *MessageCodec
	tcpTransport           net.Conn
	tcpMutex               sync.Mutex
	udpTransport           *net.UDPConn
	udpMutex               sync.Mutex
	directHDHRIP           string         // If set, connect directly to HDHomeRun instead of app proxy
	tunarr                 *TunarrBackend // Optional Tunarr backend
	useTunarrOnly          bool           // If true, ignore HDHR and only use Tunarr
	activeConnectionsMutex sync.Mutex
	activeUDPConnections   int // Number of active UDP connections
	activeDialConnections  int // Number of active dial connections to HDHR/Tunarr
}
```

With:

```go
// TunerProxy acts like an HDHomeRun tuner
type TunerProxy struct {
	codec        *MessageCodec
	tcpTransport net.Conn
	tcpMutex     sync.Mutex
	udpTransport *net.UDPConn
	udpMutex     sync.Mutex
	backendRouter
}
```

- [ ] **Step 2: Replace `NewTunerProxy`**

In `tuner_proxy.go`, replace:

```go
// NewTunerProxy creates a new TunerProxy
func NewTunerProxy() *TunerProxy {
	return &TunerProxy{
		codec: NewMessageCodec(),
	}
}
```

With:

```go
// NewTunerProxy creates a new TunerProxy
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

- [ ] **Step 3: Delete `forwardToBackend` from `tuner_proxy.go`**

Remove the entire function (currently lines 132–147):

```go
// forwardToBackend sends a query to the HDHR or Tunarr backend and replies back to the app
func (tp *TunerProxy) forwardToBackend(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn, ctx context.Context) {
	if tp.tunarr != nil {
		if tp.forwardToTunarr(queryData, appAddr, replyConn, ctx) {
			return
		}
		if tp.useTunarrOnly {
			slog.Warn("Tunarr-only mode but Tunarr request failed")
			return
		}
	}

	if tp.directHDHRIP != "" {
		tp.forwardToDirectHDHR(queryData, appAddr, replyConn)
	}
}
```

- [ ] **Step 4: Delete `forwardToTunarr` from `tuner_proxy.go`**

Remove the entire function (currently lines 149–175):

```go
// forwardToTunarr sends a request to Tunarr backend
func (tp *TunerProxy) forwardToTunarr(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn, ctx context.Context) bool {
	// Check if this is a discovery request
	queryStr := string(queryData)
	if queryStr == "TYPE: discover\r\n" || queryStr == "discover" {
		// Get Tunarr device info
		info, err := tp.tunarr.GetDiscoverInfo(ctx)
		if err != nil {
			slog.Error("Error getting Tunarr discovery info", "err", err)
			return false
		}

		// Build HDHR-like discovery response from Tunarr
		localIP := appAddr.IP.String()
		response := BuildHDHRDiscoveryPacket(info, tp.tunarr.port, localIP)
		_, err = replyConn.WriteToUDP(response, appAddr)
		if err != nil {
			slog.Error("Error sending Tunarr discovery response to app", "err", err)
			return false
		}

		slog.Debug("Tunarr discovery response sent", "bytes", len(response))
		return true
	}

	return false
}
```

- [ ] **Step 5: Delete `forwardToDirectHDHR` from `tuner_proxy.go`**

Remove the entire function (currently lines 177–225):

```go
// forwardToDirectHDHR sends a query to the HDHomeRun and replies back to the app
func (tp *TunerProxy) forwardToDirectHDHR(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn) {
	tp.activeConnectionsMutex.Lock()
	tp.activeDialConnections++
	tp.activeConnectionsMutex.Unlock()
	defer func() {
		tp.activeConnectionsMutex.Lock()
		tp.activeDialConnections--
		tp.activeConnectionsMutex.Unlock()
	}()

	hdhrAddr := net.JoinHostPort(tp.directHDHRIP, fmt.Sprintf("%d", HDHomeRunDiscoveryUDPPort))
	hdhrUDPAddr, err := net.ResolveUDPAddr("udp", hdhrAddr)
	if err != nil {
		slog.Error("Error resolving HDHomeRun address", "addr", hdhrAddr, "err", err)
		return
	}

	conn, err := net.DialUDP("udp", nil, hdhrUDPAddr)
	if err != nil {
		slog.Error("Error connecting to HDHomeRun", "addr", hdhrAddr, "err", err)
		return
	}
	defer conn.Close()

	_, err = conn.Write(queryData)
	if err != nil {
		slog.Error("Error sending query to HDHomeRun", "err", err)
		return
	}

	conn.SetReadDeadline(time.Now().Add(time.Duration(UDPReadTimeout) * time.Millisecond))
	respBuf := make([]byte, UDPReadBufferSize)
	n, err := conn.Read(respBuf)
	if err != nil {
		if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
			slog.Error("Error reading response from HDHomeRun", "err", err)
		}
		return
	}

	if n > 0 {
		slog.Debug("Response received from HDHomeRun (direct mode)", "bytes", n)
		_, err := replyConn.WriteToUDP(respBuf[:n], appAddr)
		if err != nil {
			slog.Error("Error sending response to app", "err", err)
		}
	}
}
```

- [ ] **Step 6: Delete `logActiveConnections` from `tuner_proxy.go`**

Remove the entire function (currently lines 455–473):

```go
// logActiveConnections periodically logs the number of active connections
func (tp *TunerProxy) logActiveConnections(ctx context.Context, intervalSeconds int) {
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tp.activeConnectionsMutex.Lock()
			udpCount := tp.activeUDPConnections
			dialCount := tp.activeDialConnections
			tp.activeConnectionsMutex.Unlock()

			slog.Info("TunerProxy active connections", "udp", udpCount, "dial", dialCount, "total", udpCount+dialCount)
		}
	}
}
```

- [ ] **Step 7: Build and test**

```bash
cd /home/jharnish/Work/hdhomerun_proxy_go && go build ./... && go test ./... -v
```

Expected: clean build, all 4 tests pass.

- [ ] **Step 8: Commit**

```bash
cd /home/jharnish/Work/hdhomerun_proxy_go && git add tuner_proxy.go && git commit -m "refactor: TunerProxy embeds backendRouter"
```
