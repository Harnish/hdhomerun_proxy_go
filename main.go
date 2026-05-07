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
	var tuiMode bool
	flag.BoolVar(&tuiMode, "tui", false, "Enable terminal UI (disables plain log output)")
	var webuiAddr, webuiUser, webuiPass string
	flag.StringVar(&webuiAddr, "webui", "", "Bind address for web UI (e.g. :8080)")
	flag.StringVar(&webuiUser, "webui-user", "", "HTTP Basic Auth username (required with -webui)")
	flag.StringVar(&webuiPass, "webui-pass", "", "HTTP Basic Auth password (required with -webui)")
	flag.Parse()
	args := flag.Args()

	if webuiAddr != "" && (webuiUser == "" || webuiPass == "") {
		fmt.Fprintf(os.Stderr, "Error: -webui-user and -webui-pass are required when -webui is set\n")
		os.Exit(1)
	}

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

	store := newConfigStore(cfg, configFile)

	// If webui is active without TUI, install tuiHandler{nil} so log entries
	// reach the ring buffer (served at /api/logs) and still appear on stderr.
	if webuiAddr != "" && !tuiMode {
		slog.SetDefault(slog.New(newTuiHandler(nil)))
	}

	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	mode := args[0]

	switch mode {
	case "app":
		runAppProxy(args[1:], store, tuiMode, webuiAddr, webuiUser, webuiPass)
	case "tuner":
		runTunerProxy(args[1:], store, tuiMode, webuiAddr, webuiUser, webuiPass)
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
	fmt.Fprintf(os.Stderr, "  -tui\n\tEnable terminal UI dashboard\n")
	fmt.Fprintf(os.Stderr, "  -webui string\n\tBind address for web UI (e.g. :8080)\n")
	fmt.Fprintf(os.Stderr, "  -webui-user string\n\tHTTP Basic Auth username (required with -webui)\n")
	fmt.Fprintf(os.Stderr, "  -webui-pass string\n\tHTTP Basic Auth password (required with -webui)\n")
	fmt.Fprintf(os.Stderr, "\nNote: Tunarr backend can be enabled via config file (-config)\n")
	fmt.Fprintf(os.Stderr, "Generate template with: %s -template\n", os.Args[0])
}

func runAppProxy(args []string, store *configStore, tuiMode bool, webuiAddr, webuiUser, webuiPass string) {
	cfg := store.Get()
	var bindAddr, directIP string

	if len(args) > 0 {
		bindAddr = args[0]
	}
	if len(args) > 1 {
		directIP = args[1]
	}

	if bindAddr == "" && cfg.App.BindAddress != "" {
		bindAddr = cfg.App.BindAddress
	}
	if directIP == "" && cfg.App.DirectHDHRIP != "" {
		directIP = cfg.App.DirectHDHRIP
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		slog.Info("Shutdown signal received")
		cancel()
	}()

	proxy := NewAppProxy()

	if webuiAddr != "" {
		ws := newWebServer(store, proxy)
		go func() {
			if err := ws.start(ctx, webuiAddr, webuiUser, webuiPass); err != nil {
				slog.Error("Web UI error", "err", err)
			}
		}()
	}

	if tuiMode {
		runWithTUI(ctx, cancel, proxy, func() error {
			return proxy.Run(ctx, bindAddr, directIP, store)
		})
		return
	}

	if err := proxy.Run(ctx, bindAddr, directIP, store); err != nil {
		slog.Error("App proxy error", "err", err)
		os.Exit(1)
	}
}

func runTunerProxy(args []string, store *configStore, tuiMode bool, webuiAddr, webuiUser, webuiPass string) {
	cfg := store.Get()
	if len(args) > 2 {
		fmt.Fprintf(os.Stderr, "Error: too many arguments for tuner mode\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [-flags] tuner [<host>] [-direct]\n", os.Args[0])
		os.Exit(1)
	}

	// Detect flags placed after the mode (e.g. "tuner -config file.json") — flag
	// parsing stops at the first non-flag word, so they land in args instead.
	if len(args) >= 1 && len(args[0]) > 0 && args[0][0] == '-' {
		fmt.Fprintf(os.Stderr, "Error: flags must appear before the mode, e.g.:\n")
		fmt.Fprintf(os.Stderr, "  %s -config file.json tuner <host>\n", os.Args[0])
		os.Exit(1)
	}

	var hostOrIP string
	isDirectMode := false
	if len(args) >= 1 {
		hostOrIP = args[0]
	}
	if len(args) == 2 {
		isDirectMode = args[1] == "-direct"
	}

	if hostOrIP == "" {
		if isDirectMode && cfg.Tuner.DirectHDHRIP != "" {
			hostOrIP = cfg.Tuner.DirectHDHRIP
		} else if !isDirectMode && cfg.Tuner.ProxyHost != "" {
			hostOrIP = cfg.Tuner.ProxyHost
		}
	}

	if !isDirectMode && cfg.Tuner.DirectMode {
		isDirectMode = true
		if hostOrIP == "" && cfg.Tuner.DirectHDHRIP != "" {
			hostOrIP = cfg.Tuner.DirectHDHRIP
		}
	}

	if hostOrIP == "" {
		fmt.Fprintf(os.Stderr, "Error: no host specified and none found in config\n")
		fmt.Fprintf(os.Stderr, "Usage: %s [-config file.json] tuner <host> [-direct]\n", os.Args[0])
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		slog.Info("Shutdown signal received")
		cancel()
	}()

	proxy := NewTunerProxy()

	if webuiAddr != "" {
		ws := newWebServer(store, proxy)
		go func() {
			if err := ws.start(ctx, webuiAddr, webuiUser, webuiPass); err != nil {
				slog.Error("Web UI error", "err", err)
			}
		}()
	}

	if tuiMode {
		runWithTUI(ctx, cancel, proxy, func() error {
			return proxy.Run(ctx, hostOrIP, isDirectMode, store)
		})
		return
	}

	if err := proxy.Run(ctx, hostOrIP, isDirectMode, store); err != nil {
		slog.Error("Tuner proxy error", "err", err)
		os.Exit(1)
	}
}
