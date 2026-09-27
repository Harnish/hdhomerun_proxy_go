package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

type backendRouter struct {
	tunarr                 *TunarrBackend
	useTunarrOnly          bool
	directHDHRIP           string
	store                  *configStore
	activeConnectionsMutex sync.Mutex
	activeUDPConnections   int
	activeDialConnections  int
	name                   string

	// Health, guarded by healthMu. Recorded from real traffic so the UIs
	// never show a backend as healthy without evidence.
	healthMu     sync.Mutex
	hdhrHealth   BackendHealth
	tunarrHealth BackendHealth
	linkExpected bool // true when this instance talks to a peer proxy over TCP
	linkUp       bool
	linkPeer     string
	linkSince    time.Time
}

// BackendHealth is the last observed outcome of talking to a backend.
// State is "unknown" (never contacted), "ok", or "fail".
type BackendHealth struct {
	State string
	At    time.Time `json:",omitempty"`
	Err   string    `json:",omitempty"`
}

// ProxyStats is a point-in-time snapshot of backendRouter state for display.
type ProxyStats struct {
	Name             string
	DirectHDHRIP     string
	HDHRTarget       string // DirectHDHRIP, "broadcast" (AppProxy relaying for a TunerProxy), or ""
	TunarrHost       string
	TunarrPort       int
	TunarrConfigured bool // true if tunarr != nil (configured at startup)
	ActiveUDP        int
	ActiveDial       int
	HDHR             BackendHealth
	Tunarr           BackendHealth
	LinkExpected     bool
	LinkUp           bool
	LinkPeer         string
	LinkSince        time.Time `json:",omitempty"`
}

func (br *backendRouter) Stats() ProxyStats {
	br.activeConnectionsMutex.Lock()
	s := ProxyStats{
		Name:         br.name,
		DirectHDHRIP: br.directHDHRIP,
		ActiveUDP:    br.activeUDPConnections,
		ActiveDial:   br.activeDialConnections,
	}
	br.activeConnectionsMutex.Unlock()
	if br.tunarr != nil {
		s.TunarrHost = br.tunarr.host
		s.TunarrPort = br.tunarr.port
		s.TunarrConfigured = true
	}
	br.healthMu.Lock()
	s.HDHR = withUnknown(br.hdhrHealth)
	s.Tunarr = withUnknown(br.tunarrHealth)
	s.LinkExpected = br.linkExpected
	s.LinkUp = br.linkUp
	s.LinkPeer = br.linkPeer
	s.LinkSince = br.linkSince
	br.healthMu.Unlock()
	switch {
	case br.directHDHRIP != "":
		s.HDHRTarget = br.directHDHRIP
	case br.name == "AppProxy" && s.LinkExpected:
		s.HDHRTarget = "broadcast"
	}
	return s
}

func withUnknown(h BackendHealth) BackendHealth {
	if h.State == "" {
		h.State = "unknown"
	}
	return h
}

func (br *backendRouter) recordHDHR(err string) {
	br.healthMu.Lock()
	defer br.healthMu.Unlock()
	br.hdhrHealth = healthFrom(err)
}

func (br *backendRouter) recordTunarr(err string) {
	br.healthMu.Lock()
	defer br.healthMu.Unlock()
	br.tunarrHealth = healthFrom(err)
}

func healthFrom(err string) BackendHealth {
	if err == "" {
		return BackendHealth{State: "ok", At: time.Now()}
	}
	return BackendHealth{State: "fail", At: time.Now(), Err: err}
}

// setLink records the state of the TCP link to the peer proxy.
func (br *backendRouter) setLink(up bool, peer string) {
	br.healthMu.Lock()
	defer br.healthMu.Unlock()
	br.linkExpected = true
	if up != br.linkUp || br.linkSince.IsZero() {
		br.linkSince = time.Now()
	}
	br.linkUp = up
	if peer != "" {
		br.linkPeer = peer
	}
}

// checkTunarr refreshes the Tunarr channel catalog once and records the
// outcome as Tunarr's health.
func (br *backendRouter) checkTunarr(ctx context.Context) bool {
	if err := br.tunarr.RefreshCatalog(ctx, br.store.Get()); err != nil {
		slog.Debug("Tunarr catalog refresh failed", "err", err)
		br.recordTunarr(err.Error())
		return false
	}
	br.recordTunarr("")
	return true
}

// watchTunarr keeps the channel catalog (and so Tunarr's health) current.
func (br *backendRouter) watchTunarr(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(br.store.Get().TunarrRefreshSeconds()) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			br.checkTunarr(ctx)
		}
	}
}

