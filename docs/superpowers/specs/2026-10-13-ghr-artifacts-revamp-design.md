# ghr: Artifacts page revamp

**Date:** 2026-10-13
**Status:** design presented in chat in three parts and approved by the owner on 2026-10-13, with the rulings on where the policy lives, the revamp's scope, the save model and the execution strategy made the same day. Replaces item 2 (Policy card) of section 4 in `2026-10-13-ghr-github-storage-design.md`; the rest of that section stands except where section 2 below changes it.
**Execution strategy:** sequential — the Settings move and the table changes both edit `pages/artifacts.tsx` and the policy code it imports, so the work is one chain.
**Register:** docs/superpowers/registers/2026-10-13-ghr-artifacts-revamp.md (rows 1 and 2).
**Code:** `web/src` only (Settings, the Artifacts page and its components, one form hook), `README.md`. No daemon or API change.

## Goal

The storage policy (included MB, warning levels, global cleanup age) moves from the Artifacts page to a section of its own in Settings, and the Artifacts page becomes shorter and easier to scan.

On 2026-10-13 the owner's account listed 64 repositories, 56 of them with no artifact, cache or error, and all 64 at GitHub's 90-day retention. The page showed every one of them, each with the same 90-day warning under its name, below a policy form.

## Decisions

- **The policy lives in Settings, not a dialog** (owner's ruling, 2026-10-13). It is `config.yaml` configuration like the other Settings sections.
- **The section saves through the save bar, like General** (owner's ruling, 2026-10-13), with the leave guard.
- **Revamp scope** (owner's ruling, 2026-10-13): hide empty repositories behind a switch, one retention line in place of the per-row hint, quota stat tiles, a filter box and a totals row.
- **Repositories sort by artifact size, then cache size, then name** (owner's ruling, 2026-10-13). Artifacts are what the quota bills; the README already says the Actions cache does not count against the allowance.
- **Per-repository retention and cleanup stay in the Artifacts table**, and **Set retention for all stays on the Artifacts page**: they act on GitHub or on one repository's row at once, not through the save bar.

## 1. The GitHub storage section in Settings

- `pages/settings.tsx`: `CONFIG` gains `{ id: "github-storage", title: "GitHub storage" }` between "GitHub token" and "Runner software", so the route is `/settings/github-storage`. The Host group's "Storage" section (disk use) is unchanged.
- The section shows a `PageHeader` titled "GitHub storage" with the description "Saved in config.yaml as github_storage.", then one `FieldGroup` with the three fields of today's Policy card, with the same inputs, help texts and checks:
  - Included storage: a whole number of MB, at least 1.
  - Warning levels: two whole percentages from 1 to 100, the first lower.
  - Cleanup age: empty means off; the daemon checks the 24-hour minimum, as today.
- Under the group, one line: "A repository's own cleanup age and retention are set in its row on the Artifacts page", where "Artifacts page" links to `/artifacts`.
- While the config loads it shows a spinner, and a failed read shows an error line with Retry, as General does. Offline (the status read failing) disables the inputs.

### 1.1 Saving

- A new hook `useStoragePolicyForm()` in `lib/storage-policy-form.ts` holds the draft, built on `lib/draft.ts` (`changedKeys`, `settle`), as `lib/settings-form.ts` does for General.
- The section renders `SaveBar` with the changed-field count, and `UnsavedGuard` with `page="GitHub storage"`.
- `save(): Promise<boolean>`:
  - Nothing changed: `true`. A field error: a toast "Fix the highlighted settings first" and `false`.
  - The cleanup age changed to a non-empty value: it reads `GET /github-storage/preview?cleanup=<age>`, opens the confirm dialog with today's title, body and `CleanupPreview` details, and returns `false`. Confirming sends the patch; cancelling keeps the draft. A failed preview read shows as the section's rejection line and saves nothing.
  - Otherwise it sends `PATCH /config` with `github_storage` holding only the changed keys, as today, and returns whether that succeeded.
- Because a save that needs the preview returns `false`, the leave guard keeps the page and the preview opens there: nothing leaves until the owner confirms, and confirming does not then navigate. Pressing Save on the leave prompt runs the same `save()`.
- After a successful send: the toast "Storage policy saved", the config and GitHub storage queries are invalidated, and the draft is settled against what was sent. A 4xx refusal shows the daemon's message at the top of the section with the toast "Storage policy not saved"; any other failure only toasts it with the error, as today.

### 1.2 Files

- `components/cleanup-preview.tsx`: `CleanupPreview`, moved unchanged from `storage-policy-card.tsx`. `artifact-repo-table.tsx` imports it from here.
- `components/github-storage-section.tsx`: the section. It replaces `components/storage-policy-card.tsx`, which is deleted with its test.
- `components/retention-all.tsx`: the "Retention for all" field and its confirm, moved from the Policy card for section 2.4.

## 2. The Artifacts page

Top to bottom: the header, the quota, the operation progress, the repositories, the recent deletions. The quota banner in the shell is unchanged.

### 2.1 Header

- The description reads `Checked <time>` as today, then, once the config has loaded, ` · cleanup <global age or off> · warnings at <warn>% and <critical>%`, with the defaults (off, 70 and 90) when `config.yaml` has no `github_storage` block. When repositories override the global age it adds ` (<n> repository overrides)`, counting `config.github_storage.repos`.
- Actions: a **Policy** link styled as an outline button to `/settings/github-storage`, then **Check now** as today.

### 2.2 Quota

The `Quota` section keeps its "Billing read <time>" aside, its error line and its "No billing read yet." state. With a quota it shows:

- Four tiles using `StatCard` from `components/stat-card.tsx`, in a grid of two columns below 641 px and four above, with the Dashboard's borders:
  - **Used**: the used share of the allowance as a whole percentage, with the unit `<used> of <included> GB-h`. The value is `text-destructive` at `critical` or `exhausted` and `text-warning` at `warn` or `projected`.
  - **Estimated now**: `<estimate> GB-h`.
  - **Projected by <reset day>**: `<projection> GB-h`.
  - **Resets**: the reset day, such as `Nov 1`, with the unit `in <n> days` (`in 1 day` for one), n being the calendar days in UTC from today to the reset day. The reset day is always after today: on it the month, and so `resets_at`, has moved on.
- Under the tiles, the existing meter with its estimate and projection markers, then the bold outlook line, then `Actions <n> GB-h, Packages <n> GB-h`, then the recalculation note in small text. The legend list under the meter stays, since the markers need it.

### 2.3 Operation progress

Unchanged.

### 2.4 Repositories

- **Toolbar**, the first row inside the section:
  - A text input with the label "Filter repositories" that matches repository names containing its text, ignoring case.
  - A switch "Show <n> empty repositories", off on every visit, where n counts every empty repository in the snapshot, whatever the filter. Absent when there are none.
- **Retention row**, under the toolbar:
  - "Retention for all" with the days input and **Set retention** button of `retention-all.tsx`: the same 1 to 400 check, the same confirm ("Set retention to <n> days on <count>?"), the same `POST /github-storage/retention`, disabled while a bulk operation runs.
  - While the quota level is not none, before it: "<n> repositories keep GitHub's 90-day default; lowering retention saves storage.", where n counts rows with `retention_days === 90`, in the warning color. Absent when n is 0.
  - The per-row 90-day hint is removed.
- **Rows**:
  - A repository is empty when its artifact count and cache count are both 0 and it has no error.
  - Shown: every non-empty repository; empty ones too when the switch is on; and, while the filter has text, every repository whose name matches, empty or not.
  - The filter applies to every row: with text, a non-matching repository is hidden whatever the switch says.
  - Order: artifact bytes descending, then cache bytes descending, then name ascending ignoring case.
  - With rows to show but none shown: "No repository matches." in the table body.
  - Expanding a row, the per-row retention and cleanup editors and the row error stay as they are.
- **Totals**: a last row in the table foot, `All <n> repositories`, with the artifact and cache sums (size and count) in their columns, over every repository in the snapshot whatever is shown. Absent when the snapshot has no repository.

### 2.5 Recent deletions

Unchanged.

## 3. README

In "GitHub artifact storage", the sentence on what the Artifacts tab shows no longer lists the cleanup policy, and says the policy is set under Settings, GitHub storage. Step 2 names where the global cleanup age is set (Settings, GitHub storage) and where a repository's override is set (its row on the Artifacts page).

## 4. Testing

Vitest, in the existing test files' style:

- `github-storage-section.test.tsx` (replacing `storage-policy-card.test.tsx`): each field's error; edits counted in the save bar; a save with an unchanged age sends without a preview; a changed age opens the preview and cancelling sends nothing and keeps the draft; an emptied age sends without a preview; the leave guard blocks a navigation with unsaved edits; a 4xx shows the daemon's message.
- `settings.test.tsx`: the side nav has "GitHub storage" and `/settings/github-storage` renders the section.
- `quota-card.test.tsx`: the four tiles' values for the fixture; the Used tile's color per level; the error and no-read states.
- `artifact-repo-table.test.tsx`: empty rows hidden with the switch's count right; the switch shows them; the filter ignores case and shows a matching empty repository; the sort order including ties; the totals over every repository whatever is shown; the 90-day line only while the level is not none, with its count; Set retention for all confirms before it calls the API; "No repository matches."
- `artifacts.test.tsx`: no Policy card; the header summary with and without overrides; the Policy link's target.

Before the work is called done: the web suite, `tsc` and lint; the Go suite (the web build is embedded); screenshots against the live snapshot data at 1400 px and 390 px wide in light and dark, with no horizontal page scroll at 390 px. Deploying to the LXC needs the owner's yes.
