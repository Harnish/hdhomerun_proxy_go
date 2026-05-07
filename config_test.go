package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestConfigStoreGet(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HDHomeRunPort = 12345
	store := newConfigStore(cfg, "")
	got := store.Get()
	if got.HDHomeRunPort != 12345 {
		t.Errorf("Get() returned wrong port: %d", got.HDHomeRunPort)
	}
}

func TestConfigStoreSetInMemory(t *testing.T) {
	store := newConfigStore(DefaultConfig(), "")
	newCfg := DefaultConfig()
	newCfg.Debug = true
	if err := store.Set(newCfg); err != nil {
		t.Fatal(err)
	}
	if !store.Get().Debug {
		t.Error("expected Debug=true after Set")
	}
}

func TestConfigStoreSetWritesToFile(t *testing.T) {
	f, err := os.CreateTemp("", "config-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Close()

	store := newConfigStore(DefaultConfig(), f.Name())
	newCfg := DefaultConfig()
	newCfg.Debug = true
	if err := store.Set(newCfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	var readBack Config
	if err := json.Unmarshal(data, &readBack); err != nil {
		t.Fatal(err)
	}
	if !readBack.Debug {
		t.Error("expected Debug=true in written file")
	}
}

func TestConfigStoreSetNoFileOK(t *testing.T) {
	store := newConfigStore(DefaultConfig(), "")
	newCfg := DefaultConfig()
	newCfg.TCPPort = 9999
	if err := store.Set(newCfg); err != nil {
		t.Errorf("Set with empty filePath should not error: %v", err)
	}
	if store.Get().TCPPort != 9999 {
		t.Error("in-memory update failed")
	}
}

func TestConfigStoreHasFile(t *testing.T) {
	s1 := newConfigStore(DefaultConfig(), "")
	if s1.filePath != "" {
		t.Error("expected empty filePath")
	}
	s2 := newConfigStore(DefaultConfig(), "/some/path.json")
	if s2.filePath != "/some/path.json" {
		t.Error("expected filePath to be set")
	}
}
