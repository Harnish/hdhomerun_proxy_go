# TODO

## Code Structure
- [x] Eliminate duplication between `AppProxy` and `TunerProxy` — extracted into `backendRouter` embedded struct
- [ ] `AppProxy` stores only a single `tcpTransport` — a second `TunerProxy` connecting overwrites the first; support multiple simultaneous TunerProxy connections

## Gaps
- [ ] Add tests
- [x] Add README.md
- [x] Make sure the binary gets put into the path that the service file expects it, creates the user the service file expects and sets permissions, when installing from the deb or rpm.

## Features
- [x] Create a TUI that shows the active connections, connected backends (hdhomeruns, tunarr instances), other stats available and a window with the log.
