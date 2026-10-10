# ghr UI polish and realtime updates

**Source:** owner request in chat, 2026-10-10
**Covers:** docs/superpowers/specs/2026-10-10-ghr-realtime-ui-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | Do not allow to collapse sidebar | commit a697071 | Rail has no collapse toggle; collapsed-rail styles and markup removed | done | toggle hidden by CSS; kit has no prop |
| 2 | Increase font size of entire system by one more level | commit a657e48 | Theme font-size axis is large; no new overlap or truncation versus medium | done | rail widened to 18rem; chart SVG text held at 0.75rem |
| 3 | Realtime update for UI | docs/superpowers/plans/2026-10-10-ghr-realtime-ui.md | Daemon pushes changes; UI refreshes without waiting for poll timers | planned | - |
