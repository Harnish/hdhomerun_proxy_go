package main

import (
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

//go:embed web/index.html
var indexHTML []byte

type webServer struct {
	store  *configStore
	router statsProvider
}

func newWebServer(store *configStore, router statsProvider) *webServer {
	return &webServer{store: store, router: router}
}

// handler returns an http.Handler with all routes behind Basic Auth.
func (ws *webServer) handler(user, pass string) http.Handler {
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return ws.basicAuth(user, pass, h)
	}
	mux.HandleFunc("/", auth(ws.handleIndex))
	mux.HandleFunc("/api/stats", auth(ws.handleStats))
	mux.HandleFunc("/api/logs", auth(ws.handleLogs))
	mux.HandleFunc("/api/config", auth(ws.handleConfig))
	return mux
}

// start starts the HTTP server, blocking until ctx is cancelled.
func (ws *webServer) start(ctx context.Context, addr, user, pass string) error {
	srv := &http.Server{Addr: addr, Handler: ws.handler(user, pass)}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx) //nolint:errcheck
	}()
	slog.Info("Web UI listening", "addr", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("web UI server error: %w", err)
	}
	return nil
}

func (ws *webServer) basicAuth(user, pass string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(pass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="HDHomeRun Proxy"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (ws *webServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML) //nolint:errcheck
}

func (ws *webServer) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ws.router.Stats()) //nolint:errcheck
}

// logEntryJSON is the wire format for a single log entry in GET /api/logs.
type logEntryJSON struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Attrs string `json:"attrs"`
}

func (ws *webServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	entries := getLogEntries()
	out := make([]logEntryJSON, len(entries))
	for i, e := range entries {
		out[i] = logEntryJSON{
			Time:  e.Time.Format("15:04:05"),
			Level: e.Level.String(),
			Msg:   e.Msg,
			Attrs: e.Attrs,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out) //nolint:errcheck
}

// configResponse is the envelope returned by GET /api/config.
type configResponse struct {
	Config  *Config `json:"config"`
	HasFile bool    `json:"has_file"`
}

func (ws *webServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodPost:
		var newCfg Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) //nolint:errcheck
			return
		}
		if newCfg.HDHomeRunPort == 0 || newCfg.TCPPort == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid config: hdhomerun_port and tcp_port must be non-zero"}) //nolint:errcheck
			return
		}
		if err := ws.store.Set(&newCfg); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}) //nolint:errcheck
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true}) //nolint:errcheck
	case http.MethodGet:
		json.NewEncoder(w).Encode(configResponse{ //nolint:errcheck
			Config:  ws.store.Get(),
			HasFile: ws.store.filePath != "",
		})
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}
