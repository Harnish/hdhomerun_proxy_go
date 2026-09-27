package main

import (
	"testing"
)

func TestBackendRouterStatsBasic(t *testing.T) {
	br := backendRouter{
		name:         "AppProxy",
		directHDHRIP: "192.168.1.50",
	}
	s := br.Stats()
	if s.Name != "AppProxy" {
		t.Errorf("expected Name=AppProxy, got %q", s.Name)
	}
	if s.DirectHDHRIP != "192.168.1.50" {
		t.Errorf("expected DirectHDHRIP=192.168.1.50, got %q", s.DirectHDHRIP)
	}
	if s.TunarrConfigured {
		t.Error("expected TunarrConfigured=false when tunarr is nil")
	}
	if s.TunarrPort != 0 {
		t.Errorf("expected TunarrPort=0, got %d", s.TunarrPort)
	}
}

func TestBackendRouterStatsTunarr(t *testing.T) {
	br := backendRouter{
		name:   "TunerProxy",
		tunarr: &TunarrBackend{port: 8000},
	}
	s := br.Stats()
	if !s.TunarrConfigured {
		t.Error("expected TunarrConfigured=true when tunarr != nil")
	}
	if s.TunarrPort != 8000 {
		t.Errorf("expected TunarrPort=8000, got %d", s.TunarrPort)
	}
}

func TestBackendRouterStatsConnectionCounts(t *testing.T) {
	br := backendRouter{name: "AppProxy"}
	br.activeConnectionsMutex.Lock()
	br.activeUDPConnections = 3
	br.activeDialConnections = 1
	br.activeConnectionsMutex.Unlock()
	s := br.Stats()
	if s.ActiveUDP != 3 {
		t.Errorf("expected ActiveUDP=3, got %d", s.ActiveUDP)
	}
	if s.ActiveDial != 1 {
		t.Errorf("expected ActiveDial=1, got %d", s.ActiveDial)
	}
}

func TestBackendRouterStatsHealth(t *testing.T) {
	br := backendRouter{name: "AppProxy"}
	s := br.Stats()
	if s.HDHR.State != "unknown" || s.HDHRTarget != "" || s.LinkExpected {
		t.Fatalf("fresh router: got HDHR=%q target=%q linkExpected=%v", s.HDHR.State, s.HDHRTarget, s.LinkExpected)
	}

	br.setLink(false, "")
	if s = br.Stats(); s.HDHRTarget != "broadcast" || s.LinkUp {
		t.Errorf("AppProxy awaiting peer: target=%q linkUp=%v", s.HDHRTarget, s.LinkUp)
	}
	br.setLink(true, "10.0.0.2:5555")
	if s = br.Stats(); !s.LinkUp || s.LinkPeer != "10.0.0.2:5555" {
		t.Errorf("link up: got up=%v peer=%q", s.LinkUp, s.LinkPeer)
	}

	br.recordHDHR("no reply")
	if s = br.Stats(); s.HDHR.State != "fail" || s.HDHR.Err != "no reply" {
		t.Errorf("after failure: %+v", s.HDHR)
	}
	br.recordHDHR("")
	if s = br.Stats(); s.HDHR.State != "ok" || s.HDHR.Err != "" {
		t.Errorf("after success: %+v", s.HDHR)
	}
}
