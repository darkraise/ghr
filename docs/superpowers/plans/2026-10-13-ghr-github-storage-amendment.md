# GitHub Storage Amendment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drop GitHub's billing summary from the quota (the `exhausted` level becomes used ≥ included only), and make the cleanup re-check the policy before each delete.

**Architecture:** The summary read leaves `ghstorage`'s billing pass, then the GitHub client, then the model's JSON and the web fixtures, then the quota card. The cleanup's delete loop re-reads the live config before each delete and skips a pick that no longer qualifies by age. The last task runs every suite and, with the owner's yes, deploys to the runner LXC for read-only checks.

**Tech Stack:** Go 1.26 (standard library only); TypeScript 5.7, React 19, Vitest 4 with Testing Library, all already dependencies.

**Spec:** docs/superpowers/specs/2026-10-13-ghr-github-storage-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 2 of 6 tasks are heavy (3: data shape served by the API; 5: risk 3, the delete path) and are delegated; the other 4 run in the session
**Plan review:** 2026-10-13 — dr-superpowers:judge-opus — executability 18 / coherence 19 / coverage 18 / assumptions 18 (round 1)

**Parallelism:** sequential — the owner chose sequential (spec): each task consumes the one before it, and Task 5 changes the delete path and runs alone

## Global Constraints

- Commands run from the repository root. `/tmp` is a small tmpfs that is nearly full: before the first command of a session run `mkdir -p /root/.cache/ghr-plan-tmp`, and put `TMPDIR=/root/.cache/ghr-plan-tmp` in front of every `npm`, `npx` and Go command. Go is at `/usr/local/go/bin` and not on PATH: prefix every Go command with `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH`.
- Give every command a `timeout`: 300 seconds for one Go package or one web test file, 900 for a whole suite.
- A fresh worktree has no `web/node_modules`. Once, before the first web command: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 600 npm ci --prefer-offline --no-audit --no-fund`. `web/package-lock.json` must not change.
- While working on a task run only the tests its commands name; only the last task runs every suite. Never start a dev server, a watcher or any background process.
- Before each Go commit: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/ web/` prints nothing, and `go vet` on the changed packages passes. Before each web commit: `cd web && npx tsc --noEmit` and `cd web && npx eslint src --max-warnings 0` print nothing.
- `web/fixtures_test.go` (`TestTypeFixtures`) marshals Go values and compares them with `web/src/api/fixtures/*.json`; `GHR_UPDATE_FIXTURES=1 go test ./web/` regenerates them.
- No new Go module and no new npm dependency.
- When an existing test fails because it asserted behaviour this plan changes, the task says so and gives the corrected lines. Report any other existing-test failure in the task report; never change production code to make an old expectation pass.
- Each "Find / Replace with" block was written against `master` at `6358d00` plus the earlier tasks of this plan. When a block is not found exactly as quoted, find the same lines by their content, apply the same change, and say so in the task report; never skip the change.
- Code comments: only where the reason is not obvious from the code. No comment mentions a task, a register row or the spec.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no trailing period, no attribution or co-author lines.
- Never push, never write to GitHub (no `gh api -X` other than GET), and never deploy to or restart anything on the runner LXC (192.168.0.99), except in Task 6 after the owner's yes in its first step. Never set a cleanup age, change retention or delete anything there.
- English only.

## Contracts

- **After Task 1:** `ghstorage.GitHub` (the interface in `internal/ghstorage/service.go`) no longer has `StorageSummary`; `ghstorage.Billing` has no `Over` field; `ComputeQuota` no longer reads or writes `OverAllowanceGBHours` (the field itself stays until Task 3). The level rule: `exhausted` exactly when `UsedGBHours >= IncludedGBHours`.
- **After Task 2:** package `github` has no `SummaryItem` and no `(*Client).StorageSummary`; `billingURL` is `func (c *Client) billingURL(isOrg bool, year int, month time.Month) string`.
- **After Task 3:** `model.GitHubQuota` has no `OverAllowanceGBHours`; the JSON key `over_allowance_gb_hours` is gone from `GET /github-storage` and from `web/src/api/fixtures/github-storage.json`.
- **After Task 4:** the web `GitHubQuota` type has no `over_allowance_gb_hours`; the quota card has no "past its free allowance" line.
- **Task 5:** no interface change; behaviour as spec §2.3 "Re-check".

## Assumptions (evidence)

