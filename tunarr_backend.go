package main

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// TunarrBackend is the HTTP client for a Tunarr server. Its channel catalog,
// streaming, and guide passthrough live in tunarr_catalog.go.
type TunarrBackend struct {
	host       string
	port       int
	baseURL    string
	httpClient *http.Client
	catalog    tunarrCatalog
}

// NewTunarrBackend creates a new Tunarr backend client
func NewTunarrBackend(host string, port int, timeout int) *TunarrBackend {
	if port == 0 {
		port = 8000
	}
	if timeout == 0 {
		timeout = 5
	}

	return &TunarrBackend{
		host:    host,
		port:    port,
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}
}

// GetLocalIPForConnection returns the local IP address that would be used to connect to a given address
// This is useful for building responses with the correct source IP
func GetLocalIPForConnection(remoteAddr string) (string, error) {
	conn, err := net.Dial("udp", remoteAddr)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}
