package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// AppProxy acts like an HDHomeRun app
type AppProxy struct {
	codec        *MessageCodec
	tcpTransport net.Conn
	tcpMutex     sync.Mutex
	backendRouter
}

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

// Run starts the app proxy server
// bindAddr: address to listen on (e.g., "0.0.0.0" or "192.168.1.5")
// directIP: if provided, listen for UDP broadcasts and proxy directly to this HDHomeRun IP
// cfg: configuration object for tuning parameters
func (ap *AppProxy) Run(ctx context.Context, bindAddr, directIP string, cfg *Config) error {
	ap.directHDHRIP = directIP
	ap.useTunarrOnly = cfg.Tunarr.UseTunarrOnly

	// Initialize Tunarr backend if enabled
	if cfg.Tunarr.Enabled {
		ap.tunarr = NewTunarrBackend(cfg.Tunarr.Host, cfg.Tunarr.Port, cfg.Tunarr.HttpTimeout)
		if ap.tunarr.IsAvailable(ctx) {
			slog.Info("Tunarr backend available", "host", cfg.Tunarr.Host, "port", cfg.Tunarr.Port)
		} else {
			slog.Warn("Tunarr backend not available", "host", cfg.Tunarr.Host, "port", cfg.Tunarr.Port)
			if cfg.Tunarr.UseTunarrOnly {
				return fmt.Errorf("tunarr backend required but not available")
			}
		}
	}

	if directIP != "" || (ap.tunarr != nil && ap.useTunarrOnly) {
		// Direct mode: listen for UDP broadcasts and proxy to the HDHomeRun/Tunarr directly
		return ap.runDirectMode(ctx, bindAddr, cfg)
	} else {
		// Tuner proxy mode: listen for TCP connections from the tuner proxy
		return ap.runTunerProxyMode(ctx, bindAddr, cfg)
	}
}

// runDirectMode listens for UDP broadcast queries and sends them directly to HDHomeRun
func (ap *AppProxy) runDirectMode(ctx context.Context, bindAddr string, cfg *Config) error {
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}

	addr := net.JoinHostPort(bindAddr, fmt.Sprintf("%d", HDHomeRunDiscoveryUDPPort))
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	slog.Info("App proxy listening for UDP broadcasts", "addr", addr, "direct_hdhomerun_ip", ap.directHDHRIP)

	// Start connection logging goroutine if configured
	if cfg.LogActiveConnectionsInterval > 0 {
		go ap.logActiveConnections(ctx, cfg.LogActiveConnectionsInterval)
	}

	buf := make([]byte, UDPReadBufferSize)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-ctx.Done():
				return nil
			default:
				slog.Error("Error reading UDP", "err", err)
				continue
			}
		}

		if n > 0 {
			slog.Debug("Request received from app", "bytes", n, "source", remoteAddr.String())

			// Forward the query to HDHR/Tunarr backend
			go ap.forwardToBackend(buf[:n], remoteAddr, conn, ctx)
		}
	}
}

// runTunerProxyMode listens for TCP connections from the tuner proxy
func (ap *AppProxy) runTunerProxyMode(ctx context.Context, bindAddr string, cfg *Config) error {
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}

	addr := fmt.Sprintf("%s:%d", bindAddr, TCPPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	slog.Info("App proxy listening for tuner proxy", "addr", addr)

	// Accept connections in a goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				listener.Close()
				return
			default:
			}

			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					slog.Error("Error accepting connection", "err", err)
				}
				continue
			}

			go ap.handleTunerProxyConnection(ctx, conn)
		}
	}()

	<-ctx.Done()
	return nil
}

// handleTunerProxyConnection handles a connection from the tuner proxy
func (ap *AppProxy) handleTunerProxyConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	peername := conn.RemoteAddr()
	slog.Info("Tuner proxy connected", "addr", peername)

	ap.tcpMutex.Lock()
	ap.tcpTransport = conn
	ap.tcpMutex.Unlock()

	codec := NewMessageCodec()
	buf := make([]byte, UDPReadBufferSize)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := conn.Read(buf)
		if err != nil {
			slog.Info("Tuner proxy disconnected", "addr", peername)
			ap.tcpMutex.Lock()
			ap.tcpTransport = nil
			ap.tcpMutex.Unlock()
			return
		}

		if n > 0 {
			slog.Debug("Request received from tuner proxy", "bytes", n)
			codec.Decode(buf[:n], ap.onReceivedMessage)
		}
	}
}

// onReceivedMessage handles a message from the tuner proxy
func (ap *AppProxy) onReceivedMessage(msg []byte) {
	if len(msg) < 6 {
		slog.Warn("Invalid message: too short", "len", len(msg))
		return
	}

	// Unpack the message
	sourceAddr := msg[0:4]
	sourcePort := binary.BigEndian.Uint16(msg[4:6])
	queryData := msg[6:]

	// Perform the query
	ap.queryTuner(queryData, func(replyData []byte) {
		ap.reply(sourceAddr, sourcePort, replyData)
	})
}

// queryTuner sends a broadcast query to tuners
func (ap *AppProxy) queryTuner(queryData []byte, callback func([]byte)) {
	go func() {
		broadcastAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("255.255.255.255:%d", HDHomeRunDiscoveryUDPPort))
		if err != nil {
			slog.Error("Error resolving broadcast address", "err", err)
			return
		}

		localAddr, err := net.ResolveUDPAddr("udp", "0.0.0.0:0")
		if err != nil {
			slog.Error("Error resolving local address", "err", err)
			return
		}

		conn, err := net.ListenUDP("udp", localAddr)
		if err != nil {
			slog.Error("Error creating UDP socket", "err", err)
			return
		}
		defer conn.Close()

		_, err = conn.WriteTo(queryData, broadcastAddr)
		if err != nil {
			slog.Error("Error sending broadcast query", "err", err)
			return
		}

		conn.SetReadDeadline(time.Now().Add(time.Duration(UDPReadTimeout) * time.Millisecond))
		buf := make([]byte, UDPReadBufferSize)
		for {
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
					slog.Error("Error reading UDP response", "err", err)
				}
				return
			}
			if n > 0 {
				slog.Debug("Reply received from tuner", "bytes", n)
				callback(buf[:n])
			}
		}
	}()
}

// reply sends a reply message back to the tuner proxy
func (ap *AppProxy) reply(sourceAddr []byte, sourcePort uint16, replyData []byte) {
	ap.tcpMutex.Lock()
	defer ap.tcpMutex.Unlock()

	if ap.tcpTransport == nil {
		return
	}

	// Pack up the reply
	replyMsg := make([]byte, 6+len(replyData))
	copy(replyMsg[0:4], sourceAddr)
	binary.BigEndian.PutUint16(replyMsg[4:6], sourcePort)
	copy(replyMsg[6:], replyData)

	// Encode and send
	encoded := ap.codec.Encode(replyMsg)
	_, err := ap.tcpTransport.Write(encoded)
	if err != nil {
		slog.Error("Error sending reply", "err", err)
		ap.tcpTransport = nil
	}
}

