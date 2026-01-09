package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// TunarrBackend communicates with a Tunarr server via HTTP API
type TunarrBackend struct {
	host       string
	port       int
	baseURL    string
	httpClient *http.Client
}

// TunarrDiscoverResponse matches Tunarr's discover.json endpoint
type TunarrDiscoverResponse struct {
	FriendlyName    string `json:"FriendlyName"`
	ModelNumber     string `json:"ModelNumber"`
	Manufacturer    string `json:"Manufacturer"`
	ManufacturerURL string `json:"ManufacturerURL"`
	BaseURL         string `json:"BaseURL"`
	LineupURL       string `json:"LineupURL"`
	TunerCount      int    `json:"TunerCount"`
}

// TunarrLineupItem represents a channel in Tunarr's lineup
type TunarrLineupItem struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	HDHRNumber  string `json:"HDHRNumber"`
}

// TunarrTunerStatus represents a tuner's status
type TunarrTunerStatus struct {
	TunerIndex int    `json:"TunerIndex"`
	Status     string `json:"Status"`
	Channel    string `json:"Channel"`
	SessionID  string `json:"SessionID"`
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

// IsAvailable checks if Tunarr server is reachable
func (tb *TunarrBackend) IsAvailable(ctx context.Context) bool {
	discoverURL := fmt.Sprintf("%s/discover.json", tb.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", discoverURL, nil)
	if err != nil {
		return false
	}

	resp, err := tb.httpClient.Do(req)
	if err != nil {
		slog.Debug("Tunarr server not available", "err", err)
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// GetDiscoverInfo retrieves device info from Tunarr
func (tb *TunarrBackend) GetDiscoverInfo(ctx context.Context) (*TunarrDiscoverResponse, error) {
	discoverURL := fmt.Sprintf("%s/discover.json", tb.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", discoverURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := tb.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch discover info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tunarr returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var info TunarrDiscoverResponse
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse discover response: %w", err)
	}

	return &info, nil
}

// GetLineup retrieves channel lineup from Tunarr
func (tb *TunarrBackend) GetLineup(ctx context.Context) ([]TunarrLineupItem, error) {
	lineupURL := fmt.Sprintf("%s/lineup.json", tb.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", lineupURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := tb.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch lineup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tunarr returned status %d", resp.StatusCode)
	}

	var lineup []TunarrLineupItem
	if err := json.NewDecoder(resp.Body).Decode(&lineup); err != nil {
		return nil, fmt.Errorf("failed to parse lineup: %w", err)
	}

	return lineup, nil
}

// GetLineupStatus retrieves lineup status from Tunarr
func (tb *TunarrBackend) GetLineupStatus(ctx context.Context) (map[string]interface{}, error) {
	statusURL := fmt.Sprintf("%s/lineup_status.json", tb.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := tb.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch lineup status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tunarr returned status %d", resp.StatusCode)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to parse lineup status: %w", err)
	}

	return status, nil
}

// GetTunerStatus retrieves status of a specific tuner
func (tb *TunarrBackend) GetTunerStatus(ctx context.Context, tunerIndex int) (*TunarrTunerStatus, error) {
	statusURL := fmt.Sprintf("%s/tuner%d/status.json", tb.baseURL, tunerIndex)

	req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := tb.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tuner status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tunarr returned status %d", resp.StatusCode)
	}

	var status TunarrTunerStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to parse tuner status: %w", err)
	}

	return &status, nil
}

// GetStreamURL returns the streaming URL for a channel
func (tb *TunarrBackend) GetStreamURL(channel string) string {
	// Tunarr typically uses /watch/<channel>
	return fmt.Sprintf("%s/watch/%s", tb.baseURL, channel)
}

// BuildDiscoveryResponse creates a discovery response that can be sent back to apps
// This mimics HDHomeRun discovery but points to Tunarr
func (tb *TunarrBackend) BuildDiscoveryResponse(ctx context.Context, tunarr *TunarrDiscoverResponse) string {
	response := fmt.Sprintf("Device: Tunarr-HDHR (Tunarr)\\r\\n")
	response += fmt.Sprintf("DeviceAuth: tunarr-spoofed\\r\\n")
	response += fmt.Sprintf("BaseURL: %s\\r\\n", tb.baseURL)
	response += fmt.Sprintf("FirmwareName: tunarr\\r\\n")
	response += fmt.Sprintf("FirmwareVersion: 1.0\\r\\n")
	response += fmt.Sprintf("LineupURL: %s/lineup.json\\r\\n", tb.baseURL)
	response += fmt.Sprintf("TunerCount: %d\\r\\n", tunarr.TunerCount)

	return response
}

// ConvertLineupToHDHRFormat converts Tunarr lineup to pseudo-HDHR discovery format
// This is for raw discovery protocol responses
func ConvertLineupToHDHRFormat(lineup []TunarrLineupItem) string {
	// For now, we'll return a simplified format
	// In practice, this would be more complex to fully emulate HDHomeRun
	result := ""
	for i, item := range lineup {
		result += fmt.Sprintf("Channel: %s\\r\\n", item.GuideNumber)
		result += fmt.Sprintf("Guide: %s\\r\\n", item.GuideName)
		if i < len(lineup)-1 {
			result += "---\\r\\n"
		}
	}
	return result
}

// BuildHDHRDiscoveryPacket creates an HDHomeRun-like discovery response from Tunarr data
// This can be sent back via UDP to make Tunarr appear as an HDHR device
func BuildHDHRDiscoveryPacket(tunarrInfo *TunarrDiscoverResponse, tunarPort int, srcIP string) []byte {
	// HDHR discovery response format (simplified key:value pairs)
	response := fmt.Sprintf("Device: HDHR3-US\\r\n")
	response += fmt.Sprintf("DeviceAuth: 00000000\\r\n")
	response += fmt.Sprintf("BaseURL: http://%s:%d\\r\n", srcIP, tunarPort)
	response += fmt.Sprintf("LineupURL: http://%s:%d/lineup.json\\r\n", srcIP, tunarPort)
	response += fmt.Sprintf("TunerCount: %d\\r\n", tunarrInfo.TunerCount)
	response += fmt.Sprintf("BaseURL: http://%s:%d\\r\n", srcIP, tunarPort)
	response += fmt.Sprintf("FirmwareName: http_live\\r\n")
	response += fmt.Sprintf("FirmwareVersion: 20191217\\r\n")
	response += fmt.Sprintf("FriendlyName: Tunarr\\r\n")

	return []byte(response)
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
