# ghr: GitHub artifact storage management

**Date:** 2026-10-13
**Status:** design presented in chat in three sections and approved by the owner on 2026-10-13, with the rulings on scope, coverage, the cleanup rule, the approach and the execution strategy made the same day.
**Execution strategy:** sequential — the cleanup deletes data and runs alone, and the web tasks consume the daemon's new API types, so the work is one chain.
**Register:** docs/superpowers/registers/2026-10-13-ghr-github-storage.md (rows 1 to 4).
**Code:** `internal/github` (new calls), new `internal/ghstorage`, `internal/config`, `internal/model`, `internal/daemon`, `internal/api`, `cmd` (`ghr status` line), `web/src` (API types, an Artifacts tab, a quota banner), `deploy/config.example.yaml`, `README.md`.

## Problem

CI in darkcloud, darkmem, chicken-farm and darkraise-framework failed from 2026-10-06 to 2026-10-12 with the job annotation `Failed to CreateArtifact: Artifact storage quota has been hit. Unable to upload any new artifacts. Usage is recalculated every 6-12 hours.` (found with `gh` on 2026-10-13).

What was measured that day:

- The account (`darkraise`, a user) is on the Free plan: 500 MB of storage shared by Actions artifacts and Packages.
- GitHub accrues that storage hourly and resets it each billing cycle (docs.github.com, "GitHub Actions billing", read 2026-10-13). `GET /users/darkraise/settings/billing/usage` for October 2026 reports 373.3 GB-hours of "Actions storage" and 11.2 of "Packages storage". 500 MB over October's 744 hours is 372 GB-hours.
- About 330 of those GB-hours came on 2026-10-05 and 2026-10-06 (darkmem 204, darkcloud 128 for the month). Since 2026-10-07 about 4 GB-hours a day accrue, roughly 175 MB stored, yet uploads were still refused on 2026-10-11 and 2026-10-12. So GitHub enforces the month's accumulated GB-hours, not the bytes stored now. This is inferred from the data, not stated in GitHub's docs.
- Every covered repository keeps artifacts and logs for 90 days (the default; `maximum_allowed_days` 400).
- The Actions cache (6 GB in darkmem, 3.4 GB in chicken-farm, 3.3 GB in darkcloud) does not count: it has its own 10 GB per repository (same docs page).

Deleting artifacts after the allowance is spent does not unblock the month. Prevention means keeping artifacts from accruing GB-hours, and warning before the month's allowance runs out.

## Decisions

