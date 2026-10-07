# Fable review: loopback-only web setup amendment

**Date:** 2026-10-07
**Reviewed:** the amendment in homelab 512bd71 to docs/superpowers/specs/2026-10-07-ghr-tui-retirement-design.md (register rows 8, 9, 12)
**Reviewer:** judge-fable (Fable 5.1), read-only. Verdict: approve with changes; findings 1 and 2 must be fixed before the plan.

| # | Severity | Finding | Disposition |
|---|---|---|---|
| 1 | major | `FIRST_INSTALL=1 bash -c '. ./setup.sh && set_web_password'` never prompts: sourcing runs `FIRST_INSTALL=0` (setup.sh:27) | Accepted, verified: assign after sourcing, run under `env -u GHR_WEB_PASSWORD` |
| 2 | major | A bare curl to `/auth/setup` gets 403 `missing X-GHR header` from `checks` (handler.go:68), so the LAN check proves nothing | Accepted, verified: every rollout `/auth` POST sends `X-GHR: 1` and a valid body, and asserts `ErrSetupRemote`'s message |
| 3 | minor | "403 before reading the body" is false: `checks` reads every `/auth` POST body; the 403 mapping was unnamed | Accepted: "before decoding the body and before `auth.Setup`"; new `fail` case |
| 4 | minor | reset-password output (cli.go:131), its event (web.go:26), `Auth.Reset` comment and setup.sh:208-209 still say the next visitor sets the password | Accepted: reworded in §2; tests cli_test.go:232 and web_test.go:61 added to §7 |
| 5 | minor | §7 did not name the tests that break: cli_test.go:325 and four handler tests, two of which depend on the check ordering | Accepted: named, with the 409 and 500 ordering stated |
| 6 | minor | §8's copy-aside and restore steps contradicted each other; a no-password run ended in reset-password | Accepted: one numbered sequence, restored once; the no-password branch ends with set-password |
| 7 | minor | Loopback-only closes the LAN window, not the window: a workflow job is a loopback client | Accepted: Decisions bullet says so |
| 8 | nit | Header presence check, `X-Real-IP`, a shared address helper with `ClientKey`, why `Unmap` matters, an unparseable-address test | Accepted |
| 9 | nit | SECONDS fix gives exactly 6 polls; drop the old comment with `between 5 6` | Accepted |
| 10 | nit | `/auth/state` could tell a remote visitor setup is local-only, so the form is not shown | Declined: the owner approved keeping `/auth/state` unchanged; the hint and the 403 message cover it |
| 11 | nit | Goal line and README "reopens that window" wording | Accepted |
