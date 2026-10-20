# Self-hosted artifacts

Repositories you switch on store their Actions artifacts on your own S3-compatible server, such as [RustFS](https://docs.rustfs.com), instead of GitHub's artifact storage, so they stop using the plan's [storage allowance](artifact-storage.md). Jobs on ghr's runners and on GitHub-hosted runners upload to the same bucket. Workflows use ghr's upload and download actions; in a repository that is not switched on, those actions run GitHub's own, so nothing changes until you turn a repository on.

Release assets, caches and Packages stay on GitHub.

## How it works

- **Two access keys.** ghr holds an **admin key**, which lists, reads and deletes the bucket's objects for the Artifacts page and cleanup and never leaves the host, and a **job key**, which can list, read and write the bucket but not delete.
- **One variable and one secret per repository.** Turning a repository on makes ghr write two things into it: the `GHR_ARTIFACT_STORE` repository variable, which names the endpoint, bucket, region and the job key's ID, and the `GHR_ARTIFACT_KEY` repository secret, which holds the job key. Turning it off deletes both.
- **The actions decide per run.** `darkraise/ghr/actions/upload` and `/download` read the variable. When it is empty they run `actions/upload-artifact@v4` and `actions/download-artifact@v4` with the same inputs; when it is set they zip and upload to the bucket themselves, or download and unzip from it.
- **Layout.** Each artifact is one object, `<owner>/<repo>/<run id>/<name>.zip`, zipped the way `actions/upload-artifact@v4` zips it.

## Set up RustFS

The server must be reachable over HTTPS at one address from every runner, GitHub-hosted ones included. ghr and the actions refuse a plain `http://` endpoint.

1. **Create a bucket**, for example `gha-artifacts`.
2. **Create two policies.** Replace `gha-artifacts` with your bucket's name. The admin policy:

   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {
         "Effect": "Allow",
         "Action": ["s3:ListBucket", "s3:ListBucketMultipartUploads"],
         "Resource": ["arn:aws:s3:::gha-artifacts"]
       },
       {
         "Effect": "Allow",
         "Action": ["s3:GetObject", "s3:DeleteObject", "s3:AbortMultipartUpload"],
         "Resource": ["arn:aws:s3:::gha-artifacts/*"]
       }
     ]
   }
   ```

   The job policy, which has no delete:

   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {
         "Effect": "Allow",
         "Action": ["s3:ListBucket"],
         "Resource": ["arn:aws:s3:::gha-artifacts"]
       },
       {
         "Effect": "Allow",
         "Action": ["s3:GetObject", "s3:PutObject", "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"],
         "Resource": ["arn:aws:s3:::gha-artifacts/*"]
       }
     ]
   }
   ```

3. **Create two access keys**, one with each policy, in the RustFS console or the admin tool your server uses. Keep each key's ID and secret for the next section.

ghr aborts multipart uploads left open for more than 24 hours, so the bucket needs no lifecycle rule for them.

**Behind Cloudflare's proxy.** Through a Cloudflare tunnel to a RustFS server, signed HEAD requests were refused with 403, so ghr reads object details with a ranged GET instead. Uploads whose requests ran longer than about 100 seconds still failed with a 524 timeout. For large uploads, point ghr and the runners at the store's direct address where you can.

## Set up ghr

