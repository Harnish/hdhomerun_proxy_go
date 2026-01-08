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

## Priority Order

Settings are applied in this priority:
1. Command-line arguments (highest priority)
2. Config file values
3. Built-in defaults (lowest priority)

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
