# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A single Go binary that bridges HDHomeRun device discovery across network VLANs. Core proxy code uses stdlib only (`log/slog`, `net`, `encoding/binary`, `sync`, `net/http`). The optional TUI (`-tui` flag) adds `charmbracelet/bubbletea`, `charmbracelet/lipgloss`, and `charmbracelet/bubbles`.

## Commands

```bash
# Build
go build -o hdhomerun_proxy

# Generate config template
./hdhomerun_proxy -template

# Run app proxy (tuner's network side)
./hdhomerun_proxy [-config hdhomerun_proxy.json] [-debug] app [bind_address] [hdhomerun_ip]

# Run tuner proxy (app's network side)
./hdhomerun_proxy [-config hdhomerun_proxy.json] [-debug] tuner <app_proxy_host_or_hdhomerun_ip> [-direct]

# Docker
docker build -t hdhomerun-proxy:latest .
docker run -d --network host --name hdhomerun-proxy-app hdhomerun-proxy:latest app 0.0.0.0
```

## Architecture

### Three Operating Modes

1. **Tuner Proxy Mode** — AppProxy and TunerProxy run on separate VLANs and communicate over TCP
2. **Direct HDHR Mode** — TunerProxy connects directly to a real HDHomeRun device via UDP (no AppProxy needed)
3. **Tunarr Backend Mode** — AppProxy bridges to Tunarr HTTP API, translating requests into HDHR-like discovery responses

### Components

**`app_proxy.go` (AppProxy)** — runs on the tuner's network. Listens for TCP connections from TunerProxy (proxy mode) or UDP broadcasts (direct mode). Routes discovery requests to real HDHomeRun devices via UDP or Tunarr via HTTP. Can aggregate results from both sources.

**`tuner_proxy.go` (TunerProxy)** — runs on the app's network. Listens for UDP broadcast packets from local apps on `0.0.0.0:65001`. Forwards to AppProxy over TCP (proxy mode) or directly to HDHR/Tunarr (direct mode); with Tunarr in direct mode it also serves the :5004 HDHR API.

**`tunarr_backend.go` / `tunarr_catalog.go` (TunarrBackend)** — Tunarr HTTP client. Refreshes a channel catalog from `/api/channels`, streams channels (pass-through of `/stream/channels/<id>.ts`, or ffmpeg `-c copy` remux with `stream_mode: "mpegts"`), and passes XMLTV through. Ported from the standalone tunarr-hdhr project.

**`hdhr_discovery.go`** — binary HDHomeRun discovery protocol (TLV + CRC32). The reply encoder is pinned byte-for-byte to a capture from a real HDFX-4K (`hdhr_discovery_test.go`).

**`hdhr_endpoints.go` (HDHREndpointServer)** — HDHR HTTP API on :5004 (`discover.json`, `lineup.json`/`.xml`, `epg.xml`, `/auto/v<ch>` with tuner locking). Lineup and streams come from Tunarr; identity comes from the `device` config section. Exclusive mode (`use_tunarr_only`) or hybrid mode (Tunarr tuner and real HDHR both answer discovery).

**`message_codec.go` (MessageCodec)** — simple framing protocol: 2-byte big-endian length prefix + payload. Used for TCP communication between AppProxy and TunerProxy.

**`config.go` (Config)** — priority: CLI args > JSON file > hardcoded defaults. Covers ports, timeouts, buffer sizes, and the `tunarr` section.

### Communication Paths

| Pair | Transport | Port |
|------|-----------|------|
| Apps ↔ TunerProxy | UDP broadcast | 65001 |
| TunerProxy ↔ AppProxy | TCP + MessageCodec | 65001 |
| AppProxy ↔ HDHomeRun | UDP broadcast | 65001 |
| AppProxy ↔ Tunarr | HTTP GET | configurable (default 8000) |
| Apps ↔ Proxy (HDHR API, Tunarr lineup/streams) | HTTP | 5004 |

### Key Patterns

- All `Run()` methods accept `ctx context.Context` for graceful shutdown via SIGINT/SIGTERM
- TCP and UDP connections guarded by separate mutexes (`tcpMutex`, `udpMutex`)
- UDP read deadline is 100ms; timeout errors are retried, not fatal
- Linux binds broadcast to `255.255.255.255`; Windows uses `0.0.0.0` (see `tuner_proxy.go:62-65`)
- `forwardToBackend()` sends the emulated Tunarr tuner's discovery reply, then also queries the real HDHR unless `use_tunarr_only` is set
- Structured logging via `slog`; `-debug` flag enables `slog.LevelDebug`
