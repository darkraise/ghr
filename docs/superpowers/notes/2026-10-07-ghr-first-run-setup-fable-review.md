# Fable review: ghr first-run setup spec

**Date:** 2026-10-07
**Reviewed:** docs/superpowers/specs/2026-10-07-ghr-first-run-setup-design.md as committed in homelab 1121feb
**Reviewer:** judge-fable (Fable 5.1), read-only. Verdict: rework (§3 needed a ready signal, an atomic configure gate and an explicit teardown); the rest approve-with-changes. Findings 1-4 spot-checked against the code before accepting (`run.go:137-145, 270-316`, `manager.go:199`, `router.tsx:44-53`).

| # | Severity | Finding | Disposition |
|---|---|---|---|
| 1 | blocker | 204 from `/setup/github` precedes the full API; clients hit 503 in the start-up window and `configured` could not tell store from serving | Accepted: `configured` means serving, new `starting`; CLI, wizard and setup.sh wait for it (§1, §3, §4, §5, §6) |
| 2 | major | Owner check-and-set not atomic across POST and SIGHUP; `Update` has no owner guard | Accepted: `Store.Configure` checks and writes under one `s.mu` hold; `ready` closes via `sync.Once`; owner read after it (§2, §3) |
| 3 | major | A start-up failure after serving began leaks listeners | Accepted: one `teardown` on every exit after serving; test added (§3, §9) |
| 4 | major | Cancel path and SIGHUP registration undefined in the setup phase | Accepted: `signal.Notify` before the wait; setup-phase cancel = teardown only (§3) |
| 5 | major | Epoch changes at the swap | Accepted: minted in `Run`, passed via `Manager.Epoch` (§3) |
| 6 | major | `configured` bool reads false from an older daemon and hides its tables | Accepted: wire field is `unconfigured` (§3) |
| 7 | major | Owner-without-token states; owner typo silently overwrites config | Accepted: state reports config's owner; prefilled read-only; `ErrOwnerMismatch` (§2, §3, §5) |
| 8 | major | Where the setup routes live on the full API was unspecified | Accepted: daemon-level `setupRoutes` in front of the API in both phases; `api.Backend` unchanged (§3) |
| 9 | major | Test plan missed lifecycle, session and race cases | Accepted: added to §9 |
| 10 | minor | `/setup` cannot skip the `Shell` under `appRoute` | Accepted: separate `setupLayout` route (§5) |
| 11 | minor | `UnsavedGuard` does not fire on in-page steps | Accepted: confirm dialog on Next; `SettingsForm` exposes changes and save (§5) |
| 12 | minor | Setup state caching misses changes made outside the browser | Accepted: `staleTime: 0`; banner from the polled `/status` (§5) |
| 13 | minor | GitHub error mapping omitted rate limits and differed from `FromGitHub` | Accepted: explicit kind mapping (§3) |
| 14 | minor | `ListUserRepos` walks all pages for a classic PAT | Accepted as a documented cost (§3) |
| 15 | minor | `GHR_TOKEN` without an owner silently does nothing | Accepted: warning and README note (§6) |
| 16 | minor | How `preinstall_toolchains` detects the 503 was unspecified | Accepted: checks `ghr setup` first (§6) |
| 17 | minor | Reduced `printStatus` output underspecified | Accepted: warnings only (§4) |
| 18 | minor | Setup-phase SIGHUP events undefined | Accepted (§3) |
| 19 | minor | Warn event URL used `0.0.0.0` | Accepted: `<this host>:<port>` (§3) |
| 20 | minor | `print_next` lacked a branch; `web_listen` used config not the applied listener | Accepted (§3, §6) |
| 21 | minor | Register columns empty; row 8's live checks dropped | Accepted: acceptance filled; row 8's checks carried into §10 |
| 22 | minor | setup.sh tests and `wait_ready` comment that break were unnamed | Accepted (§6, §9) |
| 23 | minor | `Configure` locking unstated | Accepted: one hold (§2) |
| 24 | nit | Marker reads per request | Accepted: `os.Stat`, uncached (§1) |
| 25 | nit | 409 message omitted the token path | Accepted (§3) |
| 26 | nit | Reverting `remoteAddr` touches `ClientKey` needlessly | Accepted: keep it (§7) |
| 27 | nit | `ghr daemon started` event not mentioned | Accepted: both events (§3) |
| 28 | nit | Socket setup routes have no session; say why that is fine | Accepted: Decisions bullet |
