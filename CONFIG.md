# HDHomeRun Proxy - Configuration

The binary now supports optional JSON configuration files for fine-tuning behavior without recompilation.

## Quick Start

### Generate a template config file:
```bash
./hdhomerun_proxy -template
```

This creates `hdhomerun_proxy.json` with all available options.

## Using a Config File

```bash
# Use config file
./hdhomerun_proxy -config hdhomerun_proxy.json app
./hdhomerun_proxy -config hdhomerun_proxy.json tuner 10.10.10.9

# Combine with command-line flags
./hdhomerun_proxy -config hdhomerun_proxy.json -debug app
```

## Configuration Options

### Global Settings
```json
{
  "hdhomerun_port": 65001,              // HDHomeRun discovery port
  "tcp_port": 65001,                    // TCP port for tuner proxy
  "udp_read_timeout_ms": 500,           // UDP response timeout
  "udp_read_buffer_size": 4096,         // UDP buffer size (bytes)
  "reconnect_interval_seconds": 3,      // Reconnection delay
  "debug": false                        // Debug logging
}
```

### App Proxy Settings
```json
{
  "app": {
    "bind_address": "0.0.0.0",          // Listen address
    "direct_hdhomerun_ip": ""           // Direct HDHomeRun IP (if not empty)
  }
}
```

### Tuner Proxy Settings
```json
{
  "tuner": {
    "app_proxy_host": "10.10.10.9",     // App proxy hostname
    "direct_mode": false,                // Connect directly to HDHomeRun
    "direct_hdhomerun_ip": "10.10.10.50" // Direct HDHomeRun IP
  }
}
```

### Tunarr Settings
```json
{
  "tunarr": {
    "enabled": true,
    "host": "tunarr.local",               // Tunarr server
    "port": 8000,
    "use_tunarr_only": false,             // true: advertise only the Tunarr tuner, never query a real HDHomeRun
    "http_timeout_seconds": 5,            // API request timeout (streams have no timeout)

    "channels_endpoint": "/api/channels", // Tunarr channel list
    "xmltv_endpoint": "/api/xmltv.xml",   // Guide served at /epg.xml
    "refresh_seconds": 30,                // Channel list refresh interval
    "channel_start": 100,                 // First guide number when numbering channels ourselves
    "use_tunarr_numbers": false,          // Use Tunarr's channel numbers instead of channel_start
    "base_url": "",                       // URL apps use to reach this proxy's :5004; empty = derived automatically
    "stream_mode": "proxy",               // "proxy": pass Tunarr's MPEG-TS through; "mpegts": ffmpeg remux from HLS
    "ffmpeg_path": "ffmpeg"               // Only used with stream_mode "mpegts"
  }
}
```

With Tunarr enabled, the proxy itself acts as an HDHomeRun tuner for Tunarr's channels. It uses the identity in the `device` section: model, `device_id`, friendly name and auth.

- **Discovery:** apps get a standard binary discovery reply on UDP 65001. A real HDHomeRun is still queried too, unless `use_tunarr_only` is set, so apps see both devices.
- **HTTP on port 5004:** `/discover.json`, `/lineup.json`, `/lineup.xml`, `/lineup_status.json`, `/device.xml`, `/epg.xml` (Tunarr's XMLTV guide), `/auto/v<channel>` (streams) and `/healthz`.
- **Streams:** each stream takes one of the model's tuners. When all tuners are busy, a new stream gets `503`, as on a real device.
- **`base_url`:** if left empty, discovery replies advertise this host's address on the route to the requesting app, and HTTP responses use the request's `Host` header. Set `base_url` when apps reach the proxy through NAT or a load balancer.
- **`stream_mode`:**
  - `"proxy"` (the default) needs nothing extra. It streams from Tunarr's `/stream/channels/<id>.ts`, the same MPEG-TS route Tunarr's own HDHR lineup uses.
  - `"mpegts"` runs one `ffmpeg -c copy` per stream to remux Tunarr's HLS output. Nothing is re-encoded. Use it only for Tunarr builds that serve HLS alone. It needs `ffmpeg` on the host, and the Docker image doesn't include it.

### Web UI Settings
```json
{
  "webui": {
    "addr": ":8080",      // Bind address; empty disables the web UI
    "user": "admin",      // HTTP Basic Auth username
    "pass": "secret"      // HTTP Basic Auth password
  }
}
```

When the config file contains `webui` settings, the proxy starts the web UI automatically without any `-webui` flags. Credentials are read on each request, so changing them via the Config tab takes effect immediately without a restart.

## Priority Order

Settings are applied in this priority:
1. Command-line arguments (highest priority) — **exception: webui (see below)**
2. Config file values
3. Built-in defaults (lowest priority)

**Web UI exception:** if the config file already has `webui.addr` set, the `-webui`/`-webui-user`/`-webui-pass` CLI flags are ignored. Pass `-webui-reset` to force the CLI values to take effect and overwrite the config.

Example: If config specifies `direct_hdhomerun_ip` but you pass a different IP on the command line, the command-line value wins.

## Example Configs

### Scenario 1: Direct app proxy pointing to local HDHomeRun
```json
{
  "app": {
    "bind_address": "0.0.0.0",
    "direct_hdhomerun_ip": "192.168.1.50"
  }
}
```

Run: `./hdhomerun_proxy -config config.json app`

### Scenario 2: Tuner in direct mode pointing to HDHomeRun
```json
{
  "tuner": {
    "direct_mode": true,
    "direct_hdhomerun_ip": "10.10.10.50"
  }
}
```

Run: `./hdhomerun_proxy -config config.json tuner 10.10.10.50 -direct`

Or with config handling it:
`./hdhomerun_proxy -config config.json tuner ignored-arg`

### Scenario 3: Two-part proxy setup with custom timeouts
```json
{
  "udp_read_timeout_ms": 1000,
  "reconnect_interval_seconds": 5,
  "app": {
    "bind_address": "0.0.0.0"
  },
  "tuner": {
    "app_proxy_host": "10.10.10.9"
  }
}
```

## Tuning Tips

- **Slow Network**: Increase `udp_read_timeout_ms` (e.g., 1000-2000ms)
- **Low Memory**: Decrease `udp_read_buffer_size` (e.g., 2048)
- **Unreliable Connection**: Increase `reconnect_interval_seconds` (e.g., 10) to reduce reconnection spam
- **Performance**: Decrease timeouts and increase buffer size if network is reliable
