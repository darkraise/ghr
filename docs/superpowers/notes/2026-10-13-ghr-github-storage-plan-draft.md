# Plan draft: GitHub storage management

**Spec:** docs/superpowers/specs/2026-10-13-ghr-github-storage-design.md (approved; owner said "continue" after commit 6c7f8af)
**Register:** docs/superpowers/registers/2026-10-13-ghr-github-storage.md (rows 1 to 4, all open)
**Plan to write:** docs/superpowers/plans/2026-10-13-ghr-github-storage.md, in the format of docs/superpowers/plans/2026-10-13-ghr-ui-config-items.md (copy its Global Constraints block: TMPDIR, Go PATH, timeouts, npm ci, gofmt/vet, tsc/eslint, fixtures test, commit rules, no push or deploy except the last task after the owner's yes).

Codex lane: `codex-gate usable=false reason=plugin-not-enabled lane=false` (2026-10-13), so no Executor lines; plan review uses dr-superpowers:judge-opus.
Execution line expected: inline — `claude --model sonnet --effort high` (heavy tasks 6 to 9 and 11 are delegated).

## Task breakdown (Rule S: at most three file shapes when a task produces an interface)

Go:
1. GitHub client storage calls: create `internal/github/storage.go` + `storage_test.go`; modify `runs.go` (`Artifact` gains `CreatedAt`, `WorkflowRun ArtifactRun{ID, HeadBranch}`). Do not touch client.go: add `IsTooManyPages(err) bool { errors.Is(err, errTooManyPages) }` in storage.go.
2. Model types: create `internal/model/ghstorage.go` + `ghstorage_test.go` (JSON key test); modify `model.go` (`Status.GitHubStorage *StorageAlert json:"github_storage,omitempty"` after WebListenError; `ConfigPatch.GitHubStorage *GitHubStoragePatch json:"github_storage,omitempty"`).
3. Config block: create `internal/config/ghstorage.go` + test; modify `config.go` (field `GitHubStorage GitHubStorage yaml:"github_storage,omitempty" json:"github_storage,omitzero"`; `Validate` appends `c.GitHubStorage.validate()...`).
4. Patch application: create `internal/daemon/storagepolicy.go` (`applyStoragePatch`) + test; `backend.go` PatchConfig calls it after the Web block.
5. Quota math: `internal/ghstorage/quota.go` + test.
6. Cleanup selection (risk 3): `internal/ghstorage/cleanup.go` + test.
7. Service core (risk 3, concurrency): `internal/ghstorage/service.go` + `service_test.go` + `fake_test.go`.
8. Cleanup run and write budget (risk 3): `internal/ghstorage/cleanup_run.go` + test; `service.go` inventory ends with `s.cleanup(...)`.
9. Manual operations (risk 3): `internal/ghstorage/ops.go` + test.
10. Daemon backend methods: create `internal/daemon/ghstorage.go` + test; `backend.go` gains field `GHS GitHubStorageService`.
11. Daemon wiring: `backend.go` (Status alert, SetToken -> `GHS.TokenChanged()`, Reload -> `GHS.Kick()`, PatchConfig with github_storage -> `GHS.PolicyChanged()`), `changes.go` (status hash sets the alert; `count("github-storage", b.GHS.Version)`), `run.go` (build, Start, Close, Wait), test.
12. API routes: `internal/api/server.go` + `internal/api/ghstorage_test.go` (fake methods live in that file, like storage_test.go).
13. CLI line: `cmd/ghr/cli.go` printStatus + its test.

Web (each at most three files):
14. Types + Go fixture: `web/src/api/types.ts`, `web/fixtures_test.go` (new `github-storage` fixture; leave the status fixture without an alert so other page tests keep no banner), `types.test.ts`; regenerate with `GHR_UPDATE_FIXTURES=1 go test ./web/`.
15. Client calls: `api/client.ts` + `api/client-pages.test.ts`.
16. Hook and topic: `api/hooks.ts` (`keys.githubStorage = ["github-storage"]`, `useGitHubStorage`), `api/use-change-stream.ts` (`"github-storage": [keys.githubStorage]`; also add keys.githubStorage to the `config` topic, because rows' cleanup and covered are decorated from the live config at read time), its test.
17. Quota text helpers: `lib/quota.ts` + test (banner text, "Oct 24" UTC dates, dismissal in localStorage keyed by month and level, wrapped in try/catch).
18. Banner: `components/quota-banner.tsx` + test + `shell.tsx` (inside the banner div). Copy the BANNER class string rather than exporting it.
19. Artifacts page skeleton: `pages/artifacts.tsx`, `router.tsx` (route `/artifacts` under appRoute), `shell.tsx` (tab after History; extend the `to` union), `pages/artifacts.test.tsx`; add `"GET /api/github-storage"` to `authedRoutes` in `test/fixtures.ts` in task 14 or here.
20. Quota card. 21. Policy card (preview dialog, Set retention on all). 22. Repositories table (retention edit, cleanup override). 23. Expanded repository rows (artifact checkboxes, delete selected, delete older than, caches, run links: ghr run page when `covered`, else github.com). 24. Operation progress and Recent deletions.
25. Docs: README (token permissions, what to set, downgrade note) and `deploy/config.example.yaml`.
26. Every suite, then the LXC read-only check (owner's yes first).

## Decisions made while reading the code

- `ghstorage.Service` mirrors `storage.Service` (internal/storage/service.go:65-140): `Start`, `Close`, `Wait`, `Refresh` returns `ErrBusy` -> 409. Loop: two timers (inventory 15 min, billing hourly or at `ResetsAt + 1 min`) plus a `kick` chan (buffer 1). `Kick()` never errors; `Refresh()` refuses while busy.
- The service keeps its own `GitHub` interface (the *github.Client satisfies it), so `daemon.GitHub` and its `fakeGH` do not change. Error wording is injected: `Describe func(err error, repo string) string` = daemon `actionsErr(err, owner, repo)` (internal/daemon/actions.go:66).
- Names: `model.GitHubOperation` (model.Operation already exists for disk storage ops), `model.GitHubQuota`, `model.GitHubStorage` gains `Error string` for a failed repository listing (beyond the spec; note it under Assumptions).
- Config zero values mean the defaults (`Included()` 500, `Warn()` [70,90]), so yaml `omitempty` writes no block. `CleanupFor(repo) (time.Duration, source)`, `ParseCleanup`, `CleanupText` ("off" or `ShortDuration`: "2d", "6h", "90m"; `Duration.String()` would write "6h0m0s"). Override keys are validated over sorted keys so the duplicate message is deterministic.
- F18 (no resurrection after a manual delete) is implemented as a map of item ids deleted in the last hour, filtered out of every listing merge, rather than a per-repository generation; same behaviour, simpler. Note in Assumptions.
- Writes go through one `writeMu` plus a rolling-hour budget of 300 (ErrBudget -> 429 with RetryAt). Cleanup caps at 100 deletes per pass, oldest first.
- `PolicyChanged()` recomputes the quota from the last billing read without GitHub calls, so a saved `included_mb` shows at once.
- Rows' `cleanup`, `cleanup_source` and `covered` are set in `Snapshot()` from the current config, not stored.
- Quota math uses `ReadAt` (not now) for the projection and `RunsOutAt`, so the snapshot changes only when a pass changes it. Guard the Duration overflow: compute `RunsOutAt` only when the hours are below the hours left.
- Usage and summary sku spellings differ ("Actions storage" vs "actions_storage"; unit "GigabyteHours" vs "gigabyte-hours"): normalise lower-case, spaces to underscores, drop hyphens.
- `events.Ring.Add` releases its lock before `hub.Wake`, which never blocks (internal/events/events.go:26-40, internal/changes/hub.go:99), so the service may add events while holding its own lock.
- Web: the client returns `undefined` for 202 (web/src/api/client.ts), so bulk routes' operation ids are not read by the UI; progress comes from the snapshot. No checkbox component in darkraise-ui is used today: use a native input. No en or em dashes in UI strings (web/src/anti-slop.test.ts).
- Test helpers: Go client tests use `newClient` (internal/github/client_test.go:23) and set `c.Sleep` to a no-op for several writes; daemon tests use `newBackend` (internal/daemon/backend_test.go:300) and `apiStatus`; web page tests use `mockApi` + `authedRoutes` + `renderApp`.
