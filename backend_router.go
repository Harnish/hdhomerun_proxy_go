package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
	resolveLocalIP         func(*net.UDPAddr) string

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

// checkTunarr probes Tunarr once and records the result.
func (br *backendRouter) checkTunarr(ctx context.Context) bool {
	if br.tunarr.IsAvailable(ctx) {
		br.recordTunarr("")
		return true
	}
	br.recordTunarr("discover.json not reachable")
	return false
}

// watchTunarr re-probes Tunarr periodically so its health reflects the
// present, not just startup. Discovery replies are built locally, so there
// is no per-request traffic to Tunarr to observe instead.
// ponytail: fixed 30s interval; make configurable if anyone needs it.
func (br *backendRouter) watchTunarr(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
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

func (br *backendRouter) buildDiscoveryPacket(srcIP string) []byte {
	cfg := br.store.Get()

	// Get device model info
	modelType := cfg.Device.ModelType
	if modelType == "" {
		modelType = "HDFX-4K"
	}

	modelInfo, err := GetModelInfo(modelType)
	if err != nil {
		modelInfo, _ = GetModelInfo("HDFX-4K")
	}

	// Get or generate Device ID
	deviceID := cfg.Device.DeviceID
	if deviceID == "" {
		deviceID = GenerateRealisticDeviceID(modelType)
	}

	// Get device auth
	deviceAuth := cfg.Device.DeviceAuth
	if deviceAuth == "" {
		deviceAuth = "00000000"
	}

	// Get friendly name
	friendlyName := cfg.Device.FriendlyName
	if friendlyName == "" {
		friendlyName = modelInfo.FriendlyName
	}

	// Get firmware version
	firmwareVersion := cfg.Device.FirmwareVersion
	if firmwareVersion == "" {
		firmwareVersion = "20250825"
	}

	// Build the discovery response packet
	response := fmt.Sprintf("Device: %s\r\n", modelInfo.ModelNumber)
	response += fmt.Sprintf("DeviceID: %s\r\n", deviceID)
	response += fmt.Sprintf("DeviceAuth: %s\r\n", deviceAuth)
	response += fmt.Sprintf("BaseURL: http://%s:5004\r\n", srcIP)
	response += fmt.Sprintf("LineupURL: http://%s:5004/lineup.json\r\n", srcIP)
	response += fmt.Sprintf("TunerCount: %d\r\n", modelInfo.TunerCount)
	response += fmt.Sprintf("FirmwareName: %s\r\n", modelInfo.FirmwareName)
	response += fmt.Sprintf("FirmwareVersion: %s\r\n", firmwareVersion)
	response += fmt.Sprintf("FriendlyName: %s\r\n", friendlyName)

	return []byte(response)
}

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

func (br *backendRouter) forwardToTunarr(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn, ctx context.Context) bool {
	queryStr := string(queryData)
	if queryStr == "TYPE: discover\r\n" || queryStr == "discover" {
		var localIP string
		if br.resolveLocalIP != nil {
			localIP = br.resolveLocalIP(appAddr)
		} else {
			localIP = appAddr.IP.String()
		}

		// Use the new discovery packet builder that includes Device ID
		response := br.buildDiscoveryPacket(localIP)
		_, err := replyConn.WriteToUDP(response, appAddr)
		if err != nil {
			slog.Error("Error sending discovery response to app", "err", err)
			return false
		}

		slog.Debug("Discovery response sent", "bytes", len(response), "device_id", br.store.Get().Device.DeviceID)
		return true
	}

	return false
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
