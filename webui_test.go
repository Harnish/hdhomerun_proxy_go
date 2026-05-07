package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mockStatsProvider struct{ stats ProxyStats }

func (m *mockStatsProvider) Stats() ProxyStats { return m.stats }

func makeTestServer(t *testing.T) (*webServer, *httptest.Server) {
	t.Helper()
	store := newConfigStore(DefaultConfig(), "")
	ws := newWebServer(store, &mockStatsProvider{
		stats: ProxyStats{Name: "TestProxy", ActiveUDP: 2, ActiveDial: 1},
	})
	srv := httptest.NewServer(ws.handler("testuser", "testpass"))
	t.Cleanup(srv.Close)
	return ws, srv
}

func TestWebServerNoAuth(t *testing.T) {
	_, srv := makeTestServer(t)
	resp, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWebServerWrongAuth(t *testing.T) {
	_, srv := makeTestServer(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/stats", nil)
	req.SetBasicAuth("wrong", "creds")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWebServerStats(t *testing.T) {
	_, srv := makeTestServer(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/stats", nil)
	req.SetBasicAuth("testuser", "testpass")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var s ProxyStats
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	if s.ActiveUDP != 2 || s.ActiveDial != 1 {
		t.Errorf("unexpected stats: %+v", s)
	}
}

func TestWebServerLogs(t *testing.T) {
	resetLogRingBuf()
	_, srv := makeTestServer(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/logs", nil)
	req.SetBasicAuth("testuser", "testpass")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var entries []logEntryJSON
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if entries == nil {
		t.Error("expected non-nil array")
	}
}

func TestWebServerGetConfig(t *testing.T) {
	_, srv := makeTestServer(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/config", nil)
	req.SetBasicAuth("testuser", "testpass")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var cr configResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		t.Fatal(err)
	}
	if cr.Config == nil {
		t.Error("expected non-nil config")
	}
	if cr.HasFile {
		t.Error("expected HasFile=false for no-file store")
	}
}

func TestWebServerPostConfig(t *testing.T) {
	_, srv := makeTestServer(t)
	newCfg := DefaultConfig()
	newCfg.Debug = true
	body, _ := json.Marshal(newCfg)
	req, _ := http.NewRequest("POST", srv.URL+"/api/config", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("testuser", "testpass")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result["ok"] {
		t.Error("expected ok=true")
	}
}
