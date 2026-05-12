package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockHDHRStatsProvider for testing HDHR endpoints
type mockHDHRStatsProvider struct {
	stats ProxyStats
}

func (m *mockHDHRStatsProvider) Stats() ProxyStats {
	return m.stats
}

func TestDiscoverJSONEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Device.ModelType = "HDFX-4K"
	cfg.Device.DeviceID = "1072ABCD"
	cfg.Device.FriendlyName = "Test Device"
	cfg.Device.FirmwareVersion = "20250825"

	store := newConfigStore(cfg, "")
	mockStats := &mockHDHRStatsProvider{
		stats: ProxyStats{
			Name:             "TestProxy",
			TunarrConfigured: false,
			ActiveUDP:        0,
			ActiveDial:       0,
		},
	}

	server := NewHDHREndpointServer(store, mockStats)
	handler := server.Handler()

	req := httptest.NewRequest("GET", "/discover.json", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response DiscoverJSONResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	if response.DeviceID != "1072ABCD" {
		t.Errorf("Expected DeviceID '1072ABCD', got '%s'", response.DeviceID)
	}
	if response.FriendlyName != "Test Device" {
		t.Errorf("Expected FriendlyName 'Test Device', got '%s'", response.FriendlyName)
	}
	if response.FirmwareVersion != "20250825" {
		t.Errorf("Expected FirmwareVersion '20250825', got '%s'", response.FirmwareVersion)
	}
	if response.TunerCount != 4 {
		t.Errorf("Expected TunerCount 4, got %d", response.TunerCount)
	}
}

func TestLineupStatusEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	store := newConfigStore(cfg, "")
	mockStats := &mockHDHRStatsProvider{
		stats: ProxyStats{
			Name:             "TestProxy",
			TunarrConfigured: false,
		},
	}

	server := NewHDHREndpointServer(store, mockStats)
	handler := server.Handler()

	req := httptest.NewRequest("GET", "/lineup_status.json", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response LineupStatusJSON
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	if response.ScanInProgress != 0 {
		t.Errorf("Expected ScanInProgress 0, got %d", response.ScanInProgress)
	}
	if response.ScanPossible != 1 {
		t.Errorf("Expected ScanPossible 1, got %d", response.ScanPossible)
	}
	if response.Source != "Cable" {
		t.Errorf("Expected Source 'Cable', got '%s'", response.Source)
	}
}

func TestLineupJSONEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	store := newConfigStore(cfg, "")
	mockStats := &mockHDHRStatsProvider{
		stats: ProxyStats{
			Name:             "TestProxy",
			TunarrConfigured: false,
		},
	}

	server := NewHDHREndpointServer(store, mockStats)
	handler := server.Handler()

	req := httptest.NewRequest("GET", "/lineup.json", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var lineup []LineupItemJSON
	if err := json.NewDecoder(w.Body).Decode(&lineup); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	// Direct HDHR mode should return empty lineup
	if len(lineup) != 0 {
		t.Errorf("Expected empty lineup for direct HDHR mode, got %d items", len(lineup))
	}
}

func TestTunerStatusEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	store := newConfigStore(cfg, "")
	mockStats := &mockHDHRStatsProvider{
		stats: ProxyStats{
			Name:             "TestProxy",
			TunarrConfigured: false,
		},
	}

	server := NewHDHREndpointServer(store, mockStats)
	handler := server.Handler()

	req := httptest.NewRequest("GET", "/tuner0/status", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var status TunerStatusJSON
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	if status.Status != "idle" {
		t.Errorf("Expected Status 'idle', got '%s'", status.Status)
	}
	if status.TunerIndex != 0 {
		t.Errorf("Expected TunerIndex 0, got %d", status.TunerIndex)
	}
}

func TestDeviceXMLEndpoint(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Device.ModelType = "HDFX-4K"
	cfg.Device.FriendlyName = "Test Device"

	store := newConfigStore(cfg, "")
	mockStats := &mockHDHRStatsProvider{
		stats: ProxyStats{
			Name:             "TestProxy",
			TunarrConfigured: false,
		},
	}

	server := NewHDHREndpointServer(store, mockStats)
	handler := server.Handler()

	req := httptest.NewRequest("GET", "/device.xml", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/xml; charset=utf-8" {
		t.Errorf("Expected Content-Type 'application/xml; charset=utf-8', got '%s'", contentType)
	}

	// Just verify it contains XML elements
	body := w.Body.String()
	if !contains(body, "<root") {
		t.Errorf("Expected XML root element in response")
	}
}

func TestMethodNotAllowedOnEndpoints(t *testing.T) {
	cfg := DefaultConfig()
	store := newConfigStore(cfg, "")
	mockStats := &mockHDHRStatsProvider{
		stats: ProxyStats{
			Name:             "TestProxy",
			TunarrConfigured: false,
		},
	}

	server := NewHDHREndpointServer(store, mockStats)
	handler := server.Handler()

	tests := []string{
		"/discover.json",
		"/lineup.json",
		"/lineup_status.json",
		"/device.xml",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("POST", path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status 405 for POST to %s, got %d", path, w.Code)
			}
		})
	}
}

func TestGetDeviceConfigAutoGeneration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Device.ModelType = "HDFX-4K"
	cfg.Device.DeviceID = "" // Empty, should auto-generate

	store := newConfigStore(cfg, "")
	mockStats := &mockHDHRStatsProvider{
		stats: ProxyStats{
			Name:             "TestProxy",
			TunarrConfigured: false,
		},
	}

	server := NewHDHREndpointServer(store, mockStats)
	deviceCfg := server.getDeviceConfig(context.Background())

	if deviceCfg.DeviceID == "" {
		t.Errorf("Expected auto-generated DeviceID, got empty string")
	}

	if !IsValidDeviceIDFormat(deviceCfg.DeviceID) {
		t.Errorf("Generated DeviceID is not in valid format: %s", deviceCfg.DeviceID)
	}

	if len(deviceCfg.DeviceID) != 8 {
		t.Errorf("Expected DeviceID length 8, got %d", len(deviceCfg.DeviceID))
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
