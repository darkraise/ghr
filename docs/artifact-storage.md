# Artifact storage

A GitHub plan includes a monthly allowance of storage for Actions artifacts and Packages, 500 MB on the Free plan. GitHub counts it in GB-hours over the month, so an artifact kept for days keeps using the allowance long after its upload, and deleting artifacts once the allowance is spent does not let uploads through again before the next month.

The **Artifacts** page shows the month's use against the allowance and each repository's artifacts, caches, retention and cleanup. The storage policy is set under **Settings → GitHub storage**. A banner on every page warns before the allowance runs out; **I know** hides it until the level rises or the month changes. The [token permissions](installation.md#requirements) it needs are listed with the requirements.

## Keeping under the allowance

1. **Lower the retention** to a few days on repositories whose artifacts are only used within their own run: a repository's Retention on the Artifacts page, or **Set retention** for all of them. GitHub's retention also limits how long it keeps runs and logs, which the Actions and log pages read.
2. **Set a cleanup age** for the rest, under Settings → GitHub storage; a repository's own age is set in its row on the Artifacts page. At the end of each inventory, every 15 minutes, ghr deletes the artifacts older than it, oldest first and at most 100 at a time, skipping the artifacts of runs that have not completed. The global age is at least 24 hours; a repository's override may go down to 1 hour, or turn cleanup off for it.

Cleanup does not protect an artifact that a different run downloads later, such as one a `workflow_run` workflow, a download by run ID or a nightly job reading the last build needs.

The Actions cache does not count against the allowance and is never cleaned automatically. A repository's caches can be deleted from its row.

## Configuration

```yaml
github_storage:
  included_mb: 500            # the plan's allowance in MB; 500 unless set
  warn_pct: [70, 90]          # the estimate's warning and critical levels
  cleanup_max_age: 2d         # absent is off; at least 24h
  repos:
    my-repo: { cleanup: 6h }  # at least 1h
    other-repo: { cleanup: off }
```

The month is taken to be the calendar month in UTC.
