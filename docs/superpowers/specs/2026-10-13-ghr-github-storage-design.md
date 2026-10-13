# ghr: GitHub artifact storage management

**Date:** 2026-10-13
**Status:** design presented in chat in three sections and approved by the owner on 2026-10-13, with the rulings on scope, coverage, the cleanup rule, the approach and the execution strategy made the same day. Amended the same day after a Fable review of the first draft (notes/2026-10-13-ghr-github-storage-fable-review.md, findings F1 to F35): the owner ruled on the age floor, bulk retention, public repositories and dismissable banners; the rest are the author's rulings, recorded below and in the review note's numbering.
**Execution strategy:** sequential — the cleanup deletes data and runs alone, and the web tasks consume the daemon's new API types, so the work is one chain.
**Register:** docs/superpowers/registers/2026-10-13-ghr-github-storage.md (rows 1 to 4).
**Code:** `internal/github` (new calls), new `internal/ghstorage`, `internal/config`, `internal/model`, `internal/daemon`, `internal/api`, `cmd/ghr` (`ghr status` line), `web/src` (API types, an Artifacts tab, a quota banner, the change-topic map), `deploy/config.example.yaml`, `README.md`.

## Problem

CI in darkcloud, darkmem, chicken-farm and darkraise-framework failed from 2026-10-06 to 2026-10-12 with the job annotation `Failed to CreateArtifact: Artifact storage quota has been hit. Unable to upload any new artifacts. Usage is recalculated every 6-12 hours.` (found with `gh` on 2026-10-13).

What was measured that day:

- The account (`darkraise`, a user) is on the Free plan: 500 MB of storage shared by Actions artifacts and Packages.
- GitHub accrues storage hourly, measures it in binary gigabytes (1 GB = 2^30 bytes), and resets accrued storage "at the start of each billing cycle" (docs.github.com, "About billing for GitHub Actions", read 2026-10-13). 500 MB over October's 744 hours is 363.3 GB-hours.
- `GET /users/darkraise/settings/billing/usage?year=2026&month=10` reports 373.3 GB-hours of "Actions storage" and 11.2 of "Packages storage". About 330 of those came on 2026-10-05 and 2026-10-06 (darkmem 204, darkcloud 128 for the month). Since 2026-10-07 about 4 GB-hours a day accrue, roughly 175 MB stored, yet uploads were still refused on 2026-10-11 and 2026-10-12. So GitHub enforces the month's accumulated GB-hours, not the bytes stored now. This is inferred from the data, not stated in GitHub's docs.
- darkrouter, public since it was created on 2026-08-21, accrues "Actions storage" GB-hours every day in September and October. GitHub's docs say Actions *minutes* are free for public repositories and say nothing of their storage, so public repositories are treated as counting.
- Every affected repository keeps artifacts and logs for 90 days (the default; `maximum_allowed_days` 400).
- The Actions cache (6 GB in darkmem, 3.4 GB in chicken-farm, 3.3 GB in darkcloud) does not count: it has its own 10 GB per repository (same docs page).

Deleting artifacts after the allowance is spent does not unblock the month. Prevention means keeping artifacts from accruing GB-hours, mainly through a short retention and an age-based cleanup, and warning before the month's allowance runs out.

## Decisions

