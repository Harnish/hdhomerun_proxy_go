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
	activeConnectionsMutex sync.Mutex
	activeUDPConnections   int
	activeDialConnections  int
	name                   string
	resolveLocalIP         func(*net.UDPAddr) string
}

// ProxyStats is a point-in-time snapshot of backendRouter state for display.
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
		s.TunarrPort = br.tunarr.port
		s.TunarrConfigured = true
	}
	return s
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
		info, err := br.tunarr.GetDiscoverInfo(ctx)
		if err != nil {
			slog.Error("Error getting Tunarr discovery info", "err", err)
			return false
		}

		var localIP string
		if br.resolveLocalIP != nil {
			localIP = br.resolveLocalIP(appAddr)
		} else {
			localIP = appAddr.IP.String()
		}
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
