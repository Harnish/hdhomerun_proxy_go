# HDHomeRun Proxy

A single Go binary that bridges HDHomeRun device discovery across network VLANs. Based on the Python proxy by [@simeoncran](https://github.com/simeoncran/hdhomerun_proxy) — all the heavy lifting is his work.

## Overview

Three operating modes:

| Mode | Use case |
|------|----------|
| **App Proxy** | Runs on the tuner's network (VLAN where the HDHomeRun lives). Receives TCP connections from the Tuner Proxy and broadcasts discovery queries to local HDHomeRun devices. |
| **Tuner Proxy** | Runs on the app's network (VLAN where Plex/Emby/Channels lives). Listens for UDP broadcasts from apps and relays them to the App Proxy over TCP. |
| **Direct Mode** | Single machine with an IP route to the HDHomeRun — no App Proxy needed. |

[Tunarr](https://github.com/chrisbenincasa/tunarr) is also supported as a backend alongside (or instead of) real HDHomeRun devices.

---

## Download

Pre-built binaries are attached to every [GitHub Release](../../releases):

| File | Platform |
|------|----------|
| `hdhomerun_proxy-linux-amd64` | Linux x86-64 (most servers/VMs) |
| `hdhomerun_proxy-linux-arm64` | Linux ARM 64-bit (Raspberry Pi 4/5, Pi 400) |
| `hdhomerun_proxy-linux-arm-v7` | Linux ARM 32-bit (Raspberry Pi 3/Zero 2 W) |
| `hdhomerun_proxy-windows-amd64.exe` | Windows x86-64 |

Linux users can instead grab a `.deb` or `.rpm` package from the same release. Installing it drops the binary at `/opt/hdhomerun-proxy/hdhomerun_proxy`, installs the systemd unit, and creates the `hdhomerun` user — see [Raspberry Pi Deployment](#raspberry-pi-deployment) below for the manual equivalent.

```bash
# Debian/Ubuntu
sudo dpkg -i hdhomerun-proxy_<version>_amd64.deb

# Fedora/RHEL
sudo rpm -i hdhomerun-proxy_<version>_amd64.rpm
```

Package arch suffixes: `amd64`, `arm64`, `arm7` (Pi 3/Zero 2 W).

---

## Building from Source

```bash
go build -o hdhomerun_proxy
```

Requires Go 1.21+. No external dependencies needed (stdlib only for the core proxy; TUI adds charmbracelet packages).

### Docker

```bash
docker build -t hdhomerun-proxy:latest .

# App proxy
docker run -d --network host --name hdhomerun-proxy-app hdhomerun-proxy:latest app 0.0.0.0

# Tuner proxy with config
docker run -d --network host -v $(pwd)/config:/app/config --name hdhomerun-proxy-tuner \
  hdhomerun-proxy:latest -config /app/config/hdhomerun_proxy.json tuner 192.168.1.100
```

Multi-arch images (`linux/amd64`, `linux/arm64`) are published to GitHub Container Registry on every push to `main` and on version tags.

---

## Usage

### Flags

| Flag | Description |
|------|-------------|
| `-config <file>` | Path to JSON config file |
| `-debug` | Enable debug logging |
| `-template` | Write a template config file and exit |
| `-tui` | Enable terminal UI dashboard |
| `-webui <addr>` | Enable web UI (e.g. `:8080`) |
| `-webui-user <user>` | Basic Auth username (required with `-webui` when config has no webui) |
| `-webui-pass <pass>` | Basic Auth password (required with `-webui` when config has no webui) |
| `-webui-reset` | Force `-webui` flags to overwrite config file webui settings |

### App Proxy

```bash
./hdhomerun_proxy app [bind_address] [hdhomerun_ip]

# Examples
./hdhomerun_proxy app
./hdhomerun_proxy app 0.0.0.0 192.168.1.50
./hdhomerun_proxy -config hdhomerun_proxy.json app
```

### Tuner Proxy

```bash
./hdhomerun_proxy tuner <app_proxy_host_or_hdhomerun_ip> [-direct]

# Examples
./hdhomerun_proxy tuner 192.168.10.9
./hdhomerun_proxy tuner 192.168.1.50 -direct
./hdhomerun_proxy -config hdhomerun_proxy.json tuner
```

### Configuration File

```bash
# Generate a template
./hdhomerun_proxy -template

# Use it
./hdhomerun_proxy -config hdhomerun_proxy.json app
```

See [CONFIG.md](CONFIG.md) for all options.

---

## Web UI

Start the embedded web interface alongside the proxy:

```bash
./hdhomerun_proxy -config hdhomerun_proxy.json -webui :8080 -webui-user admin -webui-pass secret app
```

On first run with a config file, the credentials are saved to the file. On subsequent runs, just use `-config` — no `-webui` flags needed:

```bash
./hdhomerun_proxy -config hdhomerun_proxy.json app
```

To change the bind address or credentials, use `-webui-reset` to force the CLI flags to take effect:

```bash
./hdhomerun_proxy -config hdhomerun_proxy.json -webui-reset -webui :9090 -webui-user admin -webui-pass newpass app
```

Open `http://<host>:8080` in a browser and authenticate with the credentials you provided.

**Status tab** — live connection counters, active backends, and a scrolling log (last 200 entries, with DEBUG filter toggle). Refreshes every second.

**Config tab** — all configuration fields in one form, including the Web UI address and credentials. Saving writes to the config file (if one was set at startup) and applies changes immediately where possible (debug log level and web UI credentials update live; all other changes take effect on the next restart).

The web UI is opt-in. Without `-webui` or webui settings in the config, the binary behaves exactly as before.

---

## Raspberry Pi Deployment

This is the recommended way to run the proxy permanently on a Raspberry Pi.

### 1. Download the binary

```bash
# Pi 4 or Pi 5 (64-bit OS)
curl -L https://github.com/Harnish/hdhomerun_proxy_go/releases/latest/download/hdhomerun_proxy-linux-arm64 \
  -o hdhomerun_proxy

# Pi 3 or 32-bit OS
curl -L https://github.com/Harnish/hdhomerun_proxy_go/releases/latest/download/hdhomerun_proxy-linux-arm-v7 \
  -o hdhomerun_proxy

chmod +x hdhomerun_proxy
```

### 2. Install the binary and create a config

```bash
sudo mkdir -p /opt/hdhomerun-proxy
sudo cp hdhomerun_proxy /opt/hdhomerun-proxy/

# Generate a template config, then edit it for your network
/opt/hdhomerun-proxy/hdhomerun_proxy -template
sudo cp hdhomerun_proxy.json /opt/hdhomerun-proxy/
sudo nano /opt/hdhomerun-proxy/hdhomerun_proxy.json
```

### 3. Create a dedicated user

```bash
sudo useradd -r -s /sbin/nologin -d /opt/hdhomerun-proxy hdhomerun
sudo chown -R hdhomerun:hdhomerun /opt/hdhomerun-proxy
```

### 4. Install the systemd service

```bash
curl -L https://raw.githubusercontent.com/Harnish/hdhomerun_proxy_go/main/scripts/hdhomerun-proxy.service \
  -o hdhomerun-proxy.service
```

Open `hdhomerun-proxy.service` and uncomment the `ExecStart` line that matches your setup (app proxy, tuner proxy, or app proxy with web UI). Then install:

```bash
sudo cp hdhomerun-proxy.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable hdhomerun-proxy
sudo systemctl start hdhomerun-proxy
```

Check that it started correctly:

```bash
sudo systemctl status hdhomerun-proxy
journalctl -u hdhomerun-proxy -f
```

### Service management

```bash
sudo systemctl stop hdhomerun-proxy
sudo systemctl restart hdhomerun-proxy
sudo systemctl disable hdhomerun-proxy   # remove from auto-start
```

### Example: App Proxy with Web UI on Pi

`/opt/hdhomerun-proxy/hdhomerun_proxy.json` (credentials stored in config):
```json
{
  "app": {
    "bind_address": "0.0.0.0"
  },
  "webui": {
    "addr": ":8080",
    "user": "admin",
    "pass": "changeme"
  }
}
```

In the service file, set `ExecStart` to:
```
ExecStart=/opt/hdhomerun-proxy/hdhomerun_proxy \
  -config /opt/hdhomerun-proxy/hdhomerun_proxy.json \
  app
```

Then `sudo systemctl restart hdhomerun-proxy` and open `http://<pi-ip>:8080`.

Alternatively, set the credentials on first run and let them be saved automatically:
```bash
/opt/hdhomerun-proxy/hdhomerun_proxy \
  -config /opt/hdhomerun-proxy/hdhomerun_proxy.json \
  -webui :8080 -webui-user admin -webui-pass changeme \
  app
```
Stop it (Ctrl+C) once it starts — the credentials are now in the config file.

---

## Terminal UI

```bash
./hdhomerun_proxy -tui app
./hdhomerun_proxy -tui tuner 192.168.1.100
```

Shows live connection counts, backend status, and a scrolling log. Can run simultaneously with `-webui`.

---

## Docker Compose

```yaml
services:
  app-proxy:
    image: ghcr.io/harnish/hdhomerun_proxy_go:latest
    container_name: hdhomerun-proxy-app
    network_mode: host
    command: -config /app/config/hdhomerun_proxy.json app
    volumes:
      - ./config:/app/config
    restart: unless-stopped

  tuner-proxy:
    image: ghcr.io/harnish/hdhomerun_proxy_go:latest
    container_name: hdhomerun-proxy-tuner
    network_mode: host
    command: -config /app/config/hdhomerun_proxy.json tuner
    volumes:
      - ./config:/app/config
    restart: unless-stopped
    depends_on:
      - app-proxy
```

---

## Differences from the Python Version

- Single binary, no Python runtime
- JSON config file support
- Terminal UI (`-tui`)
- Embedded web UI (`-webui`)
- Tunarr backend support
- Same UDP/TCP protocol and behavior

---

## License

[Include your license information here]