- GitHub's summary carries no allowance signal on this account: `gh api "/users/darkraise/settings/billing/usage/summary?year=2026&month=10"` on 2026-10-13 gave `actions_storage` gross 373.3388, discount 373.3372, net 0.0016, `netAmount` 0; `gh api /user` gave `plan.name` `free`; darkcloud run 38267451928 (2026-10-12) failed with "Failed to CreateArtifact: Artifact storage quota has been hit". GitHub's docs: "If your account does not have a valid payment method on file, usage is blocked once you use up your quota" (docs.github.com/en/billing/managing-billing-for-github-actions/about-billing-for-github-actions, read 2026-10-13).
- Nothing outside the files named here references the removed names: `rg -n "over_allowance|OverAllowance|StorageSummary|SummaryItem|free allowance" --glob '!docs'` on 2026-10-13 listed only those files.
- The fake's `onList` hook (`internal/ghstorage/fake_test.go:45-47`) runs while the inventory lists artifacts, after the pass read its config (`service.go:248-273`) and before the cleanup picks (`service.go:454`), so a test can save a policy between the two; `onDelete` (`fake_test.go:48-49`) runs at each delete.
- Every task's code was applied and tested on 2026-10-13 in a throwaway worktree from `6358d00`, in plan order: each Step 2 failed as written and each Step 4 passed; afterwards `go test ./...` passed in every package, `go test -race -count=5 ./internal/ghstorage/` passed, gofmt and `go vet ./...` were clean, and the web suite passed (1,411 tests) with tsc and eslint clean.
- `TestBillingReadsTheQuota`'s level moves from `exhausted` to `warn`: used 310, rate 1 + 4/24 GB-h per hour, estimate 310 + 14 = 324, which is at least 70% (254.3) and below 90% (326.95) of 363.28125.

## Task index

1. Base the exhausted level on used storage only
2. Remove the summary call from the GitHub client
3. Remove the over-allowance field from the API
4. Remove the over-allowance line from the quota card
5. Re-check the cleanup policy before each delete
6. Run every suite and check on the LXC

---

### Task 1: Base the exhausted level on used storage only

**Files:**
- Modify: `internal/ghstorage/service.go` (the `GitHub` interface; the billing read)
- Modify: `internal/ghstorage/quota.go` (`Billing`, `ComputeQuota`, `level`)
- Test: `internal/ghstorage/fake_test.go`, `internal/ghstorage/service_test.go`, `internal/ghstorage/quota_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: Contracts "After Task 1".

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Change the tests**

In `internal/ghstorage/fake_test.go`, delete these two fields from `fakeGH`:

```go
	summary   []github.SummaryItem
	sumErr    error
```

and delete this method with the blank line after it:

```go
func (f *fakeGH) StorageSummary(context.Context, bool, int, time.Month) ([]github.SummaryItem, error) {
	f.call("StorageSummary")
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.summary), f.sumErr
}
```

In `internal/ghstorage/service_test.go`, `TestBillingReadsTheQuota` asserted `exhausted` from the summary's net; it now expects `warn`. Delete the line

```go
	gh.summary = []github.SummaryItem{{SKU: github.SKUActionsStorage, Net: 0.25}, {SKU: github.SKUPackagesStorage, Net: 0.25}}
```

Find:

```go
	// 1 GiB of artifacts plus 4 GB-hours of Packages a day.
	if q.RateGBHoursPerHour != 1+4.0/24 || q.OverAllowanceGBHours == nil || *q.OverAllowanceGBHours != 0.5 || q.Level != "exhausted" {
		t.Errorf("rate %v, over %v, level %q", q.RateGBHoursPerHour, q.OverAllowanceGBHours, q.Level)
	}
	if a := s.Alert(); a == nil || a.Level != "exhausted" || a.UsedPct != 85 {
```

Replace with:

```go
	// 1 GiB of artifacts plus 4 GB-hours of Packages a day; the estimate,
	// 310 + 14 = 324, is past 70% of 363.28 but short of 90%.
	if q.RateGBHoursPerHour != 1+4.0/24 || q.Level != "warn" {
		t.Errorf("rate %v, level %q", q.RateGBHoursPerHour, q.Level)
	}
	if a := s.Alert(); a == nil || a.Level != "warn" || a.UsedPct != 85 {
```

In the same test, replace `got[0] != "github storage: quota exhausted: 310.0 of 363.3 GB-hours used in 2026-10"` with `got[0] != "github storage: quota warn: 310.0 of 363.3 GB-hours used in 2026-10"`.

In `TestAFailedBillingReadKeepsTheLastQuota`, delete these lines at its end (the closing `}` of the function stays):

```go
	gh.usageErr = nil
	gh.sumErr = errors.New("preview gone")
	s.pass(false, true)
	if q := s.Snapshot().Quota; q == nil || q.OverAllowanceGBHours != nil || !q.ReadAt.Equal(clock.Now()) {
		t.Fatalf("quota %+v: a failed summary must not fail the read", q)
	}
```

`TestTheMonthChangeWritesTheNewMonthsLevel` reached `exhausted` through the summary; it now needs used storage past both months' allowance. Find:

```go
	gh.summary = []github.SummaryItem{{SKU: github.SKUActionsStorage, Net: 0.5}}
```

Replace with:

```go
	// 370 GB-hours: past October's 363.28 and November's 351.56.
	gh.usage = append(gh.usage, github.UsageItem{Date: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), SKU: github.SKUActionsStorage, Repo: "darkcloud", Quantity: 60})