// deviceIdentity resolves the emulated device's model, ID and auth from the
// device config section, applying the same defaults everywhere they're used.
func deviceIdentity(cfg *Config) (model DeviceIDModel, deviceID, deviceAuth string) {
	modelType := orDefault(cfg.Device.ModelType, "HDFX-4K")
	model, err := GetModelInfo(modelType)
	if err != nil {
		model, _ = GetModelInfo("HDFX-4K")
	}
	deviceID = cfg.Device.DeviceID
	if deviceID == "" {
		deviceID = GenerateRealisticDeviceID(modelType)
	}
	return model, deviceID, orDefault(cfg.Device.DeviceAuth, "00000000")
}

// tunarrDiscoverReply returns the binary discovery reply advertising the
// emulated Tunarr tuner, or nil when query isn't a discover request for it.
// appIP is the requesting app; the advertised base URL is this host's address
// on the route back to it, unless tunarr.base_url overrides it.
func (br *backendRouter) tunarrDiscoverReply(query []byte, appIP net.IP) []byte {
	tags, ok := parseDiscoverRequest(query)
	if !ok {
		return nil
	}
	cfg := br.store.Get()
	model, deviceID, deviceAuth := deviceIdentity(cfg)
	if !discoverRequestMatches(tags, deviceID) {
		return nil
	}
	base := strings.TrimRight(cfg.Tunarr.BaseURL, "/")
	if base == "" {
		base = "http://" + net.JoinHostPort(localIPToward(appIP), "5004")
	}
	return buildDiscoverReply(hdhrDevice{
		DeviceID:   deviceID,
		DeviceAuth: deviceAuth,
		TunerCount: model.TunerCount,
		BaseURL:    base,
	})
}

// localIPToward returns this host's IP on the route to ip.
func localIPToward(ip net.IP) string {
	local, err := GetLocalIPForConnection(net.JoinHostPort(ip.String(), "65001"))
	if err != nil {
		return "127.0.0.1"
	}
	return local
}

// forwardToBackend answers an app's discovery query. With Tunarr enabled the
// emulated tuner replies; the real HDHomeRun is also queried unless
// use_tunarr_only is set, so apps see both devices.
func (br *backendRouter) forwardToBackend(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn, ctx context.Context) {
	if br.tunarr != nil {
		if reply := br.tunarrDiscoverReply(queryData, appAddr.IP); reply != nil {
			if _, err := replyConn.WriteToUDP(reply, appAddr); err != nil {
				slog.Error("Error sending Tunarr discovery reply to app", "err", err)
			} else {
				slog.Debug("Tunarr discovery reply sent", "bytes", len(reply), "app", appAddr.String())
			}
		}
		if br.useTunarrOnly {
			return
		}
	}

	if br.directHDHRIP != "" {
		br.forwardToDirectHDHR(queryData, appAddr, replyConn)
	}
}

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
		br.recordHDHR("cannot resolve " + hdhrAddr)
		return
	}

	conn, err := net.DialUDP("udp", nil, hdhrUDPAddr)
	if err != nil {
		slog.Error("Error connecting to HDHomeRun", "addr", hdhrAddr, "err", err)
		br.recordHDHR(err.Error())
		return
	}
	defer conn.Close()

	_, err = conn.Write(queryData)
	if err != nil {
		slog.Error("Error sending query to HDHomeRun", "err", err)
		br.recordHDHR(err.Error())
		return
	}

	conn.SetReadDeadline(time.Now().Add(time.Duration(UDPReadTimeout) * time.Millisecond))
	respBuf := make([]byte, UDPReadBufferSize)
	n, err := conn.Read(respBuf)
	if err != nil {
		if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
			slog.Error("Error reading response from HDHomeRun", "err", err)
			br.recordHDHR(err.Error())
		} else {
			br.recordHDHR(fmt.Sprintf("no reply within %dms", UDPReadTimeout))
		}
		return
	}

	if n > 0 {
		br.recordHDHR("")
		slog.Debug("Response received from HDHomeRun", "bytes", n)
		_, err := replyConn.WriteToUDP(respBuf[:n], appAddr)
		if err != nil {
			slog.Error("Error sending response to app", "err", err)
		}
	}
}

func (br *backendRouter) logActiveConnections(ctx context.Context, store *configStore) {
	intervalSeconds := store.Get().LogActiveConnectionsInterval
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if newInterval := store.Get().LogActiveConnectionsInterval; newInterval != intervalSeconds {
				intervalSeconds = newInterval
				if newInterval > 0 {
					ticker.Reset(time.Duration(intervalSeconds) * time.Second)
				} else {
					return
				}
			}
			br.activeConnectionsMutex.Lock()
			udpCount := br.activeUDPConnections
			dialCount := br.activeDialConnections
			br.activeConnectionsMutex.Unlock()

			slog.Info("active connections", "name", br.name, "udp", udpCount, "dial", dialCount, "total", udpCount+dialCount)
		}
	}
}
