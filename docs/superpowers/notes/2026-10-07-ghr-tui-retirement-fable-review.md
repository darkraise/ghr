# Fable review: ghr TUI retirement spec (round 1)

**Date:** 2026-10-07
**Reviewed:** docs/superpowers/specs/2026-10-07-ghr-tui-retirement-design.md at homelab 8a0ee6f, against ghr master 8f93feb
**Verdict (reviewer):** direction sound, Auth/socket design safe; revise findings 1 to 5 before planning.

| # | Sev | Finding | Ruling |
|---|---|---|---|
| 1 | high | Deleting the 12 client methods breaks `internal/daemon/run_test.go` and strips route coverage in `internal/api/api_test.go`, `update_test.go` | accepted: all client methods kept |
| 2 | medium | setup.sh hint printed in `install_config`, before the daemon exists, then scrolled away by `wait_ready` | accepted: new `set_web_password` step after `wait_ready`; owner chose to prompt for the password (`GHR_WEB_PASSWORD` for scripts) |
| 3 | medium | Nothing reminds the owner while the first-visitor window is open | accepted: start-up `warn` event, `web_setup_required` in `/status`, warning line in `printStatus` |
| 4 | medium | `cli()` has no stderr writer; spec conflated stdin-only and stdin+stdout terminal checks | accepted: `cli` gains `stderr`; new `stdinIsTerminal` |
| 5 | medium | 4 KiB body cap can refuse a valid 1024-byte password after JSON escaping | accepted: 16 KiB cap on the encoded body, test with 1024 `<` |
| 6 | low | Four parity deviations (Dashboard row actions, Runners log preview, chips on every page, copy run URL) | owner chose to close them in the web UI (spec §5) |
| 7 | low | "touches nothing locally" is false; TUI derives labels, activity, warnings client-side | accepted: Decision lists each derivation and its web counterpart |
| 8 | low | go.mod list misses `lrstanley/bubblezone`, `muesli/termenv` | accepted |
| 9 | low | `internal/daemon/backend.go:141` TUI comment missing from the list | accepted |
| 10 | low | Old-daemon 404 message described wrongly | accepted: actual message quoted |
| 11 | low | `fdIsTerminal` type differs between term packages | accepted: keeps `func(uintptr) bool`, wraps `int(fd)` |
| 12 | low | Ctrl-C does not abort `term.ReadPassword` | accepted: `ctx.Err()` checked after each read |
| 13 | low | Socket route decode strictness unspecified | accepted: MaxBytesReader + ReadAll + Unmarshal, as `decodeBody` |
| 14 | low | `ErrUnreadable` advises reset-password; Set repairs a corrupt file | accepted: message changed, test added |
| 15 | low | Auth.Set wording about sessions and lock placement | accepted: clarified |
| 16 | low | Rollout reason wrong (stopping ghr does not stop runners); file juggling fragile | reason accepted; replacement live check declined: re-running set-password "with the real password" needs a password the agent does not know, so copy/restore plus restart stays |
| 17 | low | Missing tests (oversize, trailing data, client method, usage text, corrupt file, unreachable daemon, fakeDaemon restore) | accepted |
| 18 | low | README: show bare `ghr`; LAN-only listen note | accepted |
