# ghr first-run setup from the CLI or the web UI

**Source:** owner requests in chat, 2026-10-07 (three messages after the loopback-only setup amendment to the TUI-retirement spec)
**Covers:** docs/superpowers/specs/2026-10-07-ghr-first-run-setup-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | I still want to set and claim admin password on first time login | docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md | a remote browser on a fresh install claims the password on first visit (spec §5, §10 fresh-install check) | done | owner claimed the password in the browser on a fresh install (throwaway LXC 192.168.0.104, 2026-10-08) |
| 2 | claim that, only the owner who setup and config the runner server will access that web UI for the first time | docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md | ruling recorded in spec Decisions; the loopback guard from ghr e87935d is reverted and its tests removed | done | shipped in v0.1.16; fresh-install check passed 2026-10-08 |
| 3 | You can pick your recommendation for all items (revert the loopback-only setup guard e87935d, restore the setup-page hint, amend the TUI-retirement spec and the loopback plan, keep the setup.sh password prompt optional) | docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md | spec §7 done: guard reverted, login hint reworded, TUI-retirement spec, loopback plan and register amended; setup.sh password prompt stays optional | done | shipped in v0.1.16; fresh-install check passed 2026-10-08 |
| 4 | we can initial setup using both cli/setup script and web ui | docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md | spec §4 and §6: setup.sh prompts (Enter skips) and ghr setup github configure an unconfigured daemon; tests/setup_test.sh and cmd/ghr tests green | done | shipped in v0.1.16; fresh-install check passed 2026-10-08 |
| 5 | web ui initial setup not only for admin password, but for all other things that need initiali config to work | docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md | spec §3 and §5: daemon starts unconfigured; the /setup wizard covers password, GitHub, repos, every setting and toolchains; go test and vitest green | done | shipped in v0.1.16; fresh-install check passed 2026-10-08 |
