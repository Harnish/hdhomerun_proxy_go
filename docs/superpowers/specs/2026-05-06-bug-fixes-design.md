# Bug Fixes Design — hdhomerun_proxy_go

**Date:** 2026-05-06  
**Scope:** Five discrete bug fixes, one commit each, ordered by severity.

---

## Commit 1 — Fix `queryTuner` UDP socket mismatch

**File:** `app_proxy.go`  
**Severity:** Critical — AppProxy proxy mode never receives HDHR replies.

**Problem:** `queryTuner` dials a UDP connection to the HDHR address to send the query, then opens a *second, unrelated* `ListenUDP` on `:0` to receive replies. HDHR replies are sent to the port of the dial connection, not to the listener's port, so the listener never sees them.

**Fix:** Remove the separate listener. Use the existing dial connection for both send and receive — `conn.Read` on a connected UDP socket receives replies from the remote address. The dial connection already has the correct local port.

---

## Commit 2 — Fix `\r\n` escape literals

**File:** `tunarr_backend.go` — `BuildDiscoveryResponse`, `ConvertLineupToHDHRFormat`  
**Severity:** High — both functions emit the 4-character literal sequence `\r\n` instead of actual CRLF bytes, producing corrupt protocol output.

**Problem:** Go string literals use `\\r\\n` which is the escaped representation; in the resulting string, that is the characters `\`, `r`, `\`, `n` — not carriage return + newline.

**Fix:** Change all `\\r\\n` occurrences in those two functions to `\r\n`.

Note: `BuildHDHRDiscoveryPacket` already uses correct `\r\n` literals and is not affected.

---

## Commit 3 — Remove duplicate `BaseURL` in `BuildHDHRDiscoveryPacket`

**File:** `tunarr_backend.go`  
**Severity:** Low — duplicate field in discovery response; some apps may reject or misbehave.

**Problem:** Lines 245 and 246 both append `BaseURL` to the response packet.

**Fix:** Delete the second `BaseURL` line (line 246).

---

## Commit 4 — Implement connection counter tracking

**Files:** `app_proxy.go`, `tuner_proxy.go`  
**Severity:** Low — `logActiveConnections` always logs `0/0`; the feature is a no-op.

**Problem:** `activeUDPConnections` and `activeDialConnections` are declared and read in the logging goroutine but never incremented or decremented anywhere.

**Fix:** In both `AppProxy` and `TunerProxy`:
- Increment `activeUDPConnections` when a UDP forwarding goroutine starts; decrement when it exits.
- Increment `activeDialConnections` when a dial to HDHR or Tunarr is opened; decrement when it closes.
Use `activeConnectionsMutex` for all reads and writes.

---

## Commit 5 — Use config buffer size in `handleUDPBroadcasts`

**File:** `tuner_proxy.go`  
**Severity:** Low — inconsistency; ignores user-configured buffer size.

**Problem:** `handleUDPBroadcasts` allocates `make([]byte, 4096)` directly instead of using `UDPReadBufferSize` (the package constant used everywhere else).

**Fix:** Change the allocation to `make([]byte, UDPReadBufferSize)`. No config threading is needed — `UDPReadBufferSize` is the constant default and is consistent with the rest of the codebase. If per-instance config is ever wired through (a future improvement), this can be updated then.
