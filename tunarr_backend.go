package main

import (
	"fmt"
	"net"
	"net/http"
	"strings"
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
// host is a bare hostname (port defaults to 8000) or a URL such as
// "https://tunarr.example" (port defaults to the scheme's).
func NewTunarrBackend(host string, port int, timeout int) *TunarrBackend {
	if timeout == 0 {
		timeout = 5
	}

	var baseURL string
	if strings.Contains(host, "://") {
		baseURL = strings.TrimRight(host, "/")
		if port != 0 {
			baseURL = fmt.Sprintf("%s:%d", baseURL, port)
		}
	} else {
		if port == 0 {
			port = 8000
		}
		baseURL = fmt.Sprintf("http://%s:%d", host, port)
	}

	return &TunarrBackend{
		host:    host,
		port:    port,
		baseURL: baseURL,
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
