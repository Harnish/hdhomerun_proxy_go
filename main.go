package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var debug bool
	var configFile string
	var templateMode bool

	flag.BoolVar(&debug, "debug", false, "Enable debug logging")
	flag.StringVar(&configFile, "config", "", "Path to config file (JSON)")
	flag.BoolVar(&templateMode, "template", false, "Generate a template config file and exit")
	flag.Parse()
	args := flag.Args()

	if templateMode {
		if err := SaveConfigTemplate("hdhomerun_proxy.json"); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving template: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Load configuration
	cfg, err := LoadConfig(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Override debug flag if set in config or command line
	if debug {
		cfg.Debug = true
	}

	// Initialize structured logging
	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)

	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	mode := args[0]

	switch mode {
	case "app":
		runAppProxy(args[1:], cfg)
	case "tuner":
		runTunerProxy(args[1:], cfg)
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\n", mode)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  %s app [bind_address] [hdhomerun_ip]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s tuner <app_proxy_host_or_hdhomerun_ip> [-direct]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "\nFlags:\n")
	fmt.Fprintf(os.Stderr, "  -config string\n\tPath to JSON config file\n")
	fmt.Fprintf(os.Stderr, "  -debug\n\tEnable debug logging\n")
	fmt.Fprintf(os.Stderr, "  -template\n\tGenerate a template config file and exit\n")
}

func runAppProxy(args []string, cfg *Config) {
	var bindAddr, directIP string

	if len(args) > 0 {
		bindAddr = args[0]
	}
	if len(args) > 1 {
		directIP = args[1]
	}

	// Override with config values if not provided via CLI
	if bindAddr == "" && cfg.App.BindAddress != "" {
		bindAddr = cfg.App.BindAddress
	}
	if directIP == "" && cfg.App.DirectHDHRIP != "" {
		directIP = cfg.App.DirectHDHRIP
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		slog.Info("Shutdown signal received")
		cancel()
	}()

	proxy := NewAppProxy()
	if err := proxy.Run(ctx, bindAddr, directIP, cfg); err != nil {
		slog.Error("App proxy error", "err", err)
		os.Exit(1)
	}
}

func runTunerProxy(args []string, cfg *Config) {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintf(os.Stderr, "Error: tuner mode requires host argument\n")
		fmt.Fprintf(os.Stderr, "Usage: %s tuner <app_proxy_host_or_hdhomerun_ip> [-direct]\n", os.Args[0])
		os.Exit(1)
	}

	hostOrIP := args[0]
	isDirectMode := len(args) == 2 && args[1] == "-direct"

	// Override with config values if not provided via CLI
	if hostOrIP == "" {
		if isDirectMode && cfg.Tuner.DirectHDHRIP != "" {
			hostOrIP = cfg.Tuner.DirectHDHRIP
		} else if !isDirectMode && cfg.Tuner.ProxyHost != "" {
			hostOrIP = cfg.Tuner.ProxyHost
		}
	}

	// Check config for direct mode setting
	if !isDirectMode && cfg.Tuner.DirectMode {
		isDirectMode = true
		if hostOrIP == "" && cfg.Tuner.DirectHDHRIP != "" {
			hostOrIP = cfg.Tuner.DirectHDHRIP
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		slog.Info("Shutdown signal received")
		cancel()
	}()

	proxy := NewTunerProxy()
	if err := proxy.Run(ctx, hostOrIP, isDirectMode, cfg); err != nil {
		slog.Error("Tuner proxy error", "err", err)
		os.Exit(1)
	}
}
