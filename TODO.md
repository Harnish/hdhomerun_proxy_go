# TODO

## Code Structure
- [x] Eliminate duplication between `AppProxy` and `TunerProxy` — extracted into `backendRouter` embedded struct
- [ ] `AppProxy` stores only a single `tcpTransport` — a second `TunerProxy` connecting overwrites the first without closing it; support multiple simultaneous connections

## Gaps
- [ ] Add tests
- [x] Add README.md

## Features
- [ ] Create a TUI that shows the active connections, connected backends (hdhomeruns, tunarr instances), other stats available and a window with the log.