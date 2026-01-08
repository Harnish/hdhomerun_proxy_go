package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"sync"
	"time"
)

// TunerProxy acts like an HDHomeRun tuner
type TunerProxy struct {
	codec        *MessageCodec
	tcpTransport net.Conn
	tcpMutex     sync.Mutex
	udpTransport *net.UDPConn
	udpMutex     sync.Mutex
	directHDHRIP string // If set, connect directly to HDHomeRun instead of app proxy
}

// NewTunerProxy creates a new TunerProxy
func NewTunerProxy() *TunerProxy {
	return &TunerProxy{
		codec: NewMessageCodec(),
	}
}

// Run starts the tuner proxy
// appProxyHostOrIP: app proxy hostname (tuner proxy mode) or HDHomeRun IP (direct mode)
// isDirectMode: if true, appProxyHostOrIP is treated as direct HDHomeRun IP
// cfg: configuration object for tuning parameters
func (tp *TunerProxy) Run(ctx context.Context, appProxyHostOrIP string, isDirectMode bool, cfg *Config) error {
	if isDirectMode {
		tp.directHDHRIP = appProxyHostOrIP
		return tp.runDirectMode(ctx, cfg)
	} else {
		return tp.runTunerProxyMode(ctx, appProxyHostOrIP, cfg)
	}
}

// runDirectMode listens for UDP broadcasts and proxies directly to the HDHomeRun
func (tp *TunerProxy) runDirectMode(ctx context.Context, cfg *Config) error {
	// Create UDP listener for broadcast packets
	var bindAddr string
	if runtime.GOOS == "windows" {
		bindAddr = "0.0.0.0"
	} else {
		bindAddr = "255.255.255.255"
	}

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", bindAddr, HDHomeRunDiscoveryUDPPort))
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	udpConn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP: %w", err)
	}
	defer udpConn.Close()

	tp.udpMutex.Lock()
	tp.udpTransport = udpConn
	tp.udpMutex.Unlock()

	slog.Info("Tuner proxy listening for broadcasts (direct mode)", "bind_addr", bindAddr, "direct_hdhomerun_ip", tp.directHDHRIP)

	buf := make([]byte, UDPReadBufferSize)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		udpConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, remoteAddr, err := udpConn.ReadFromUDP(buf)
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
			ip := remoteAddr.IP.String()
			port := remoteAddr.Port
			slog.Debug("Request received from app (direct mode)", "bytes", n, "source", fmt.Sprintf("%s:%d", ip, port))

			// Forward the query directly to the HDHomeRun and reply back
			go tp.forwardToDirectHDHR(buf[:n], remoteAddr, udpConn)
		}
	}
}

// forwardToDirectHDHR sends a query to the HDHomeRun and replies back to the app
func (tp *TunerProxy) forwardToDirectHDHR(queryData []byte, appAddr *net.UDPAddr, replyConn *net.UDPConn) {
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

	// Send query to HDHomeRun
	_, err = conn.Write(queryData)
	if err != nil {
		slog.Error("Error sending query to HDHomeRun", "err", err)
		return
	}

	// Wait for response
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

		// Send response back to the original app
		_, err := replyConn.WriteToUDP(respBuf[:n], appAddr)
		if err != nil {
			slog.Error("Error sending response to app", "err", err)
		}
	}
}

// runTunerProxyMode connects to app proxy and relays broadcasts
func (tp *TunerProxy) runTunerProxyMode(ctx context.Context, appProxyHost string, cfg *Config) error {
	// Create UDP listener for broadcast packets
	var bindAddr string
	if runtime.GOOS == "windows" {
		bindAddr = "0.0.0.0"
	} else {
		bindAddr = "255.255.255.255"
	}

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", bindAddr, HDHomeRunDiscoveryUDPPort))
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	udpConn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP: %w", err)
	}
	defer udpConn.Close()

	tp.udpMutex.Lock()
	tp.udpTransport = udpConn
	tp.udpMutex.Unlock()

	slog.Info("Tuner proxy listening for broadcasts", "addr", bindAddr, "port", HDHomeRunDiscoveryUDPPort)

	// Start UDP listener goroutine
	go tp.handleUDPBroadcasts(ctx)

	// Keep trying to connect to app proxy
	ticker := time.NewTicker(time.Duration(cfg.GetReconnectInterval()) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			tp.closeTCP()
			return nil
		case <-ticker.C:
			if tp.getTCPTransport() == nil {
				slog.Info("Connecting to app proxy", "host", appProxyHost)
				if err := tp.connectToAppProxy(ctx, appProxyHost); err != nil {
					slog.Error("Failed to connect to app proxy", "err", err)
					if opErr, ok := err.(*net.OpError); ok {
						if opErr.Err.Error() == "no such host" {
							slog.Error("Unknown host", "host", appProxyHost)
							os.Exit(1)
						}
					}
					continue
				}
			}
		}
	}
}

