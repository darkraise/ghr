# ghr config items editable in the UI

**Source:** owner request and answers in chat, 2026-10-13
**Covers:** docs/superpowers/specs/2026-10-13-ghr-ui-config-items-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | Add ability to configure the items that need to restart daemon on UI | docs/superpowers/plans/2026-10-13-ghr-ui-config-items.md | Restart ghr from the banner applies a pending web setting and the browser lands on the login page (Task 16 check 1, 2); refused while daemon work runs (Task 7 tests) | done | LXC browser checks 2026-10-13, build config-items-19863e5: checks 1, 2 and 5 passed |
| 2 | The option to serve public repo as well | docs/superpowers/plans/2026-10-13-ghr-ui-config-items.md | Serve when public switch saves allow_public on and off, asking before on (Task 11 tests, Task 16 check 4) | done | LXC browser checks 2026-10-13, build config-items-19863e5: check 4 passed |
| 3 | there are many items, let check again (owner ruling 2026-10-13: all but owner, i.e. allow_public, web.listen, web.hosts) | docs/superpowers/plans/2026-10-13-ghr-ui-config-items.md | Web access edits web.listen and web.hosts, refuses an unreachable address, and a failed bind falls back with a banner (Tasks 3, 8, 12 tests; Task 16 checks 1, 2, 3, 5) | done | LXC browser checks 2026-10-13, build config-items-19863e5: checks 1, 2, 3 and 5 passed |
