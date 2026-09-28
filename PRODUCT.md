# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Technical homelab self-hosters running Plex, Channels DVR, Emby, or Jellyfin on one VLAN with an HDHomeRun tuner (and/or Tunarr) on another. They install the proxy on a Raspberry Pi, VM, or Docker host and treat it as set-and-forget: they open the web UI during initial configuration, then again only when discovery breaks and they need to diagnose why.

## Product Purpose

HDHomeRun discovery relies on UDP broadcast, which does not cross VLAN boundaries. The proxy bridges it so media apps find tuners on another network segment, and can present Tunarr as an HDHomeRun-compatible device. Success means apps see the tuners and nobody has to think about the proxy.

## Positioning

A single static Go binary (Pi-friendly; shipped as release binaries, deb/rpm, and multi-arch Docker) that both bridges HDHomeRun discovery across VLANs and can expose Tunarr as an HDHR device, merged with real tuners or exclusively, with built-in TUI and web observability. Based on @simeoncran's Python proxy, credited in the README.

## Operating Context

- Two-process deployment: App Proxy on the tuner's VLAN, Tuner Proxy on the app's VLAN, linked over TCP 65001. Direct mode runs a single Tuner Proxy with a routed path to the HDHomeRun.
- Runs headless under systemd or Docker with `--network host`.
- Web UI is opt-in (`-webui`), with two tabs: **Status** (live connection counts, backend status, log tail) and **Config** (full config form; saves to the JSON file, debug level and credentials apply live, other changes on restart).
- TUI (`-tui`) is the terminal equivalent and can run alongside the web UI.

## Capabilities and Constraints

- Web UI is one file, `web/index.html`, embedded with `go:embed`. Vanilla JS, no framework, no build step.
- Must work fully offline on an isolated LAN/VLAN: no web fonts, CDNs, or other external requests at runtime.
- Protected by HTTP Basic Auth. Intended for LAN use only, not public internet exposure.
- Web UI and TUI should show the same live information: connections, backends, and logs.
- Core proxy is stdlib-only Go. The TUI adds only charmbracelet packages.
- Known gap (TODO.md): AppProxy supports only one TunerProxy connection at a time.

## Brand Commitments

Product name "HDHomeRun Proxy", binary `hdhomerun_proxy`. Credit to @simeoncran's original Python project must be kept. No logo or visual identity has been established.

## Evidence on Hand

README.md, CONFIG.md, and design specs under `docs/superpowers/specs/`. There are no testimonials, user counts, or benchmarks, and future work must not invent them.

## Product Principles

1. Invisible when working: the UI exists to confirm health quickly and to diagnose failure, not to be browsed.
2. Diagnosis first: backend status, connection state, and logs have to answer "why can't my app see the tuner?"
3. Zero dependencies at runtime: everything ships inside the binary and works on an air-gapped VLAN.
4. One source of truth: web UI, TUI, and config file reflect the same state.
5. Opt-in surfaces: without `-webui`/`-tui`, behavior is unchanged.
