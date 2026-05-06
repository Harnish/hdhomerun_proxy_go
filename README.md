# HDHomeRun Proxy - Go Binary

A single Go binary that replaces the separate Python proxy scripts from https://github.com/simeoncran/hdhomerun_proxy.  @simeoncran did all the heavy lifting, all the credit goes to him.  I tweaked it to simplify using it in my situation. I am using this to keep my hdhomerun on its own vlan. This binary can run in two modes:

## Building

### From Source
```bash
go build -o hdhomerun_proxy
```

### Docker
```bash
docker build -t hdhomerun-proxy:latest .

# Run as app proxy
docker run -d --network host --name hdhomerun-proxy-app hdhomerun-proxy:latest app 0.0.0.0

# Run as tuner proxy with config
docker run -d --network host -v $(pwd)/config:/app/config --name hdhomerun-proxy-tuner \
  hdhomerun-proxy:latest -config /app/config/hdhomerun_proxy.json tuner 192.168.1.100
```

## Usage

### App Proxy Mode
Run the app proxy (receives broadcast requests from apps and forwards them to tuners):

```bash
./hdhomerun_proxy app [bind_address] [hdhomerun_ip]
```

- `bind_address` (optional): The IP address to bind to. If not specified, binds to all interfaces (0.0.0.0).
- `hdhomerun_ip` (optional): Direct HDHomeRun IP address (bypasses broadcast discovery).

**Example:**
```bash
./hdhomerun_proxy app
./hdhomerun_proxy app 192.168.1.100
./hdhomerun_proxy app 0.0.0.0 192.168.1.50
```

### Tuner Proxy Mode
Run the tuner proxy (receives responses from tuners and forwards them to apps):

```bash
./hdhomerun_proxy tuner <app_proxy_host_or_hdhomerun_ip> [-direct]
```

- `app_proxy_host_or_hdhomerun_ip` (required): The IP address or hostname of the machine running the app proxy, or direct HDHomeRun IP.
- `-direct` (optional): Enable direct mode to connect directly to HDHomeRun instead of using the app proxy.

**Example:**
```bash
./hdhomerun_proxy tuner 192.168.1.100
./hdhomerun_proxy tuner app-proxy.local
./hdhomerun_proxy tuner 192.168.1.50 -direct
```

## Configuration

The binary now supports optional JSON configuration files for advanced customization. See [CONFIG.md](CONFIG.md) for detailed configuration options.

### Quick Start
```bash
./hdhomerun_proxy -template
./hdhomerun_proxy -config hdhomerun_proxy.json app
```

### Flags
- `-config <file>`: Path to JSON configuration file
- `-debug`: Enable debug logging
- `-template`: Generate a template configuration file and exit

## How It Works

The binary supports three operating modes:

**Tuner Proxy Mode** (two machines, different VLANs):
1. **App Proxy** (runs on tuner's network): listens for TCP connections from the Tuner Proxy, broadcasts discovery queries to local HDHomeRun devices via UDP, and forwards responses back.
2. **Tuner Proxy** (runs on app's network): listens for UDP broadcast packets from local apps, forwards them to the App Proxy over TCP, and relays responses back to apps.

**Direct Mode** (single machine with IP route to HDHomeRun): the Tuner Proxy connects directly to the HDHomeRun device without an App Proxy.

**Tunarr Backend Mode**: the proxy bridges to a [Tunarr](https://github.com/chrisbenincasa/tunarr) server via HTTP, making Tunarr appear as an HDHomeRun device to apps. Can run alongside real HDHomeRun devices (hybrid) or replace them entirely (`use_tunarr_only`).

## Requirements

- Go 1.21 or later (for building from source)
- Docker (for containerized deployment)
- Network connectivity between the two machines running app and tuner proxies

## Debugging

Enable debug logging:

```bash
./hdhomerun_proxy -debug app
./hdhomerun_proxy -debug tuner 192.168.1.100
```

Or use the `-config` flag with debug enabled in the configuration file.

## Docker Deployment

### Prerequisites
- Docker installed on the host machine
- Two machines with network connectivity (or one machine running both containers)

### Building the Image
```bash
docker build -t hdhomerun-proxy:latest .
```

### Running Containers

**App Proxy Container:**
```bash
docker run -d --network host \
  --name hdhomerun-proxy-app \
  hdhomerun-proxy:latest app 0.0.0.0
```

**Tuner Proxy Container:**
```bash
docker run -d --network host \
  --name hdhomerun-proxy-tuner \
  hdhomerun-proxy:latest tuner 192.168.1.100
```

**With Configuration File:**
```bash
docker run -d --network host \
  -v $(pwd)/config:/app/config \
  --name hdhomerun-proxy-app \
  hdhomerun-proxy:latest -config /app/config/hdhomerun_proxy.json app
```

### Using Docker Compose

Create a `docker-compose.yml`:
```yaml
version: '3.8'
services:
  app-proxy:
    build: .
    container_name: hdhomerun-proxy-app
    network_mode: host
    command: app 0.0.0.0
    restart: unless-stopped

  tuner-proxy:
    build: .
    container_name: hdhomerun-proxy-tuner
    network_mode: host
    volumes:
      - ./config:/app/config
    command: -config /app/config/hdhomerun_proxy.json tuner 192.168.1.100
    restart: unless-stopped
    depends_on:
      - app-proxy
```

Run with: `docker-compose up -d`

## Recent Changes

### Bug Fixes (Latest)
- Fixed `queryTuner` UDP socket: was opening a separate listener that never received replies; now uses an unconnected socket with `ReadFromUDP` so unicast replies from HDHomeRun devices are correctly received
- Fixed CRLF escaping in discovery response functions — protocol output now contains actual CR+LF bytes instead of the literal 4-character sequence `\r\n`
- Removed duplicate `BaseURL` field from `BuildHDHRDiscoveryPacket`
- Fixed `activeDialConnections` counter — connection logging now reports accurate values instead of always `0/0`
- Fixed `handleUDPBroadcasts` buffer allocation to use the `UDPReadBufferSize` constant consistently

### Tunarr Backend Support
- Added Tunarr as a backend source alongside (or instead of) real HDHomeRun devices
- Tunarr devices appear as HDHR-compatible devices to apps via translated discovery responses
- Hybrid mode: query both HDHR and Tunarr, prefer Tunarr responses
- Exclusive mode (`use_tunarr_only`): ignore HDHR entirely and use only Tunarr
- Configurable via the `tunarr` section in the JSON config file (see [CONFIG.md](CONFIG.md))

### Configuration File Support
- Added JSON configuration file support via `-config` flag
- New `-template` flag to generate template configuration
- Support for direct HDHomeRun connections in both app and tuner modes
- Command-line arguments now override config file settings
- Structured logging with configurable levels

## Differences from Python Version

- Single binary instead of multiple Python scripts
- Better resource efficiency and faster startup
- No Python runtime dependency
- JSON configuration support for complex setups
- Direct HDHomeRun connection mode for simplified deployments
- Structured logging for better debugging
- Same protocol and behavior as the original Python implementation

## License

[Include your license information here]
