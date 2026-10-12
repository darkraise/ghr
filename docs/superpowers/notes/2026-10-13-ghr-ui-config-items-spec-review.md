# Review of the config-items spec, first draft

**Date:** 2026-10-13
**Reviewer:** an independent read-only review of `specs/2026-10-13-ghr-ui-config-items-design.md` at commit 59d2ac8, checked against the code.
**Outcome:** 16 findings, all checked against the source and accepted; the spec was amended the same day. None conflicted with the owner's rulings.

| # | Finding | Evidence | Change |
|---|---|---|---|
| 1 | "An IP of this host" accepts addresses a browser cannot reach (docker bridges, other NICs), an IPv4 wildcard locks out an IPv6 browser, a LAN address breaks a proxy on loopback; a successful bind defeats the fallback and overwrote the last good address | `run.go` `Run` binds or fails; nine bridge addresses on the dev host | §2.1 check 3 compares with the connection's local address; §2.2 records an address only after a browser's authenticated request |
| 2 | The automatic disk prune never sets `maint.Running`, so "pruning" missed it | `runner/manager.go` sets only `m.pruning` for it | §1.2 adds `Manager.Pruning()` |
| 3 | The repository note needed a visibility field the API lacks | `model.RepoStatus` | §1.5 adds `RepoStatus.Public` |
| 4 | `Addr().String()` reports `[::]` for a wildcard, so Pending would never clear | `run.go` comment on `webListen` | §1.2 compares configured strings |
| 5 | The status fingerprint hashes only the manager's status, so the banner would lag up to 60 s | `daemon/changes.go` `fingerprints` | §1.2 hashes `Restart` and `WebListenError` |
| 6 | `PatchConfig` has no context for the browser mark | `api/server.go` `Backend` interface | §1.1 adds the context |
| 7 | The once-per-change rule contradicted the reload response, pinned by `TestRunServesTheWebUI` | `daemon/backend.go` `Reload` | §1.4 splits response and event |
| 8 | Two tests pin an exit on a bind failure | `run_test.go` | §2.2 names their rewrite |
| 9 | Responding before `systemctl` runs was underspecified; a `Systemd` method would break fakes | `run.go` teardown, `runner.Systemd` | §1.3 flushes the 202 and uses an `Options.Restart` seam |
| 10 | The restarting screen raced the app's 401 redirect and guards; a proxy's port is not the listen port | `web/src/app.tsx`, `router.tsx` | §3.3 clears the cache, renders outside `appRoute`, keeps the page URL behind a proxy |
| 11 | `INVOCATION_ID` is inherited by runner jobs | systemd behaviour (inferred) | §1.2 checks `ghr.service`'s main PID |
| 12 | No mechanism placed a 400 under a field; plumbing for nested and set-compared keys | `api.respond`, `lib/settings-form.ts` | §2.1 prefixes messages, §3.2 places them and names the plumbing |
| 13 | Work a restart interrupts beyond the blockers | `Manager.Kill`, `Tick`, `Backend.Close`, `Service.Close` | Decisions list them as accepted |
| 14 | Fallback details: setup state, connection limits, a bad file | `run.go` `notConfigured`, `limitConns` | §2.2 |
| 15 | A hand edit emptying `web.listen` offered a restart that turns the UI off unannounced | `Store.Reload` | §3.2 note, §3.3 confirmation |
| 16 | Smaller: the CSP has `default-src`, not `connect-src`; `UnsavedGuard` saves too; `blockedText` is pinned; parse failures keep `Validate`'s wording; the patch wakes the loop; `RepoConfig` lacks `allow_public` | `webui/handler.go`, `repository.tsx`, `blocked_test.go` | §3.3, §3.1, §1.4, §2.1, §1.1, §3.1 |
