# AI Coding Assistant Instructions - HDHomeRun Proxy Go

## Project Overview
A single Go binary that bridges HDHomeRun (home media device) discovery across network VLANs. Implements a two-component proxy system: **AppProxy** (tuner's network) and **TunerProxy** (app's network) using UDP broadcasts and TCP tunneling. Now includes **Tunarr backend integration** for streaming media server discovery.

## Architecture

### Three Operating Modes
**Tuner Proxy Mode**: AppProxy and TunerProxy communicate via TCP, forwarding discovery across VLANs.
**Direct HDHR Mode**: Proxy connects directly to a real HDHomeRun device via UDP.
**Tunarr Backend Mode**: Proxy bridges to Tunarr HTTP API, translating requests into HDHR-like discovery responses.

**AppProxy** ([app_proxy.go](../app_proxy.go)):
- Listens for TCP from TunerProxy (tuner proxy mode) OR UDP broadcasts (direct mode)
- Routes requests to: real HDHomeRun devices via UDP OR Tunarr backend via HTTP API
- Returns responses via TCP/UDP/HTTP back to TunerProxy/apps
- Aggregates responses from multiple sources (HDHR + Tunarr) if configured

**TunerProxy** ([tuner_proxy.go](../tuner_proxy.go)):
- Listens for UDP broadcast packets from local apps (0.0.0.0:65001)
- Forwards requests to: AppProxy via TCP (tuner proxy mode) OR directly to HDHR/Tunarr (direct mode)
- Translates Tunarr HTTP responses into pseudo-HDHR UDP packets
- Relays responses back to apps via UDP

**Tunarr Backend** ([tunarr_backend.go](../tunarr_backend.go)):
- HTTP client for Tunarr API (discover.json, lineup.json, tuner status)
- Converts Tunarr device info into HDHR-compatible discovery packets
- Supports both aggregation (HDHR + Tunarr) and exclusive mode (Tunarr only)
- Health checks and fallback handling

### Message Protocol
[MessageCodec](../message_codec.go) implements simple framing: **2-byte big-endian length prefix + payload**. Used for TCP (between proxies) and UDP (app/tuner communication).

### Configuration System
[Config](../config.go) priority: CLI args > JSON file > hardcoded defaults. New `tunarr` section supports:
- `enabled`: boolean to activate Tunarr backend
- `host`/`port`: Tunarr server location
- `use_tunarr_only`: if true, ignore HDHR and use Tunarr exclusively
- `http_timeout_seconds`: HTTP request timeout

## Key Patterns & Conventions

- **Context-based cancellation**: All `Run()` methods accept `ctx context.Context` for graceful shutdown
- **Mutex protection**: TCP (`tcpMutex`) and UDP (`udpMutex`) connections guarded separately to avoid blocking
- **UDP timeout handling**: Read deadlines set to 100ms; timeout errors caught and retried (not fatal)
- **Backend routing**: `forwardToBackend()` methods prioritize Tunarr if available, fall back to HDHR if not `use_tunarr_only`
- **Platform awareness**: Linux uses "255.255.255.255" for broadcast binds, Windows uses "0.0.0.0" (see `tuner_proxy.go:62-65`)
- **Structured logging**: Uses `slog` with Info/Debug levels; debug mode enables detailed diagnostics

## Development Workflow

### Build
```bash
go build -o hdhomerun_proxy
```

### Docker
```bash
docker build -t hdhomerun-proxy:latest .
docker run -d --network host --name hdhomerun-proxy-app hdhomerun-proxy:latest app 0.0.0.0
```

### Test Locally
```bash
# Terminal 1: App proxy (with Tunarr backend enabled in config)
./hdhomerun_proxy -config hdhomerun_proxy.json app

# Terminal 2: Tuner proxy (forwards to Tunarr)
./hdhomerun_proxy -config hdhomerun_proxy.json tuner 127.0.0.1
```

### Configuration
Generate template: `./hdhomerun_proxy -template` → generates `hdhomerun_proxy.json` with all options

Example Tunarr config:
```json
{
  "tunarr": {
    "enabled": true,
    "host": "tunarr.local",
    "port": 8000,
    "use_tunarr_only": false,
    "http_timeout_seconds": 5
  }
}
```

## Cross-Component Communication

- **AppProxy ↔ TunerProxy**: TCP on port 65001, wrapped with MessageCodec
- **Apps ↔ TunerProxy**: UDP broadcasts to 0.0.0.0:65001, raw HDHomeRun protocol
- **AppProxy ↔ HDHomeRun**: UDP broadcasts to 255.255.255.255:65001 (direct mode)
- **AppProxy ↔ Tunarr**: HTTP GET requests to `http://host:port/discover.json`, `lineup.json`, `tuner*/status.json`
- **TunerProxy ↔ Tunarr**: HTTP via AppProxy, translated to UDP discovery responses

## Common Modifications

- **Add Tunarr support**: Set `tunarr.enabled: true` in config with `host`/`port`
- **Hybrid mode**: Leave `use_tunarr_only: false` to query both HDHR and Tunarr, prefer Tunarr
- **Timeout tuning**: `UDPReadTimeout` for HDHR delays, `tunarr.http_timeout_seconds` for Tunarr
- **Custom ports**: Set `hdhomerun_port`/`tcp_port` and `tunarr.port` in config
- **Debug Tunarr**: Use `-debug` flag to see HTTP requests and discovery packet translation
- **Health checks**: Tunarr backend automatically checks availability on startup and logs warnings if unreachable

## External Dependencies
None beyond stdlib. Uses `log/slog` (1.21+), `net`, `encoding/binary`, `sync` for concurrency, `net/http` for Tunarr API calls.

See [README.md](../README.md) and [CONFIG.md](../CONFIG.md) for user-facing details.