// getTCPTransport safely gets the TCP transport
func (tp *TunerProxy) getTCPTransport() net.Conn {
	tp.tcpMutex.Lock()
	defer tp.tcpMutex.Unlock()
	return tp.tcpTransport
}

// setTCPTransport safely sets the TCP transport
func (tp *TunerProxy) setTCPTransport(conn net.Conn) {
	tp.tcpMutex.Lock()
	defer tp.tcpMutex.Unlock()
	tp.tcpTransport = conn
}

// closeTCP safely closes the TCP transport
func (tp *TunerProxy) closeTCP() {
	tp.tcpMutex.Lock()
	defer tp.tcpMutex.Unlock()
	if tp.tcpTransport != nil {
		tp.tcpTransport.Close()
		tp.tcpTransport = nil
	}
}

// connectToAppProxy connects to the app proxy and handles the connection
func (tp *TunerProxy) connectToAppProxy(ctx context.Context, appProxyHost string) error {
	addr := net.JoinHostPort(appProxyHost, fmt.Sprintf("%d", TCPPort))
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}

	tp.setTCPTransport(conn)
	peername := conn.RemoteAddr()
	slog.Info("Connected to app proxy", "addr", peername)

	// Handle the connection in a separate goroutine
	go func() {
		codec := NewMessageCodec()
		buf := make([]byte, UDPReadBufferSize)

		for {
			select {
			case <-ctx.Done():
				tp.closeTCP()
				return
			default:
			}

			n, err := conn.Read(buf)
			if err != nil {
				slog.Info("Disconnected from app proxy")
				tp.closeTCP()
				return
			}

			if n > 0 {
				slog.Debug("Reply received from app proxy", "bytes", n)
				codec.Decode(buf[:n], tp.onMessageReceivedFromAppProxy)
			}
		}
	}()

	return nil
}

// handleUDPBroadcasts handles incoming broadcast packets
func (tp *TunerProxy) handleUDPBroadcasts(ctx context.Context) {
	buf := make([]byte, 4096)
	codec := NewMessageCodec()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		udpConn := func() *net.UDPConn {
			tp.udpMutex.Lock()
			defer tp.udpMutex.Unlock()
			return tp.udpTransport
		}()

		if udpConn == nil {
			return
		}

		n, remoteAddr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				slog.Error("Error reading UDP", "err", err)
			}
			continue
		}

		// Ignore datagrams until TCP is connected
		if tp.getTCPTransport() == nil {
			continue
		}

		if n > 0 {
			ip := remoteAddr.IP.String()
			port := remoteAddr.Port
			slog.Debug("Request received from app", "bytes", n, "ip", ip, "port", port)

			// Package into a message with source address and port
			msgData := make([]byte, 6+n)
			copy(msgData[0:4], remoteAddr.IP.To4())
			binary.BigEndian.PutUint16(msgData[4:6], uint16(port))
			copy(msgData[6:], buf[:n])

			// Encode and send to app proxy
			encodedMsg := codec.Encode(msgData)

			tcpConn := tp.getTCPTransport()
			if tcpConn != nil {
				_, err := tcpConn.Write(encodedMsg)
				if err != nil {
					slog.Error("Error sending to app proxy", "err", err)
					tp.closeTCP()
				}
			}
		}
	}
}

// onMessageReceivedFromAppProxy handles a message from the app proxy
func (tp *TunerProxy) onMessageReceivedFromAppProxy(msg []byte) {
	if len(msg) < 6 {
		slog.Warn("Invalid message: too short", "len", len(msg))
		return
	}

	// Unpack the message
	sourceAddr := msg[0:4]
	sourcePort := binary.BigEndian.Uint16(msg[4:6])
	replyData := msg[6:]

	// Convert IP bytes to string
	ip := net.IPv4(sourceAddr[0], sourceAddr[1], sourceAddr[2], sourceAddr[3]).String()

	slog.Debug("Replying to app", "bytes", len(replyData), "ip", ip, "port", sourcePort)

	// Send reply back to the app
	addr := &net.UDPAddr{
		IP:   net.IPv4(sourceAddr[0], sourceAddr[1], sourceAddr[2], sourceAddr[3]),
		Port: int(sourcePort),
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		slog.Error("Error sending reply", "err", err)
		return
	}
	defer conn.Close()

	_, err = conn.Write(replyData)
	if err != nil {
		slog.Error("Error sending reply", "err", err)
	}
}