```

In `internal/ghstorage/quota_test.go`, `TestComputeQuota`: delete

```go
	if q.OverAllowanceGBHours != nil {
		t.Fatalf("over %v without a summary", *q.OverAllowanceGBHours)
	}
```

In `TestQuotaLevel`, delete the line `	over := func(v float64) *float64 { return &v }` and these two table rows:

```go
		{"GitHub bills past its allowance", Billing{Actions: 100, Over: over(0.0016)}, 0, [2]int{70, 90}, "exhausted"},
		{"rounding noise in net", Billing{Actions: 100, Over: over(0.0005)}, 0, [2]int{70, 90}, ""},
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/`
Expected: build failure, `*fakeGH does not implement GitHub (missing method StorageSummary)`.

- [ ] **Step 3: Change the code**

In `internal/ghstorage/service.go`, delete this line from the `GitHub` interface:

```go
	StorageSummary(ctx context.Context, isOrg bool, year int, month time.Month) ([]github.SummaryItem, error)
```

and delete these lines from the billing read (just after `b.Actions, b.Packages, b.PackagesDaily = SumUsage(items, now)`):

```go
	// The summary is a public preview: its failure never fails the read.
	if sum, err := s.GH.StorageSummary(ctx, isOrg, year, month); err == nil {
		over := 0.0
		for _, it := range sum {
			over += it.Net
		}
		b.Over = &over
	}
```

In `internal/ghstorage/quota.go`, find:

```go
// Billing is one billing read. PackagesDaily is the Packages GB-hours of the
// last full UTC day before ReadAt; Over is the summary's net Actions and
// Packages storage, nil when the summary could not be read.
type Billing struct {
	Month                            string
	ReadAt                           time.Time
	Actions, Packages, PackagesDaily float64
	Over                             *float64
}
```

Replace with:

```go
// Billing is one billing read. PackagesDaily is the Packages GB-hours of the
// last full UTC day before ReadAt.
type Billing struct {
	Month                            string
	ReadAt                           time.Time
	Actions, Packages, PackagesDaily float64
}
```

Delete from `ComputeQuota`:

```go
	if b.Over != nil {
		v := *b.Over
		q.OverAllowanceGBHours = &v
	}