- **Scope** (owner's ruling, 2026-10-13): a quota monitor with warnings, an auto-cleanup policy, an Artifacts page, and per-repository retention and Actions cache management.
- **Coverage** (owner's ruling, 2026-10-13): every repository of the configured owner that the token can reach, the same list as `GET /repos/available`, not only configured and watched ones. The quota is account-wide.
- **Cleanup rule** (owner's ruling, 2026-10-13): one global maximum age, off until set, with a per-repository override (an age, or off).
- **Approach** (owner's ruling, 2026-10-13): a daemon poller in a new package that keeps a snapshot; the web UI and the warnings read the snapshot.
- **Public repositories are not cleaned by the global age** (ruling made while writing this spec): their artifacts do not count against the allowance, so deleting them gains nothing. A public repository is cleaned only when its own override sets an age.
- **The Actions cache is never cleaned automatically**: it does not count against the allowance. It can be deleted by hand.
- **The allowance is a config value.** A fine-grained token cannot read the account's plan.

## 1. GitHub client

New calls in `internal/github`, each with a fake-server test in the `client_test.go` pattern:

| Call | Endpoint | Token permission |
|---|---|---|
| `OwnerType(ctx)` | `GET /users/{owner}` (`type`: `User` or `Organization`) | none |
| `StorageUsage(ctx, isOrg, year, month)` | `GET /users/{owner}/settings/billing/usage?year=&month=` or `GET /organizations/{owner}/settings/billing/usage?year=&month=` | user account "Plan: read"; organization "Administration: read" |
| `ListRepoArtifacts(ctx, repo)` | `GET /repos/{o}/{r}/actions/artifacts?per_page=100`, all pages | Actions: read |
| `DeleteArtifact(ctx, repo, id)` | `DELETE /repos/{o}/{r}/actions/artifacts/{id}` | Actions: write |
| `ListCaches(ctx, repo)` | `GET /repos/{o}/{r}/actions/caches?per_page=100`, all pages | Actions: read |
| `DeleteCache(ctx, repo, id)` | `DELETE /repos/{o}/{r}/actions/caches/{id}` | Actions: write |
| `Retention(ctx, repo)` / `SetRetention(ctx, repo, days)` | `GET` / `PUT /repos/{o}/{r}/actions/permissions/artifact-and-log-retention` | Administration: read / write |

- `Artifact` gains `CreatedAt time.Time` and `WorkflowRun struct{ ID int64; HeadBranch string }`. The existing per-run `ListArtifacts` keeps working with the extra fields.
- `StorageUsage` returns only the items whose `unitType` is `GigabyteHours` and whose `sku` is `Actions storage` or `Packages storage`: date, sku, repository name and quantity. Git LFS storage is billed separately and is ignored.
- A 404 from `DeleteArtifact` or `DeleteCache` counts as success: someone else deleted it first.

## 2. The `ghstorage` package

`internal/ghstorage` owns the poller, the snapshot, the cleanup and the warning level. It gets the GitHub client, the config store, a clock and a logger, and has no knowledge of HTTP.

### 2.1 Passes

- **Billing**, hourly and at start: reads the current month's usage (UTC), sums Actions and Packages GB-hours, and keeps per-repository and per-day totals. The owner type is read once at start and after an owner change.
- **Inventory**, every 15 minutes and at start: lists the token's repositories of the owner (`ListUserRepos` filtered as `AvailableRepos` does). For each one, 4 at a time, it lists non-expired artifacts and caches. Retention is read for a repository at most once a day.
- **Cleanup** runs at the end of each inventory pass (§2.3).
- A pass is skipped, and the last snapshot kept, while the client is suspended for a rate limit, or while the token has fewer than 500 calls left this hour. That leaves the runner poller its budget. At 36 repositories a pass costs about 75 calls; four passes an hour are about 300 of 5,000.
- `Refresh()` starts an inventory pass and a billing read now. When one is already running it does nothing.
- An unconfigured daemon (no owner or token) runs no passes.

### 2.2 Snapshot

`model.GitHubStorage`, served by `GET /github-storage`:

```go
type GitHubStorage struct {
	CheckedAt time.Time // last finished inventory pass; zero before the first
	Quota     *Quota    // nil until the first successful billing read
	QuotaError string   // last billing read error, kept while Quota shows the previous value
	Repos     []RepoStorage // sorted by artifact bytes, largest first
	Deletions []Deletion    // newest first, at most 200, kept in memory only
}

type Quota struct {
	Month          string    // "2026-10"
	ReadAt         time.Time
	IncludedGBHours float64  // included_mb / 1000 × hours in the month (GitHub's GB is 10^9 bytes; verify against the October numbers, §5)
	UsedGBHours    float64   // Actions + Packages this month
	ActionsGBHours float64
	PackagesGBHours float64
	ProjectedGBHours float64 // Used + stored bytes now (GB) × hours left in the month
	RunsOutAt      *time.Time // now + (included − used) / current hourly rate; nil when that falls after ResetsAt
	Level          string    // "", "projected", "warn", "critical", "exhausted"
	ResetsAt       time.Time // 00:00 UTC on the 1st of next month
}

type RepoStorage struct {
	Name, Visibility string
	ArtifactCount int; ArtifactBytes int64
	CacheCount int;    CacheBytes int64
	RetentionDays, RetentionMaxDays int // 0 while unknown
	CleanupMaxAge string // effective: "", "off" or a duration; Inherited bool says whether it comes from the global value
	Inherited bool
	Artifacts []ArtifactItem // id, name, bytes, created, expires, run id, branch
	Caches    []CacheItem    // id, key, ref, bytes, last used
	Error string             // last error for this repository, e.g. a missing permission
}

type Deletion struct { At time.Time; Repo, Name string; Bytes int64; Age string; By string } // By: "cleanup" or "manual"
```

"Stored bytes now" is the sum of non-expired artifact bytes across private repositories in the snapshot. Packages bytes are not listed by ghr, so the projection adds Packages at the rate of the last full day's Packages GB-hours.

### 2.3 Cleanup

For each repository whose effective maximum age is a duration:

1. List the repository's runs that are queued or in progress (`ListRuns` with those statuses; one call per status).
2. Delete each artifact whose `CreatedAt` is older than the age and whose `WorkflowRun.ID` is not in that set.
3. Record each deletion in `Deletions` and log it (`github storage: deleted artifact <name> (<size>) in <repo>, age <age>`).
4. Stop that repository's cleanup at its first error other than 404, and record the error on the repository.

The effective age is the repository's override if it has one. Otherwise it is the global `cleanup_max_age`, but only when the repository is private (see Decisions).

### 2.4 Warning level

Computed after each billing read and each inventory pass, highest first:

- `exhausted`: used ≥ included.
- `critical` / `warn`: used ≥ the second / first `warn_pct`.
- `projected`: projected ≥ included.
- otherwise none.

A level change is logged once and published as an event. The level is also copied into `model.Status` as `GitHubStorage *StorageAlert {Level, UsedPct, RunsOutAt, ResetsAt}`, omitted when there is no quota.

## 3. Config, API and CLI

### 3.1 Config

```yaml
github_storage:
  included_mb: 500          # the plan's artifact + Packages allowance
  warn_pct: [70, 90]
  cleanup_max_age: ""       # "" is off; otherwise a duration of at least 1h, e.g. "24h" or "2d"
  repos:                    # optional per-repository overrides
    darkmem: { cleanup_max_age: "6h" }
    homelab: { cleanup_max_age: "off" }
```

- Defaults when the block is absent: `included_mb` 500, `warn_pct` [70, 90], cleanup off, no overrides.
- `config.Validate` rejects an age under 1h, `warn_pct` that is not two increasing values in 1–100, and `included_mb` below 1.
- An override for a repository the token cannot reach is kept, and ignored while it cannot be reached.
- `model.ConfigPatch` gains `GitHubStorage *GitHubStoragePatch` (all fields optional, overrides replaced per repository, `null` removes one). It goes through the existing `PATCH /config`.

### 3.2 API

| Route | Body | Answer |
|---|---|---|
| `GET /github-storage` | | the snapshot |
| `POST /github-storage/refresh` | | 202 |
| `POST /github-storage/{repo}/artifacts/delete` | `{"ids": [..]}` or `{"older_than": "24h"}` | 200 with `{deleted, bytes, failed: [{id, error}]}` |
| `DELETE /github-storage/{repo}/artifacts/{id}` | | 204 |
| `DELETE /github-storage/{repo}/caches/{id}` | | 204 |
| `DELETE /github-storage/{repo}/caches` | | 200 with `{deleted, bytes, failed}` |
| `PUT /github-storage/{repo}/retention` | `{"days": n}` | 204; 400 outside 1 to `RetentionMaxDays` |
| `GET /github-storage/preview?cleanup_max_age=&repo=` | | `[{repo, count, bytes}]` that a save would delete now, from the snapshot |

- `{repo}` must be in the snapshot's repository list; otherwise 404.
- Manual deletes record `By: "manual"` and update the snapshot without a full pass.
- Deletes skip artifacts of in-progress runs only in cleanup. A manual delete deletes what it is asked to.
- The change stream gains a `github-storage` topic, fingerprinted from the snapshot without its timestamps, so open pages refresh.

### 3.3 CLI

`ghr status` gains one line once a quota exists: `artifacts  373 / 372 GB-h this month (100%), exhausted until Nov 1`.

## 4. Web UI

A new top tab **Artifacts** after History (`/artifacts`), with:

1. **Quota card.** A meter of used against included GB-hours, with a projection marker, the Actions and Packages split, the "runs out around <date>" line, or "uploads fail until <date> unless a budget is set" when exhausted. The billing read time is shown. `QuotaError` shows an error line with the permission to grant.
2. **Policy card.** Included MB, the two warning percentages and the global cleanup age. Saving a changed age opens a confirmation dialog listing the preview (§3.2) before the patch is sent.
3. **Repositories table**, one row per repository: artifacts (count, size), caches (count, size), retention days (editable, confirmation dialog), and cleanup (inherit / age / off, previewed like the global age). Each row shows its error line. Expanding a row lists its artifacts, with checkboxes, a bulk delete, a "delete older than" action and a link to the producing run's page in ghr. It also lists its caches, each deletable, plus "delete all caches". Every delete asks first.
4. **Recent deletions**: time, repository, name, size, age, and cleanup or manual.

The quota banner shows on every page for any level other than none, in the shell like the restart banners, and links to `/artifacts`. Levels `projected` and `warn` use the warning style; `critical` and `exhausted` use the error style.

## 5. Errors and testing

**Errors**

- A 403 for a missing permission turns off only the feature that needs it, with the existing wording "token lacks <permission> on <owner>/<repo>: grant it in the PAT settings". Billing errors land in `QuotaError`; repository errors land on the repository row.
- A billing read failure keeps the previous `Quota` with its `ReadAt`; ghr never shows an estimate as if it were read.
- GitHub recalculates the quota every 6 to 12 hours, so ghr's level can lead or trail GitHub's decision by that much. The quota card says so in its help text.

**Tests**

- `internal/github`: each new call against a fake server, including pagination, the org/user endpoint switch, and 404-as-success on delete.
- `internal/ghstorage` on a fake clock: the age rule, the in-progress skip, inherit / override / off, public repositories skipped by the global age, the 1-hour minimum, the projection and `RunsOutAt` math, each warning level, the rate-budget skip, and keeping the last quota on a failed read.
- `internal/config`: validation and defaults of the new block.
- `internal/api`: each route, the 404 for an unknown repository, and the preview.
- `web/src`: the Artifacts page, the save preview dialog, the delete dialogs, and the banner per level.

**Live check on the runner LXC after a release.** Done read-only first:

- per-repository artifact bytes match `gh api repos/darkraise/<repo>/actions/artifacts`;
- October's used GB-hours match the billing endpoint's 373.3 + 11.2;
- the allowance shows 372 GB-hours for October. This settles whether GitHub's GB is 10^9 or 2^30 bytes in the allowance: if 2^30, the formula changes and this spec is amended.

Cleanup is turned on there only with the owner's go-ahead, starting with one repository.

## README

The PAT paragraph names the new permissions: **Actions: read/write** (it was read) and **Plan: read** (account permission; **Administration: read** at the organization for an org owner). Without them ghr runs jobs as before, and the Artifacts page says which permission is missing.