1. **Grant the token two more permissions** on the repositories you will switch on: Variables: read and write, and Secrets: read and write. [Installation](installation.md#requirements) lists them with the others. Until you switch a repository on, the token can do without them: ghr reads each switched-off repository once after it starts, to remove anything of its own left there, and a refusal there counts as off.
2. **Enter the connection** under Settings → Self-hosted storage: the endpoint (such as `https://s3.example.com`), the bucket and the region. RustFS accepts any region; keep `us-east-1` unless your server says otherwise. Press **Save changes**: Test connection checks the saved connection, not what is typed.
3. **Enter both keys** with each key's **Set** button (it reads **Replace** once a key is set). ghr keeps them in `/etc/ghr/artifact-store-keys`, readable only by root, and shows only a shortened key ID afterwards. On the host, `ghr artifact-store set-key admin` and `ghr artifact-store set-key job` do the same: at a terminal they ask for the ID and the secret without echoing them, and from a file or pipe they read them as two lines:

   ```bash
   ghr artifact-store set-key job < job-key.txt
   ```

4. **Test the connection.** Test connection reaches the endpoint, lists the bucket and reads one object's metadata, all with the admin key. A workflow's first upload proves the job key.
5. **Set the policy.** The cleanup age applies to artifacts uploaded without `retention-days`: 7 days unless you change it, at least 24 hours, or `off`. Capacity, in decimal GB, is optional; the Artifacts page shows use against it. ghr does not read the server's disk.
6. **Turn repositories on** in the Artifacts page's Self-hosted tab, one at a time or with **Turn on for all private repositories**. ghr writes the repository's variable and secret before it answers, so the next run uses the bucket. Public repositories cannot be switched on.

The settings live in `config.yaml`:

```yaml
artifact_store:
  endpoint: https://s3.example.com   # HTTPS only
  bucket: gha-artifacts
  region: us-east-1                  # the default when empty
  cleanup_max_age: 7d                # empty is 7d; off, or at least 24h
  capacity_gb: 50                    # optional, decimal GB
  repos:                             # written by the Artifacts page
    my-repo: { enabled: true, cleanup: 3d }   # a repository's own age: at least 1h, or off
```

Edit `repos` from the Artifacts page rather than by hand: the page checks that a repository is private and served or watched before it switches it on.

## Move workflows

Replace `actions/upload-artifact` and `actions/download-artifact` with ghr's actions, and pass the key to those two steps:

```yaml
env:
  GHR_ARTIFACT_STORE: ${{ vars.GHR_ARTIFACT_STORE }}   # empty: GitHub storage

jobs:
  build:
    runs-on: [self-hosted, linux, x64]
    steps:
      - uses: actions/checkout@v4
      - run: make dist
      - uses: darkraise/ghr/actions/upload@vX.Y.Z
        with:
          name: dist
          path: dist/
          key: ${{ secrets.GHR_ARTIFACT_KEY }}

  test:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: darkraise/ghr/actions/download@vX.Y.Z
        with:
          name: dist
          key: ${{ secrets.GHR_ARTIFACT_KEY }}
```

- **Reference a release tag.** Replace `vX.Y.Z` with a ghr release, such as the one you run; Settings → Self-hosted storage shows the snippet with it filled in. The actions fetch `ghr-artifact` from that release and check it against the release's `ghr-artifact-checksums.txt` on every run. A commit SHA or branch fails with "reference a ghr release tag such as @vX.Y.Z, or set `version`".
- **Pass the key per step.** Give `key` only to the upload and download steps, not to the job's `env`, so no other step sees it.
- **Inputs.** The upload action takes the inputs of `actions/upload-artifact@v4`; the download action takes those of `actions/download-artifact@v4` except `repository`. Both also take `store` (the variable's value; `GHR_ARTIFACT_STORE` from the environment when empty), `key` and `version` (the release to fetch the tool from, when the reference is not a release tag).
- **Outputs.** The upload sets `object-key` and `artifact-digest`; the download sets `download-path`.
- **Runners.** The tool is built for Linux on x64 and ARM64 and for Windows on x64. macOS runners are not supported.

Adding the actions to a repository that is still off changes nothing: its variable is empty, so they run GitHub's actions.

## When a repository is off

- **Uploads go to GitHub.** With the variable empty, the actions run `actions/upload-artifact@v4` and `actions/download-artifact@v4`, pinned by commit, with the same inputs.
- **Downloads fall back.** When the bucket holds no artifact that matches, the download action runs `actions/download-artifact@v4` with the same inputs. This covers runs from before the switch and steps that still use GitHub's upload. A download by `run-id` needs `github-token` for that fallback, as GitHub's action does.
- **Turning a repository off** deletes ghr's variable and secret. Its artifacts already in the bucket stay until they expire or cleanup removes them.

## Managing artifacts

- **Artifacts page, Self-hosted tab.** The bucket's use, each repository's switch, state and cleanup, and, under each repository, its artifacts with **Download**, **Copy link** and **Delete**. A copied link is valid for 1 hour, 24 hours or 7 days; anyone with it can download that artifact straight from the server until it expires. The link is a presigned URL signed with the admin key, so its `X-Amz-Credential` parameter carries the admin key's ID in full, not shortened as everywhere else ghr shows key IDs; the secret is never in it. Recent deletions lists both stores.
- **Run page.** A run's Artifacts card lists its GitHub and self-hosted artifacts together, each marked with its store.
- **Cleanup.** At the end of each pass, every 15 minutes, ghr deletes artifacts whose `retention-days` have passed, whatever their run's state, and artifacts without `retention-days` that are older than the repository's cleanup age, except those of runs that have not completed. It deletes at most 100 a pass and never touches objects outside your owner's folder, which the usage counts as other.
- **Command line.** `ghr artifact-store` prints the connection, totals and each repository's state; `ghr status` adds one line with the use and the number of repositories on.

## Limits

- **One job key.** Every switched-on repository shares the job key, so each can read and write the others' artifacts, though none can delete. Anyone who can push a workflow to such a repository can read the key. Replacing it under Settings makes ghr rewrite every repository's secret at its next pass.
- **Duplicate names.** An upload refuses a name the same run attempt already uploaded unless `overwrite: true` is set. Two uploads of one name running at the same moment both pass that check, and the last to finish wins.
- **Re-run attempts.** A run keeps one object per name: a re-run attempt that uploads the same name replaces the earlier attempt's artifact, so every attempt's run page shows the latest upload.
- **Patterns across stores.** The download falls back to GitHub only when the bucket matched nothing, so a `pattern` that matches artifacts in both stores downloads only the bucket's.
- **Shorter keeping.** Without `retention-days`, artifacts go after 7 days, where GitHub keeps them 90 days by default. Check workflows that read older artifacts, such as `workflow_run` workflows, downloads by run ID and nightly jobs reading the last build, and give them `retention-days` or a longer cleanup age.
- **Fork pull requests.** GitHub gives pull requests from forks of a private repository no secrets by default, so their upload step fails rather than falling back to GitHub.
- **Other repositories.** The download action cannot fetch another repository's artifacts.
- **Moving the store.** ghr counts a `GHR_ARTIFACT_STORE` variable as its own only while it names the endpoint and bucket set in Settings. Turn every repository off before you change either. Otherwise each repository keeps a variable and secret pointing at the old server, its row shows another ghr's variable that ghr leaves alone, and you remove them with `gh variable delete GHR_ARTIFACT_STORE -R <repo>` and `gh secret delete GHR_ARTIFACT_KEY -R <repo>`.
- **Removing a repository by hand.** Remove repositories through ghr, not by editing `config.yaml`. A repository taken out of `repos` or `watch_repos` by hand is no longer looked after, so it keeps ghr's `GHR_ARTIFACT_STORE` variable and `GHR_ARTIFACT_KEY` secret, and its `artifact_store.repos` entry stays, which switches it on again if you add the repository back. Switch it off first, or delete the variable and the secret with `gh variable delete GHR_ARTIFACT_STORE -R <repo>` and `gh secret delete GHR_ARTIFACT_KEY -R <repo>`.
- **Downgrades.** A ghr release from before this feature refuses a config with an `artifact_store` block; remove the block before downgrading.

## Messages

| Message | What to do |
|---|---|
| `pass key: ${{ secrets.GHR_ARTIFACT_KEY }} to the action` | Add the `key` input to the step. |
| `GHR_ARTIFACT_KEY is out of date; turn the repository off and on in ghr` | The secret holds an older job key than the variable names; switching the repository off and on rewrites both. |
| `an artifact named <name> was already uploaded by this run attempt; set overwrite: true to replace it` | Give the artifact another name, or set `overwrite: true`. |
| `<name>.zip is damaged: its digest does not match; upload it again` | The object changed after its upload; run the uploading job again. |
