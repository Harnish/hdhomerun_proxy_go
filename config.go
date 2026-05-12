package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

// Config holds the configuration for the proxy
type Config struct {
	// Network settings
	HDHomeRunPort     int `json:"hdhomerun_port"`
	TCPPort           int `json:"tcp_port"`
	UDPReadTimeout    int `json:"udp_read_timeout_ms"` // milliseconds
	UDPReadBuffSize   int `json:"udp_read_buffer_size"`
	ReconnectInterval int `json:"reconnect_interval_seconds"`

	// Logging
	Debug                        bool `json:"debug"`
	LogActiveConnectionsInterval int  `json:"log_active_connections_interval_seconds"` // Log active connections at this interval (0 to disable)

	// Device emulation settings
	Device struct {
		// Model type (e.g., "HDFX-4K", "HDHR3-US")
		ModelType string `json:"model_type"`
		// Device ID (8-hex string, e.g., "1072ABCD"). Auto-generated if empty.
		DeviceID string `json:"device_id"`
		// Friendly name (e.g., "HDHomeRun FLEX 4K"). Uses default if empty.
		FriendlyName string `json:"friendly_name"`
		// Firmware version (e.g., "20250825"). Uses realistic default if empty.
		FirmwareVersion string `json:"firmware_version"`
		// Device auth token (usually a hex string, can be empty for unauth)
		DeviceAuth string `json:"device_auth"`
	} `json:"device"`

	// App proxy settings
	App struct {
		BindAddress  string `json:"bind_address"`
		DirectHDHRIP string `json:"direct_hdhomerun_ip"`
	} `json:"app"`

	// Tuner proxy settings
	Tuner struct {
		ProxyHost    string `json:"app_proxy_host"`
		DirectMode   bool   `json:"direct_mode"`
		DirectHDHRIP string `json:"direct_hdhomerun_ip"`
	} `json:"tuner"`

	// Tunarr backend settings
	Tunarr struct {
		Enabled       bool   `json:"enabled"`
		Host          string `json:"host"`
		Port          int    `json:"port"`
		UseTunarrOnly bool   `json:"use_tunarr_only"` // If true, only use Tunarr, ignore HDHR
		HttpTimeout   int    `json:"http_timeout_seconds"`
	} `json:"tunarr"`

	// Web UI settings (stored in config so credentials persist across restarts)
	WebUI struct {
		Addr string `json:"addr"`
		User string `json:"user"`
		Pass string `json:"pass"`
	} `json:"webui"`
}

// DefaultConfig returns a config with default values
func DefaultConfig() *Config {
	return &Config{
		HDHomeRunPort:                HDHomeRunDiscoveryUDPPort,
		TCPPort:                      TCPPort,
		UDPReadTimeout:               UDPReadTimeout,
		UDPReadBuffSize:              UDPReadBufferSize,
		ReconnectInterval:            ReconnectInterval,
		Debug:                        false,
		LogActiveConnectionsInterval: 0,
	}
}

// LoadConfig loads configuration from a JSON file
// Falls back to defaults if file doesn't exist or there are errors
func LoadConfig(filepath string) (*Config, error) {
	cfg := DefaultConfig()

	// If no filepath provided, just return defaults
	if filepath == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("Config file not found, using defaults", "path", filepath)
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	slog.Info("Config loaded", "path", filepath)
	return cfg, nil
}

// SaveConfigTemplate saves a template config file for reference
func SaveConfigTemplate(filepath string) error {
	template := &Config{
		HDHomeRunPort:                65001,
		TCPPort:                      65001,
		UDPReadTimeout:               500,
		UDPReadBuffSize:              4096,
		ReconnectInterval:            3,
		Debug:                        false,
		LogActiveConnectionsInterval: 60, // Log every 60 seconds
	}

	template.Device.ModelType = "HDFX-4K"
	template.Device.DeviceID = "" // Will auto-generate if empty
	template.Device.FriendlyName = "HDHomeRun FLEX 4K"
	template.Device.FirmwareVersion = "20250825"
	template.Device.DeviceAuth = ""

	template.App.BindAddress = "0.0.0.0"
	template.App.DirectHDHRIP = "192.168.1.50"
	template.Tuner.ProxyHost = "10.10.10.9"
	template.Tuner.DirectMode = false
	template.Tuner.DirectHDHRIP = "10.10.10.50"
	template.Tunarr.Enabled = false
	template.Tunarr.Host = "tunarr.local"
	template.Tunarr.Port = 8000
	template.Tunarr.UseTunarrOnly = false
	template.Tunarr.HttpTimeout = 5

	data, err := json.MarshalIndent(template, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write template config: %w", err)
	}

	fmt.Printf("Template config saved to %s\n", filepath)
	return nil
}

// ApplyConfig applies config values to the global constants
// This is done by returning the config and using it in the functions
func (c *Config) GetUDPReadTimeout() int {
	if c.UDPReadTimeout > 0 {
		return c.UDPReadTimeout
	}
	return UDPReadTimeout
}

func (c *Config) GetUDPReadBuffSize() int {
	if c.UDPReadBuffSize > 0 {
		return c.UDPReadBuffSize
	}
	return UDPReadBufferSize
}

func (c *Config) GetReconnectInterval() int {
	if c.ReconnectInterval > 0 {
		return c.ReconnectInterval
	}
	return ReconnectInterval
}

func (c *Config) GetHDHomeRunPort() int {
	if c.HDHomeRunPort > 0 {
		return c.HDHomeRunPort
	}
	return HDHomeRunDiscoveryUDPPort
}

func (c *Config) GetTCPPort() int {
	if c.TCPPort > 0 {
		return c.TCPPort
	}
	return TCPPort
}

// configStore holds a live *Config protected by a mutex.
// filePath is the file the config was loaded from; empty means no backing file.
type configStore struct {
	mu       sync.RWMutex
	cfg      *Config
	filePath string
}

func newConfigStore(cfg *Config, filePath string) *configStore {
	return &configStore{cfg: cfg, filePath: filePath}
}

// Get returns the current config. Callers must not mutate the returned pointer.
func (cs *configStore) Get() *Config {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.cfg
}

// Set saves newCfg to disk (if filePath is set) and updates the in-memory config.
func (cs *configStore) Set(newCfg *Config) error {
	cs.mu.Lock()
	if cs.filePath != "" {
		data, err := json.MarshalIndent(newCfg, "", "  ")
		if err != nil {
			cs.mu.Unlock()
			return fmt.Errorf("failed to marshal config: %w", err)
		}
		if err := os.WriteFile(cs.filePath, data, 0644); err != nil {
			cs.mu.Unlock()
			return fmt.Errorf("failed to write config: %w", err)
		}
	}
	cs.cfg = newCfg
	cs.mu.Unlock()
	cs.ApplyLive(newCfg)
	return nil
}

// ApplyLive applies the debug/log level immediately without requiring a restart.
// Skipped when the TUI handler is active (it ignores level filtering already).
func (cs *configStore) ApplyLive(newCfg *Config) {
	if _, isTUI := slog.Default().Handler().(*tuiHandler); isTUI {
		return
	}
	level := slog.LevelInfo
	if newCfg.Debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))
}