```

Find:

```go
// level compares the thresholds with the estimate, because the billed figure
// trails; exhausted uses what GitHub billed, because it promises failures.
func level(q model.GitHubQuota, warn [2]int) string {
	switch {
	case q.UsedGBHours >= q.IncludedGBHours, q.OverAllowanceGBHours != nil && *q.OverAllowanceGBHours > 0.001:
```

Replace with:

```go
// level compares the thresholds with the estimate, because the billed figure
// trails; exhausted uses what GitHub billed, because it promises failures.
// GitHub's billing summary is no second test: on an account without a
// payment method it counts the blocked usage as discounted, so its net stays
// near zero while uploads are refused.
func level(q model.GitHubQuota, warn [2]int) string {
	switch {
	case q.UsedGBHours >= q.IncludedGBHours:
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/ ./internal/daemon/`
Expected: both `ok`. Then `PATH=/usr/local/go/bin:$PATH gofmt -l internal/` prints nothing and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/ghstorage/` passes.

- [ ] **Step 5: Commit**

```bash
git add internal/ghstorage/service.go internal/ghstorage/quota.go internal/ghstorage/fake_test.go internal/ghstorage/service_test.go internal/ghstorage/quota_test.go
git commit -m "fix(ghstorage): base exhausted on used storage only"
```

### Task 2: Remove the summary call from the GitHub client

**Files:**
- Modify: `internal/github/storage.go` (`SummaryItem`, `billingURL`, `StorageUsage`, `StorageSummary`)
- Test: `internal/github/storage_test.go`

**Interfaces:**
- Consumes: Contracts "After Task 1" (nothing calls `StorageSummary` any more).
- Produces: Contracts "After Task 2".

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Change the test**

In `internal/github/storage_test.go`, delete the constant `summaryBody` (the five lines from ``const summaryBody = `{"timePeriod"`` to the line ending ``"unitType":"gigabyte-hours"}]}` ``, and the blank line after it). Rename `func TestStorageUsageAndSummary(t *testing.T) {` to `func TestStorageUsage(t *testing.T) {`. Delete from its handler:

```go
				case tc.prefix + "/summary":
					fmt.Fprint(w, summaryBody)
```

Find:

```go
			sum, err := c.StorageSummary(ctx, tc.isOrg, 2026, time.October)
			if err != nil {
				t.Fatal(err)
			}
			wantSum := []SummaryItem{
				{SKU: SKUActionsStorage, Gross: 373.317530205, Discount: 373.315932471, Net: 0.001597734},
				{SKU: SKUPackagesStorage, Gross: 11.155870203, Discount: 11.155870203, Net: 0},
			}
			if len(sum) != 2 || sum[0] != wantSum[0] || sum[1] != wantSum[1] {
				t.Fatalf("summary %+v", sum)
			}
			wantReq := "GET " + tc.prefix + "?year=2026&month=10|GET " + tc.prefix + "/summary?year=2026&month=10"
```

Replace with:

```go
			wantReq := "GET " + tc.prefix + "?year=2026&month=10"
```

- [ ] **Step 2: Confirm the code is still there**

Run: `rg -n "StorageSummary|SummaryItem|summaryBody" internal cmd`
Expected: matches only in `internal/github/storage.go` (the type and the method this task removes).

- [ ] **Step 3: Change the code**

In `internal/github/storage.go`, delete the type with its comment and the blank line after it:

```go
// SummaryItem is the month's storage of one SKU, split into the part the
// plan's allowance covers (Discount) and the part over it (Net).
type SummaryItem struct {
	SKU                  string
	Gross, Discount, Net float64
}
```

Delete the method with its comment and the blank line after it:

```go
// StorageSummary reads the month's Actions and Packages storage totals.
// The endpoint is a public preview.
func (c *Client) StorageSummary(ctx context.Context, isOrg bool, year int, month time.Month) ([]SummaryItem, error) {
	var body struct {
		UsageItems []struct {
			SKU      string  `json:"sku"`
			Gross    float64 `json:"grossQuantity"`
			Discount float64 `json:"discountQuantity"`
			Net      float64 `json:"netQuantity"`
		} `json:"usageItems"`
	}
	if err := c.getJSON(ctx, c.billingURL(isOrg, "/summary", year, month), &body); err != nil {
		return nil, err
	}
	var out []SummaryItem
	for _, it := range body.UsageItems {
		if sku := NormalizeSKU(it.SKU); storageSKU(sku) {
			out = append(out, SummaryItem{SKU: sku, Gross: it.Gross, Discount: it.Discount, Net: it.Net})
		}
	}
	return out, nil
}
```

`billingURL`'s `rest` argument is now always empty. Find:

```go
func (c *Client) billingURL(isOrg bool, rest string, year int, month time.Month) string {
	kind := "users"
	if isOrg {
		kind = "organizations"
	}
	return fmt.Sprintf("%s/%s/%s/settings/billing/usage%s?year=%d&month=%d", c.BaseURL, kind, url.PathEscape(c.Owner), rest, year, int(month))
}
```

Replace with:

```go
func (c *Client) billingURL(isOrg bool, year int, month time.Month) string {
	kind := "users"
	if isOrg {
		kind = "organizations"
	}
	return fmt.Sprintf("%s/%s/%s/settings/billing/usage?year=%d&month=%d", c.BaseURL, kind, url.PathEscape(c.Owner), year, int(month))
}
```

In `StorageUsage`, replace `c.billingURL(isOrg, "", year, month)` with `c.billingURL(isOrg, year, month)`.

Two comments still describe the summary's spelling. Find:

```go
// The storage SKUs, normalized. The usage endpoint spells them "Actions
// storage", the summary "actions_storage".
```

Replace with:

```go
// The storage SKUs, normalized. The usage endpoint spells them "Actions
// storage" and "Packages storage".
```

Find:

```go
// NormalizeSKU lower-cases s, turns spaces into underscores and drops
// hyphens, so both endpoints' spellings of a SKU or a unit compare equal.
```

Replace with:

```go
// NormalizeSKU lower-cases s, turns spaces into underscores and drops
// hyphens, so a SKU or a unit compares equal however GitHub spells it.
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/github/ ./internal/ghstorage/`
Expected: both `ok`. `rg -n "StorageSummary|SummaryItem|summaryBody" internal cmd` prints nothing. gofmt prints nothing and `go vet ./internal/github/` passes.

- [ ] **Step 5: Commit**

```bash
git add internal/github/storage.go internal/github/storage_test.go
git commit -m "refactor(github): drop the billing summary call"
```

### Task 3: Remove the over-allowance field from the API

**Files:**
- Modify: `internal/model/ghstorage.go` (`GitHubQuota`)
- Test: `internal/model/ghstorage_test.go`, `web/fixtures_test.go`, `web/src/api/fixtures/github-storage.json` (regenerated), `web/src/api/types.test.ts`

**Interfaces:**
- Consumes: Contracts "After Task 1" (no Go code writes the field any more).
- Produces: Contracts "After Task 3".

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Change the tests**

In `web/fixtures_test.go`, `githubStorageFixture`, replace `EstimatedGBHours: 262.9, OverAllowanceGBHours: ptr(0.0), ProjectedGBHours: 396.95,` with `EstimatedGBHours: 262.9, ProjectedGBHours: 396.95,`.

In `internal/model/ghstorage_test.go`, `TestGitHubStorageJSON`:
- delete the line `	over := 0.0016`;
- replace `PackagesGBHours: 11.2, RateGBHoursPerHour: 0.17, EstimatedGBHours: 386.54, OverAllowanceGBHours: &over,` with `PackagesGBHours: 11.2, RateGBHoursPerHour: 0.17, EstimatedGBHours: 386.54,`;
- replace ``	`"packages_gb_hours":11.2,"rate_gb_hours_per_hour":0.17,"estimated_gb_hours":386.54,"over_allowance_gb_hours":0.0016,` +`` with ``	`"packages_gb_hours":11.2,"rate_gb_hours_per_hour":0.17,"estimated_gb_hours":386.54,` +``;
- replace ` || strings.Contains(string(b), "runs_out_at") || strings.Contains(string(b), "over_allowance") {` with ` || strings.Contains(string(b), "runs_out_at") {`.

In `web/src/api/types.test.ts`, delete the line `    expect(snap.quota?.over_allowance_gb_hours).toBe(0)`.

- [ ] **Step 2: Run the tests to see them fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./web/`
Expected: FAIL, `web/src/api/fixtures/github-storage.json is out of date with the Go types`.

- [ ] **Step 3: Change the code and regenerate the fixture**

In `internal/model/ghstorage.go`, find the start of `GitHubQuota`:

```go
type GitHubQuota struct {
	Month                string     `json:"month"`
	ReadAt               time.Time  `json:"read_at"`
	IncludedGBHours      float64    `json:"included_gb_hours"`
	UsedGBHours          float64    `json:"used_gb_hours"`
	ActionsGBHours       float64    `json:"actions_gb_hours"`
	PackagesGBHours      float64    `json:"packages_gb_hours"`
	RateGBHoursPerHour   float64    `json:"rate_gb_hours_per_hour"`
	EstimatedGBHours     float64    `json:"estimated_gb_hours"`
	OverAllowanceGBHours *float64   `json:"over_allowance_gb_hours,omitempty"`
	ProjectedGBHours     float64    `json:"projected_gb_hours"`
	RunsOutAt            *time.Time `json:"runs_out_at,omitempty"`
```

Replace with:

```go
type GitHubQuota struct {
	Month              string     `json:"month"`
	ReadAt             time.Time  `json:"read_at"`
	IncludedGBHours    float64    `json:"included_gb_hours"`
	UsedGBHours        float64    `json:"used_gb_hours"`
	ActionsGBHours     float64    `json:"actions_gb_hours"`
	PackagesGBHours    float64    `json:"packages_gb_hours"`
	RateGBHoursPerHour float64    `json:"rate_gb_hours_per_hour"`
	EstimatedGBHours   float64    `json:"estimated_gb_hours"`
	ProjectedGBHours   float64    `json:"projected_gb_hours"`
	RunsOutAt          *time.Time `json:"runs_out_at,omitempty"`
```

Then regenerate: `GHR_UPDATE_FIXTURES=1 TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./web/`. `git diff web/src/api/fixtures/` must show exactly one removed line, `"over_allowance_gb_hours": 0,` in `github-storage.json`; anything else is a fault to report, not to commit.

- [ ] **Step 4: Run the tests to see them pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./web/ ./internal/...`
Expected: every package `ok`. Then `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/api/types.test.ts`: all pass. gofmt prints nothing and `go vet ./internal/model/ ./web/` passes.

- [ ] **Step 5: Commit**

```bash
git add internal/model/ghstorage.go internal/model/ghstorage_test.go web/fixtures_test.go web/src/api/fixtures/github-storage.json web/src/api/types.test.ts
git commit -m "refactor(model): drop the over-allowance figure"
```

### Task 4: Remove the over-allowance line from the quota card

**Files:**
- Modify: `web/src/api/types.ts` (`GitHubQuota`), `web/src/components/quota-card.tsx`
- Test: `web/src/components/quota-card.test.tsx`

**Interfaces:**
- Consumes: Contracts "After Task 3".
- Produces: Contracts "After Task 4".

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Remove the field from the type**

In `web/src/api/types.ts`, delete the line `  over_allowance_gb_hours?: number` from `interface GitHubQuota`.

- [ ] **Step 2: Run the type check to see it fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit`
Expected: errors in `src/components/quota-card.tsx` (`Property 'over_allowance_gb_hours' does not exist on type 'GitHubQuota'`) and `src/components/quota-card.test.tsx`.

- [ ] **Step 3: Change the card and its test**

In `web/src/components/quota-card.tsx`, delete the line `  const over = q?.over_allowance_gb_hours` and these three lines:

```tsx
            {over !== undefined && over > 0.001 && (
              <p className="text-sm text-destructive">{`GitHub reports use past its free allowance: ${over < 0.1 ? "under 0.1 GB-h" : gbh(over)}.`}</p>
            )}
```

In `web/src/components/quota-card.test.tsx`, find:

```tsx
    ["exhausted", { level: "exhausted", used_gb_hours: 373.3, over_allowance_gb_hours: 0.0016, runs_out_at: undefined }, "Uploads fail until Nov 1 unless a budget is set.", "bg-destructive"],
```

Replace with:

```tsx
    ["exhausted", { level: "exhausted", used_gb_hours: 373.3, runs_out_at: undefined }, "Uploads fail until Nov 1 unless a budget is set.", "bg-destructive"],
```

and delete this test with the blank line after it:

```tsx
  it.each<[number, string | null]>([
    [0.0016, "GitHub reports use past its free allowance: under 0.1 GB-h."],
    [9.5, "GitHub reports use past its free allowance: 9.5 GB-h."],
    [0, null],
  ])("shows GitHub's own figure past the free allowance (%s)", (over, text) => {
    const card = draw({ level: "exhausted", over_allowance_gb_hours: over })
    expect(within(card).queryByText(/past its free allowance/)?.textContent ?? null).toBe(text)
  })
```

- [ ] **Step 4: Run the checks to see them pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0 && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/quota-card.test.tsx src/api/types.test.ts`
Expected: tsc and eslint print nothing; all tests pass. `rg -n "over_allowance|OverAllowance|StorageSummary|SummaryItem|free allowance" --glob '!docs' .` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/types.ts web/src/components/quota-card.tsx web/src/components/quota-card.test.tsx
git commit -m "fix(web): drop the over-allowance line"
```

### Task 5: Re-check the cleanup policy before each delete

**Files:**
- Modify: `internal/ghstorage/cleanup_run.go` (the delete loop in `cleanup`)
- Test: `internal/ghstorage/cleanup_run_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing other tasks use.

**Items:** 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 3 = 4

- [ ] **Step 1: Write the failing test**

Append to `internal/ghstorage/cleanup_run_test.go`:

```go

// The cleanup picks from the config the pass read at its start and re-reads
// the policy before each delete, so a policy saved mid-pass can stop deletes
// but never add them. "list" saves while the inventory lists, before the
// picks; "delete" saves at the first delete.
func TestCleanupRechecksThePolicyBeforeEachDelete(t *testing.T) {
	for _, tc := range []struct {
		name   string
		repo   string
		ages   [2]time.Duration
		when   string
		change func(*config.GitHubStorage)
		want   []string
	}{
		{"turned off", "darkmem", [2]time.Duration{50 * time.Hour, 30 * time.Hour}, "delete",
			func(g *config.GitHubStorage) { g.CleanupMaxAge = 0 }, []string{"DeleteArtifact darkmem 1"}},
		{"lengthened past the second", "darkmem", [2]time.Duration{50 * time.Hour, 30 * time.Hour}, "delete",
			func(g *config.GitHubStorage) { g.CleanupMaxAge = config.Duration(40 * time.Hour) }, []string{"DeleteArtifact darkmem 1"}},
		{"override removed, global longer", "chicken-farm", [2]time.Duration{30 * time.Hour, 10 * time.Hour}, "delete",
			func(g *config.GitHubStorage) { delete(g.Repos, "chicken-farm") }, []string{"DeleteArtifact chicken-farm 1"}},
		{"turned off before the picks", "darkmem", [2]time.Duration{50 * time.Hour, 30 * time.Hour}, "list",
			func(g *config.GitHubStorage) { g.CleanupMaxAge = 0 }, []string{}},
		{"turned on mid-pass", "homelab", [2]time.Duration{50 * time.Hour, 30 * time.Hour}, "list",
			func(g *config.GitHubStorage) { g.Repos["homelab"] = config.StorageOverride{Cleanup: "1h"} }, []string{}},
		{"shortened mid-pass", "darkmem", [2]time.Duration{50 * time.Hour, 20 * time.Hour}, "list",
			func(g *config.GitHubStorage) { g.CleanupMaxAge = config.Duration(10 * time.Hour) }, []string{"DeleteArtifact darkmem 1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGH()
			cur := cleanupConfig()
			s, clock := newTestService(t, gh, cur)
			s.Config = func() *config.Config { return cur }
			gh.addRepo(tc.repo, true, ghArt(1, 10, clock.Now(), tc.ages[0], 5), ghArt(2, 20, clock.Now(), tc.ages[1], 5))
			save := func() {
				next := cleanupConfig()
				tc.change(&next.GitHubStorage)
				cur = next
			}
			if tc.when == "list" {
				gh.onList = func(context.Context, string) error { save(); return nil }
			} else {
				gh.onDelete = func(context.Context, string, int64) error { save(); return nil }
			}
			s.pass(true, false)
			if got := gh.callsLike("Delete"); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("deletes %v, want %v", got, tc.want)
			}
			st := s.Snapshot()
			if len(st.Deletions) != len(tc.want) || len(s.writes) != len(tc.want) {
				t.Fatalf("%d deletions and %d writes, want %d: a skipped pick writes and records nothing", len(st.Deletions), len(s.writes), len(tc.want))
			}
			if got := eventsLike(s.Events, "cleanup deleted"); len(got) != min(len(tc.want), 1) {
				t.Fatalf("events %q", got)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to see it fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 -run TestCleanupRechecksThePolicyBeforeEachDelete ./internal/ghstorage/`
Expected: FAIL in four subtests: `turned_off`, `lengthened_past_the_second` and `override_removed,_global_longer` delete both artifacts; `turned_off_before_the_picks` deletes both instead of none. `turned_on_mid-pass` and `shortened_mid-pass` already pass: they guard that the re-check never adds a delete. A global age turned on mid-pass takes the same path (`CleanupFor` resolves override and global alike) and has no case of its own.

- [ ] **Step 3: Write the implementation**

In `internal/ghstorage/cleanup_run.go`, function `cleanup`, find:

```go
		if !s.keep(gen) {
			break
		}
		deleted, err := s.deleteArtifact(ctx, p.Repo, p.Item, "cleanup")
```

Replace with:

```go
		if !s.keep(gen) {
			break
		}
		// The policy may have been saved since the pass read it. A pick that
		// no longer qualifies by age is skipped, not replaced.
		if age, _ := s.Config().GitHubStorage.CleanupFor(p.Repo); len(Candidates([]model.ArtifactItem{p.Item}, age, s.now(), nil)) == 0 {
			continue
		}
		deleted, err := s.deleteArtifact(ctx, p.Repo, p.Item, "cleanup")
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/`, then `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH CGO_ENABLED=1 timeout 300 go test -race -count=5 ./internal/ghstorage/`
Expected: both `ok`. gofmt prints nothing and `go vet ./internal/ghstorage/` passes.

- [ ] **Step 5: Commit**

```bash
git add internal/ghstorage/cleanup_run.go internal/ghstorage/cleanup_run_test.go
git commit -m "fix(ghstorage): recheck cleanup policy per delete"
```

### Task 6: Run every suite and check on the LXC

**Files:**
- Modify: `docs/superpowers/registers/2026-10-13-ghr-github-storage.md` (Step 6)
- No code changes, unless a check below fails: then fix it in the task the failure belongs to and report it.

**Interfaces:**
- Consumes: everything above.
- Produces: the register's acceptance evidence.

**Items:** 1, 5, 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 2 = 3

- [ ] **Step 1: Ask the owner**

Ask: "All tasks are committed. May I build this branch, deploy it to the runner LXC (192.168.0.99) and restart ghr there to check the quota read-only? Restarting logs every browser out; running jobs keep running. I will not set a cleanup age, change retention or delete anything there." Wait for an explicit yes. Without it, skip Steps 4 and 5, still run Steps 2, 3 and 6.

- [ ] **Step 2: Run the Go suite**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go test -count=1 ./...`
Expected: every package `ok`. Then `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/ web/` prints nothing and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./...` passes.

- [ ] **Step 3: Run the web suite**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 900 npx vitest run`, then `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit`, then `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`.
Expected: every test passes; tsc and eslint print nothing. A test that only times out is the loaded host: run that file once more, alone, before believing it.

- [ ] **Step 4: Build and deploy (only after the yes)**

```bash
B=/root/.cache/ghr-plan-tmp; H=$(git rev-parse --short HEAD)
rm -rf $B/build && mkdir -p $B/build && git archive HEAD | tar -x -C $B/build
ln -sfn "$(git rev-parse --show-toplevel)/web/node_modules" $B/build/web/node_modules
(cd $B/build/web && TMPDIR=$B timeout 600 npm run -s build)
rg -c "free allowance" $B/build/web/dist; echo "matches exit $?"
(cd $B/build && TMPDIR=$B GOTMPDIR=$B PATH=/usr/local/go/bin:$PATH CGO_ENABLED=0 timeout 600 go build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=gh-storage-$H" -o $B/ghr-gh-storage ./cmd/ghr)
timeout 30 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 'curl -s --max-time 5 --unix-socket /run/ghr/ghr.sock http://ghr/status | python3 -c "import json,sys; d=json.load(sys.stdin); print(\"busy\", sum(1 for i in d[\"instances\"] if i[\"state\"]==\"busy\"))"'
```

Expected: the `rg` line prints `matches exit 1` (the built web app has no "free allowance" text). A failing SSH command means the LXC is not reachable from this host: stop and report it. Busy runners block the deploy: when `busy` is above 0, check again every two minutes for at most 30 minutes, then stop and ask the owner. When `busy` is 0:

```bash
OLD=$(timeout 30 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 ghr version)
timeout 120 scp -q -o BatchMode=yes -o ConnectTimeout=10 $B/ghr-gh-storage root@192.168.0.99:/usr/local/bin/ghr.new
timeout 120 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 "cp -p /usr/local/bin/ghr /usr/local/bin/ghr.$OLD.bak && mv /usr/local/bin/ghr.new /usr/local/bin/ghr && systemctl restart ghr && sleep 3 && systemctl is-active ghr && ghr version && grep -c github_storage /etc/ghr/config.yaml"
```

Expected: `active` and `gh-storage-<sha>`. Record the `grep -c` count (0 means no `github_storage` block, so cleanup is off) and the backup name `ghr.$OLD.bak` for Step 6.

- [ ] **Step 5: Read-only checks (only after the yes)**

Wait 2 minutes for the first billing read, then:

```bash
timeout 30 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 'curl -s --max-time 10 --unix-socket /run/ghr/ghr.sock http://ghr/github-storage' > /root/.cache/ghr-plan-tmp/ghs.json
python3 -c "import json; d=json.load(open('/root/.cache/ghr-plan-tmp/ghs.json')); q=d.get('quota') or {}; print('quota_error', d.get('quota_error')); print('over key present', 'over_allowance_gb_hours' in q); print({k: q.get(k) for k in ('month','used_gb_hours','included_gb_hours','level')})"
timeout 30 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 'ghr status' | rg '^artifacts'
```

Expected: `over key present False`; `level` is `exhausted` exactly when `used_gb_hours` ≥ `included_gb_hours`, otherwise one of `critical`, `warn`, `projected` or empty; and the `ghr status` line (`artifacts  <used> / <included> GB-h this month (<pct>%)`) ends with `, exhausted until Nov 1` for `exhausted`, contains `, critical` or `, warning` for those levels (a `, runs out around <date>` may follow), and contains neither otherwise. While the month is October 2026, used is above 363.28, so the level is `exhausted`: that matches GitHub refusing uploads (darkcloud run 38267451928). When `quota_error` is set, record it and treat this step as not passed.

- [ ] **Step 6: Record the results**

Run, from the repository root:

```bash
R=docs/superpowers/registers/2026-10-13-ghr-github-storage.md
REG="$(ls -d /root/.claude/plugins/cache/darkraise/dr-superpowers/*/ | sort -V | tail -1)scripts/register"
bash "$REG" set $R 5 done --note "Summary read and over-allowance field removed; Go and web suites passed <date>"
bash "$REG" set $R 6 done --note "Per-delete re-check with TestCleanupRechecksThePolicyBeforeEachDelete; suites passed <date>"
bash "$REG" set $R 1 done --note "LXC <date>, build gh-storage-<sha>: level <level> with used <used> of <included>, no over-allowance key, ghr status agrees; the billing cycle check lives in row 12"
bash "$REG" set $R 2 verify --note "Row 6 shipped in gh-storage-<sha>; a first live cleanup needs the owner's go-ahead and choice of repository"
bash "$REG" set $R 12 open --note "Owner checks. 2026-11-01 check: the quota clears at the month change, November starts near 0, uploads work again. LXC backups to remove when satisfied: ghr.config-items-19863e5.bak, ghr.<OLD>.bak"
```

Fill each `<...>` from Steps 2 to 5. When Step 5 did not pass or did not run, record row 1 as `verify` with what failed or `LXC check not run: no owner yes on <date>`. When nothing was deployed, row 2's note reads `Row 6 committed at <short sha>, not deployed; a first live cleanup needs the owner's go-ahead and choice of repository`, and row 12's note leaves out the new backup name. Commit: `git add docs/superpowers/registers/2026-10-13-ghr-github-storage.md && git commit -m "docs(register): record storage amendment checks"`.