- **Scope** (owner's ruling, 2026-10-13): a quota monitor with warnings, an auto-cleanup policy, an Artifacts page, and per-repository retention and Actions cache management.
- **Coverage** (owner's ruling, 2026-10-13): every repository of the configured owner that the token can reach, the same list as `GET /repos/available`, not only configured and watched ones. The quota is account-wide.
- **Cleanup rule** (owner's ruling, 2026-10-13): one global maximum age, off until set, with a per-repository override (an age, or off).
- **Age floor** (owner's ruling after the review, F2): the global age is at least 24 hours; a repository override may go down to 1 hour. ghr cannot see an artifact being needed by a different run (a `workflow_run`-triggered workflow, a download by run ID, a nightly job reading the last build), and the save dialog says so.
- **Public repositories are treated like private ones** (owner's ruling after the review, F12): counted in the projection and cleaned by the global age.
- **Bulk retention** (owner's ruling after the review, F35): the page can set retention days on every repository in the snapshot in one action.
- **Dismissable banners** (owner's ruling after the review, F33): the `projected` and `warn` banners can be dismissed per level per month in that browser; `critical` and `exhausted` always show.
- **Approach** (owner's ruling, 2026-10-13): a daemon poller in a new package that keeps a snapshot; the web UI and the warnings read the snapshot.
- **The Actions cache is never cleaned automatically**: it does not count against the allowance. It can be deleted by hand.
- **The allowance is a config value.** A fine-grained token cannot read the account's plan.
- **The billing cycle is assumed to be the calendar month in UTC** (F13). The usage endpoint is queried by calendar month, but GitHub's docs say only "billing cycle". The quota card's help text states the assumption, and the live check (§5) confirms it on 2026-11-01.
- **Bulk operations run in the background** (F4). A bulk delete or bulk retention change answers 202 and reports progress in the snapshot; a single-item route answers when done.

## 1. GitHub client

New calls in `internal/github`, each with a fake-server test in the `client_test.go` pattern:

| Call | Endpoint | Token permission |
|---|---|---|
| `OwnerType(ctx)` | `GET /users/{owner}` (`type`: `User` or `Organization`) | none |
| `TokenLogin(ctx)` | `GET /user` (`login`) | none |
| `StorageUsage(ctx, isOrg, year, month)` | `GET /users/{owner}/settings/billing/usage?year=&month=` or `GET /organizations/{owner}/settings/billing/usage?year=&month=` | user account "Plan: read"; organization "Administration: read" |
| `StorageSummary(ctx, isOrg, year, month)` | `GET /users/{owner}/settings/billing/usage/summary?year=&month=` or the `/organizations/{owner}/...` form (public preview) | as above |
| `ListRepoArtifacts(ctx, repo)` | `GET /repos/{o}/{r}/actions/artifacts?per_page=100`, all pages | Actions: read |
| `DeleteArtifact(ctx, repo, id)` | `DELETE /repos/{o}/{r}/actions/artifacts/{id}` | Actions: write |
| `ListCaches(ctx, repo)` | `GET /repos/{o}/{r}/actions/caches?per_page=100`, all pages | Actions: read |
| `DeleteCache(ctx, repo, id)` | `DELETE /repos/{o}/{r}/actions/caches/{id}` | Actions: write |
| `Retention(ctx, repo)` / `SetRetention(ctx, repo, days)` | `GET` / `PUT /repos/{o}/{r}/actions/permissions/artifact-and-log-retention` | Administration: read / write |

- `Artifact` gains `CreatedAt time.Time` and `WorkflowRun struct{ ID int64; HeadBranch string }`. The existing per-run `ListArtifacts` keeps working with the extra fields.
- `StorageUsage` returns only the items whose `unitType` is `GigabyteHours` and whose `sku` is `Actions storage` or `Packages storage`: date, sku, repository name and quantity. `quantity` is decoded as `float64`: GitHub's schema says integer, but the measured values are fractional (F27). Git LFS storage is billed separately and is ignored.
- `DeleteArtifact` and `DeleteCache` return `(deleted bool, err error)`: `true` on 204, `false` with no error on 404 (someone else deleted it first). Deletes go through the client's existing `mutate`, one second apart.
- `StorageSummary` returns, for the skus `actions_storage` and `packages_storage`, `grossQuantity`, `discountQuantity` and `netQuantity` as `float64`. Measured for October 2026 on 2026-10-13: `actions_storage` gross 373.310, discount 373.316, net 0.0016 GB-h, while uploads were refused. The endpoint is a public preview, so its failure never fails a billing read.
- `getAll`'s 50-page limit stays. A repository with more than 5,000 live artifacts or caches gets a row error naming the limit, and the row's hint points to retention (F29).

## 2. The `ghstorage` package

`internal/ghstorage` owns the poller, the snapshot, the cleanup, the bulk operations and the warning level. It gets the GitHub client, the config store, the events ring, a clock and a logger, and has no knowledge of HTTP.

### 2.1 Passes

- **Billing**, hourly, at start, and at `ResetsAt`. It reads the current UTC month's usage, sums Actions and Packages GB-hours, and keeps per-repository and per-day totals. It then reads the summary (§1) as a cross-check: GitHub's own split of the month's storage into the part its free allowance covers (discount) and the part over it (net). The owner type is read once at start; the owner cannot change while the daemon runs (F14).
- **Quota only for the token's own account** (F15). For a user owner, the billing endpoint reads the token user's own billing. When `TokenLogin` differs from the owner, there is no billing read, and `QuotaError` says "the quota can only be read with a token of <owner>". For an organization owner, the token must belong to an organization admin.
- **Inventory**, every 15 minutes and at start. It lists the token's repositories of the owner (`ListUserRepos` filtered as `AvailableRepos` does), then for each one, 4 at a time, lists artifacts and caches. Expired artifacts are dropped from the snapshot entirely and never deleted (F16). Retention is read for a repository at most once a day.
- **Cleanup** runs at the end of each inventory pass (§2.3).
- **Reading config.** A pass reads the config once, at its start (F30).
- **Token generation** (F19). A pass records the token generation it started under. If the generation changed by the time the pass finishes, its result is dropped. `SetToken` and `Reload` call `Refresh()`, which also clears the rows' errors.
- **Rate budget** (F3). A pass is skipped, and the last snapshot kept, while the client is suspended for a rate limit or while the token has fewer than 500 calls left this hour.
  - At 36 repositories an inventory pass costs about 75 calls, plus 5 per repository that has cleanup candidates (§2.3), plus up to 36 retention reads once a day: 300 to 600 of the 5,000 an hour.
  - Writes (deletes and retention changes) are counted in a rolling hour and capped at 300, below GitHub's secondary limit of 500 content-generating requests an hour. That leaves the runner manager at least 200 for its own writes (JIT configs, runner deletes).
  - A cleanup pass makes at most 100 deletes, oldest artifacts first across all repositories. The rest wait for the next pass.
  - All of ghstorage's writes come from one worker, one at a time. A runner start waiting for the client's write slot therefore waits behind at most one delete.
- **`Refresh()`** starts an inventory pass and a billing read now. It returns `ErrBusy` while a pass runs, which the API answers with 409 like the disk storage refresh (F22).
- **Unconfigured daemon.** One with no owner or token runs no passes.

### 2.2 Snapshot

`model.GitHubStorage`, served by `GET /github-storage`:

```go
type GitHubStorage struct {
	CheckedAt  *time.Time    `json:"checked_at,omitempty"` // last finished inventory pass
	Quota      *Quota        `json:"quota,omitempty"`      // nil until the first successful billing read
	QuotaError string        `json:"quota_error,omitempty"` // last billing read error; Quota keeps the previous value
	Repos      []RepoStorage `json:"repos"`                // sorted by artifact bytes, largest first
	Operation  *Operation    `json:"operation,omitempty"`  // the running or last finished bulk operation
	Deletions  []Deletion    `json:"deletions"`            // newest first, at most 200, in memory only
}

type Quota struct {
	Month            string     // "2026-10" (UTC)
	ReadAt           time.Time
	IncludedGBHours  float64    // included_mb / 1024 × hours in the month
	UsedGBHours      float64    // Actions + Packages this month, as billed
	ActionsGBHours   float64
	PackagesGBHours  float64
	RateGBHoursPerHour float64  // artifact bytes now / 2^30 + last full UTC day's Packages GB-h / 24
	EstimatedGBHours float64    // UsedGBHours + RateGBHoursPerHour × 12 (GitHub's recalculation lag)
	OverAllowanceGBHours *float64 // net actions_storage + packages_storage from the summary; nil when the summary could not be read
	ProjectedGBHours float64    // UsedGBHours + RateGBHoursPerHour × hours until ResetsAt
	RunsOutAt        *time.Time // see below
	Level            string     // "", "projected", "warn", "critical", "exhausted"
	ResetsAt         time.Time  // 00:00 UTC on the 1st of next month
}

type RepoStorage struct {
	Name          string
	Visibility    string // "public" or "private"
	Covered       bool   // configured or watched: ghr has a run page for it
	ArtifactCount int
	ArtifactBytes int64
	CacheCount    int
	CacheBytes    int64
	RetentionDays    int // 0 while unknown
	RetentionMaxDays int // 0 while unknown
	Cleanup       string // effective: "off" or a duration such as "24h"
	CleanupSource string // "global", "override" or "none" (no global age and no override)
	Artifacts     []ArtifactItem // id, name, bytes, created, expires, run id, branch
	Caches        []CacheItem    // id, key, ref, bytes, last used
	Error         string         // last error for this repository
}

type Operation struct {
	ID         string
	Kind       string // "delete-artifacts", "delete-caches", "set-retention"
	Repo       string // empty for set-retention across repositories
	Total, Done int
	Bytes      int64
	Failed     []OperationFailure // {Repo, ID, Error}
	StartedAt  time.Time
	FinishedAt *time.Time
}

type Deletion struct {
	At    time.Time
	Repo  string
	Kind  string // "artifact" or "cache"
	Name  string
	Bytes int64
	Age   string
	By    string // "cleanup" or "manual"
}
```

Go field names stand for snake_case JSON tags, as elsewhere in `internal/model`.

**Rate, estimate and projection** (F7, F8, F34):
- Bytes are converted to GB with 2^30, and `included_mb` with 1024.
- The rate is the hourly accrual if nothing changes: every artifact in the snapshot, public or private, plus Packages at the last full UTC day's rate. ghr does not list Packages.
- **`RunsOutAt`** is `ReadAt + (Included − Estimated) / Rate`. It is nil when the rate is 0, when `Estimated` ≥ `Included`, or when the date falls at or after `ResetsAt`.
- **When computed.** All of these are computed at the end of a billing read or an inventory pass and stored. Nothing is computed per request, so the snapshot only changes when a pass changes it.
- **Month rollover.** At `ResetsAt` a billing read runs. Until it returns, `Quota` is cleared, so last month's numbers never show as this month's.

**Snapshot writes after a manual delete** (F18). Each repository has a generation, bumped by every manual delete. A pass that listed a repository under an older generation removes from its listing the items deleted since. A deleted artifact therefore does not come back.

### 2.3 Cleanup

For each repository whose effective cleanup is a duration:

1. Find the candidates: artifacts whose `CreatedAt` is older than the age, measured against the daemon's clock. A host clock running fast deletes early by its skew; the 24-hour global floor keeps that small (F32).
2. If there are candidates, list the repository's runs that have not completed: `ListRuns` once each for `queued`, `in_progress`, `waiting`, `requested` and `pending` (F1). This covers a deploy job waiting for an environment approval.
3. Drop the candidates whose `WorkflowRun.ID` belongs to one of those runs.
4. Add the rest to the pass's delete list.

The pass then deletes up to 100 artifacts from that list, oldest first (§2.1):
- Each delete that returns `deleted == true` is recorded in `Deletions` and logged (`github storage: deleted artifact <name> (<size>) in <repo>, age <age>`). A 404 records nothing (F17).
- One event per repository per pass goes to the events ring, e.g. `cleanup deleted 12 artifacts (340 MB)` (F28).
- A repository's cleanup stops at its first error other than 404, and the error is recorded on the repository.

**Effective cleanup:** the repository's override when it has one; otherwise the global `cleanup_max_age`; otherwise off.

**What cleanup cannot protect.** An artifact whose consumer is a *different* run is not protected. The README, the policy card's help text and every save-preview dialog say so (F2).

### 2.4 Warning level

Computed with the projection (§2.2), highest first:

- `exhausted`: `Used` ≥ `Included`, or `OverAllowanceGBHours` > 0.001 (GitHub reports usage past its free allowance). Uploads are failing now. The second test needs no configured allowance, so it also catches a wrong `included_mb`; the quota card shows both figures.
- `critical` / `warn`: `Estimated` ≥ the second / first `warn_pct` of `Included`.
- `projected`: `Projected` ≥ `Included`.
- otherwise none.

The thresholds compare against `Estimated`, not `Used`, because GitHub's figure trails by up to 12 hours (F34); `exhausted` uses the billed figure because it promises failures. A level change is logged once and added to the events ring.

`model.Status` gains `GitHubStorage *StorageAlert`, omitted when there is no quota (F6):

```go
type StorageAlert struct {
	Level           string
	Month           string
	UsedGBHours     float64
	IncludedGBHours float64
	UsedPct         int
	RunsOutAt       *time.Time
	ResetsAt        time.Time
}
```

It is set in `Backend.Status` and in the `status` topic's fingerprint function (`internal/daemon/changes.go`), so the banner refreshes when the level changes.

## 3. Config, API and CLI

### 3.1 Config

```yaml
github_storage:
  included_mb: 500          # the plan's artifact + Packages allowance
  warn_pct: [70, 90]
  cleanup_max_age: 2d       # absent is off; at least 24h
  repos:                    # optional per-repository overrides
    darkmem: { cleanup: 6h }   # at least 1h
    homelab: { cleanup: off }
```

Go types (F5):

```go
type GitHubStorage struct {
	IncludedMB    int                      `yaml:"included_mb,omitempty"`
	WarnPct       []int                    `yaml:"warn_pct,omitempty"`
	CleanupMaxAge Duration                 `yaml:"cleanup_max_age,omitempty"` // 0 is off
	Repos         map[string]StorageOverride `yaml:"repos,omitempty"`
}
type StorageOverride struct {
	Cleanup string `yaml:"cleanup"` // "off" or a duration
}
```

- **Defaults.** When the block or a field is absent: `included_mb` 500, `warn_pct` [70, 90], cleanup off, no overrides. The block is omitted from a written config.yaml while every value is its default.
- **`Validate` rejects:**
  - a non-zero `cleanup_max_age` under 24h;
  - an override `cleanup` that is neither `off` nor a duration of at least 1h;
  - `warn_pct` that is not two increasing values in 1–100;
  - `included_mb` below 1;
  - an override key that is empty or contains a slash, and two keys that differ only by case (F24).
- **Matching.** Override keys are matched to repositories ignoring case. An override for a repository the token cannot reach is kept, and ignored while it cannot be reached.
- **The patch.** `model.ConfigPatch` gains `GitHubStorage *GitHubStoragePatch`, sent through the existing `PATCH /config`:
  - `IncludedMB *int`;
  - `WarnPct *[]int`;
  - `CleanupMaxAge *string`, where `""` turns cleanup off;
  - `Repos map[string]*string`, where a value sets that repository's `cleanup` and `null` removes its override.

### 3.2 API

All routes that call GitHub go through `githubErr()` first and answer 503 or 429 as the other GitHub-backed routes do (F31). `{repo}` must name a repository in the snapshot, matched ignoring case; otherwise 404 (F20).

| Route | Body | Answer |
|---|---|---|
| `GET /github-storage` | | the snapshot |
| `POST /github-storage/refresh` | | 202; 409 while a pass runs |
| `POST /github-storage/{repo}/artifacts/delete` | `{"ids": [..]}` or `{"older_than": "24h"}` | 202 with the operation id; 409 while another operation runs |
| `DELETE /github-storage/{repo}/artifacts/{id}` | | 204 |
| `DELETE /github-storage/{repo}/caches/{id}` | | 204 |
| `POST /github-storage/{repo}/caches/delete` | `{"ids": [..]}` or `{"all": true}` | 202; 409 as above |
| `PUT /github-storage/{repo}/retention` | `{"days": n}` | 204 |
| `POST /github-storage/retention` | `{"days": n}` | 202: sets every repository in the snapshot; 409 as above |
| `GET /github-storage/preview` | query `cleanup` and optional `repo` | `[{repo, count, bytes}]` |

- **Bulk delete bodies** (F20). Exactly one of `ids` (non-empty) or `older_than` (a duration of at least 1h), or for caches `ids` or `all: true`; anything else is a 400.
  - An id not in the snapshot for that repository is reported in `Operation.Failed` with "not in the last inventory".
  - `older_than` applies the same not-completed-run skip as cleanup (§2.3). Explicit `ids` delete exactly what was named.
- **One bulk operation runs at a time.** Its progress and result are in `GitHubStorage.Operation`, and finishing adds one event to the events ring with counts and bytes (F28).
- **Retention days.**
  - Must be 1 to `RetentionMaxDays`, or 1 to 400 while that is unknown. GitHub's 422 is answered as 400 with its message.
  - On success, the new value is written into the snapshot at once (F11).
  - `POST /github-storage/retention` skips a repository whose maximum is below the value and records it in `Failed`.
- **`GET /github-storage/preview`** (F21) computes from the snapshot, without GitHub calls:
  - With `repo`, `cleanup` is that repository's proposed override.
  - Without `repo`, `cleanup` is the proposed global age, applied to every repository without an override.
  - `cleanup=off` answers an empty list; a missing or invalid `cleanup` is a 400.
  - The counts include artifacts of runs still in progress, so the dialog says "up to".
- **The change stream** gains a `github-storage` topic. It is a counter, bumped on every snapshot change, like `events` and `history` (F9). Its fingerprint is not a hash of the snapshot. `web/src/api/use-change-stream.ts`'s `TOPIC_QUERIES` maps it to the snapshot query.

### 3.3 CLI

`ghr status` gains one line once a quota exists, from `Status.GitHubStorage`: `artifacts  373 / 363 GB-h this month (103%), exhausted until Nov 1`.

## 4. Web UI

A new top tab **Artifacts** after History (`/artifacts`; the shell's tab union gains it), with:

1. **Quota card.**
   - A meter of billed used against included GB-hours, with markers for the estimate and the projection.
   - The Actions and Packages split, and the billing read time.
   - A line that says "runs out around <date>", or "uploads fail until <date> unless a budget is set" when exhausted.
   - Help text: GitHub recalculates every 6 to 12 hours; the month is assumed to be the calendar month in UTC.
   - `QuotaError` shows as an error line naming the permission or token owner needed.
2. **Policy card.** Included MB, the two warning percentages and the global cleanup age.
   - Saving a changed age opens a dialog listing the preview ("up to N artifacts, X MB, per repository") and the sentence that artifacts needed by a different run are not protected.
   - A **Set retention** action sets one retention value on every repository in the snapshot (not only `Covered` ones). Its dialog says that GitHub's retention also limits how long it keeps runs and logs, which ghr's Actions and log pages read (F11).
3. **Repositories table**, one row per repository: artifacts (count, size), caches (count, size), retention days (editable, same dialog wording), and cleanup (inherit / age / off, previewed like the global age). Each row shows its error line.
   - A row at the 90-day default retention, while the level is not none, shows a hint to lower it (F35).
   - Expanding a row lists its artifacts, with checkboxes, a bulk delete and a "delete older than" action. Each artifact links to ghr's run page when `Covered`, otherwise to `https://github.com/{owner}/{repo}/actions/runs/{id}` (F10).
   - It also lists its caches, each deletable, plus "delete all caches". Every delete asks first.
   - While a bulk operation runs, its progress shows above the table, and other bulk actions are disabled.
4. **Recent deletions**: time, repository, kind, name, size, age, and cleanup or manual.

**Banner.** The quota banner shows on every page for any level other than none, in the shell like the restart banners, and links to `/artifacts`. `projected` and `warn` use the warning style and can be dismissed. A dismissal is stored in local storage per level and month, and wrapped so the banner still works without local storage. `critical` and `exhausted` use the error style and cannot be dismissed.

## 5. Errors and testing

**Errors**

- **Missing permission.** A 403 turns off only the feature that needs it.
  - Repository rows use the existing wording: "token lacks <permission> on <owner>/<repo>: grant it in the PAT settings".
  - Billing has its own: "the token cannot read <owner>'s billing: grant the account permission Plan: read". For an organization: "an organization admin's token with Administration: read is needed".
- **Failed billing read.** It keeps the previous `Quota` with its `ReadAt`, except across a month change (§2.2). ghr never shows an estimate as if it were read.

**Tests**

- `internal/github`: each new call against a fake server, including pagination, the org/user endpoint switch, fractional quantities, the summary's discount and net fields, and `deleted == false` on 404.
- `internal/ghstorage`, on a fake clock and a fake client:
  - cleanup: the age rule, the skip for each of the five not-completed statuses, the override (age, off) and the global fallback, the 100-delete cap oldest first, and the rolling write cap;
  - the passes: the rate-budget skip, the token-generation drop, the manual-delete generation, the 409 while busy, keeping the last quota on a failed read, clearing it at the month change, and the token-login mismatch;
  - the numbers: rate, estimate, projection and `RunsOutAt`, including a zero rate and binary units, and each warning level.
- `internal/config`: defaults, validation, and that a default block is not written.
- `internal/api`: each route, the body rules, the 404 for an unknown repository, the 409s, and the preview cases.
- `internal/daemon`: the alert in `Status` and in the `status` fingerprint, and the `github-storage` counter.
- `web/src`: the Artifacts page, the save-preview and retention dialogs, the delete dialogs, operation progress, the banner per level with dismissal, and the topic mapping.

**Live check on the runner LXC after a release.** Read-only first:

- per-repository artifact bytes match `gh api repos/darkraise/<repo>/actions/artifacts`;
- the month's used GB-hours match the billing endpoint;
- whether the summary's discount stops growing at the configured allowance (500 / 1024 × hours, prorated to the day) or elsewhere. If elsewhere, the allowance formula is amended to match GitHub's discount;
- the allowance shows 500 / 1024 × hours in the month;
- on or after 2026-11-01, the billing read drops to near zero on the 1st. If it does not, the billing cycle is not the calendar month, and this spec is amended (F13).

Cleanup and retention changes are made there only with the owner's go-ahead, starting with one repository.

## README

- **Token.** The PAT paragraph names the new permissions: **Actions: read/write** (it was read) and **Plan: read** (an account permission; **Administration: read** at the organization for an org owner, from an admin). The quota can only be read when the token belongs to the owner. Without these permissions ghr runs jobs as before, and the Artifacts page says which permission is missing.
- **What to set.** A short paragraph says: lower retention to a few days on repositories whose artifacts are only used within their run, and set a cleanup age for the rest. Cleanup does not protect an artifact a different run downloads later.
- **Downgrading.** The note gains: a config with a `github_storage` block is refused by any ghr older than the release that adds it; remove the block first (F25).
