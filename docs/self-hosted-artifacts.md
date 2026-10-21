# Self-hosted artifacts

Repositories you switch on store the Actions artifacts of jobs on ghr's runners on your own S3-compatible server, such as [RustFS](https://docs.rustfs.com), instead of GitHub's artifact storage, so those jobs stop using the plan's [storage allowance](artifact-storage.md). Only private repositories ghr serves can be switched on, and only jobs that run on ghr's own runners use the server; every other job, a GitHub-hosted one included, uses GitHub's artifact storage. Workflows use ghr's upload and download actions, which run GitHub's own wherever the store does not apply, so nothing changes until you turn a repository on.

Release assets, caches and Packages stay on GitHub.

## How it works

- **Two access keys.** ghr holds an **admin key**, which lists, reads and deletes the bucket's objects for the Artifacts page and cleanup and never leaves the host, and a **job key**, which can list, read and write the bucket but not delete. Only ghr's runners receive the job key.
- **A settings file per runner.** When ghr starts a runner for a repository that is switched on, served and private, it writes the endpoint, bucket, region and job key into `_work/_ghr/artifact-store.json` in the runner's instance directory. The `_ghr` folder and the file are readable only by the `ghrunner` user the runners run as, and they go with the instance directory when the runner ends. Nothing about the store is written to GitHub: no repository variable and no secret.
- **The actions decide per job.** `darkraise/ghr/actions/upload` and `/download` start by looking for that file next to the job's workspace. When they find it they zip and upload to the bucket themselves, or download and unzip from it; when they do not, they run `actions/upload-artifact@v4` and `actions/download-artifact@v4` with the same inputs.
- **Layout.** Each artifact is one object, `<owner>/<repo>/<run id>/<name>.zip`, zipped the way `actions/upload-artifact@v4` zips it.

## Who uses the store

- **Served, private repositories.** A repository can be switched on only when it is listed under Repositories, so ghr runs its jobs, and is private. A watched repository cannot be switched on, since ghr runs no runners for it, and neither can a public one, a served public repository with `allow_public` included.
- **Jobs on ghr's runners.** In a switched-on repository only jobs that run on ghr's runners use the store. A job on a GitHub-hosted runner, or on any other self-hosted runner, finds no settings file and uses GitHub's storage, as if the repository were off.
- **Linux only.** ghr's runners are Linux x64 runners on ghr's host. A Windows or macOS runner is never one of ghr's, so its jobs always use GitHub's storage through the same actions.

## Set up RustFS

The server must be reachable at one address from ghr's host, where ghr and its runners run, and from the browsers that use the Artifacts page's **Download** and **Copy link**. [The endpoint](#the-endpoint) says which addresses ghr accepts.

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

**Behind Cloudflare's proxy.** Through a Cloudflare tunnel to a RustFS server, signed HEAD requests were refused with 403 and Range headers were dropped, so ghr reads object details from the headers of a plain GET. Uploads whose requests ran longer than about 100 seconds failed with a 524 timeout. Point ghr at the store's direct address where you can, such as its LAN address over `http://`.

## The endpoint

ghr accepts:

- `https://host[:port]` for any host name or IP address.
- `http://ip[:port]` when the IP address is private (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`), loopback (`127.0.0.0/8`, `::1`) or link-local (`169.254.0.0/16`, `fe80::/10`), such as `http://192.168.1.20:9000` or `http://[fd00::20]:9000`. An IPv4-mapped IPv6 address, such as `[::ffff:192.168.1.20]`, counts as its IPv4 address.

ghr refuses:

- `http://` with a host name, `localhost` included, and with any other address: a public one, `0.0.0.0`, `::`, or a carrier-grade NAT address in `100.64.0.0/10`;
- an IPv4 address written in any but the plain dotted form, such as `010.0.0.1` or `10.1`;
- an IPv6 zone, such as `http://[fe80::1%25eth0]:9000`, with either scheme;
- a path (a trailing `/` included), a query, a fragment or user info.

The Settings form, config loading and ghr-artifact apply the same rule, and the [Messages](#messages) table lists what each refusal says. A loopback endpoint such as `http://127.0.0.1:9000` reaches the server only from the host itself: inside a `container:` job it names the container, and **Download** and **Copy link** send the browser to its own machine. [Limits](#limits) says what else changes with `http://`.

## Set up ghr

ghr needs no token permission for the store beyond those it already has. [Installation](installation.md#requirements) says what the Variables and Secrets permissions still do.

1. **Enter the connection** under Settings → Self-hosted storage: the endpoint (such as `https://s3.example.com` or `http://192.168.1.20:9000`), the bucket and the region. RustFS accepts any region; keep `us-east-1` unless your server says otherwise. Press **Save changes**: Test connection checks the saved connection, not what is typed.
2. **Enter both keys** with each key's **Set** button (it reads **Replace** once a key is set). ghr keeps them in `/etc/ghr/artifact-store-keys`, readable only by root, and shows only a shortened key ID afterwards. On the host, `ghr artifact-store set-key admin` and `ghr artifact-store set-key job` do the same: at a terminal they ask for the ID and the secret without echoing them, and from a file or pipe they read them as two lines:

   ```bash
   ghr artifact-store set-key job < job-key.txt
   ```

3. **Test the connection.** Test connection reaches the endpoint, lists the bucket and reads one object's metadata, all with the admin key. A workflow's first upload proves the job key.
4. **Set the policy.** The cleanup age applies to artifacts uploaded without `retention-days`: 7 days unless you change it, at least 24 hours, or `off`. A repository's own age, set in its row on the Artifacts page, may go down to 1 hour. Capacity, in decimal GB, is optional; the Artifacts page shows use against it. ghr does not read the server's disk.
5. **Turn repositories on** in the Artifacts page's Self-hosted tab, one at a time or with **Turn on for all private repositories**, which switches on every private repository ghr serves. Watched and public repositories have no switch. A switch reaches the runners ghr starts afterwards: in `all` mode a warm runner that was already idle runs its one job with GitHub's storage.

The settings live in `config.yaml`:

```yaml
artifact_store:
  endpoint: https://s3.example.com   # or http://<private IP>:<port>; no path
  bucket: gha-artifacts
  region: us-east-1                  # the default when empty
  cleanup_max_age: 7d                # empty is 7d; off, or at least 24h
  capacity_gb: 50                    # optional, decimal GB
  repos:                             # written by the Artifacts page
    my-repo: { enabled: true, cleanup: 3d }   # a repository's own age: at least 1h, or off
```

Edit `repos` from the Artifacts page rather than by hand: the page checks that a repository is served and private before it switches it on. An entry for a repository that is not under `repos` stays in the file but counts as off.

## Move workflows

Replace `actions/upload-artifact` and `actions/download-artifact` with ghr's actions. They need no secret, variable or `env`:

```yaml
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

  test:
    needs: build
    runs-on: [self-hosted, linux, x64]
    steps:
      - uses: darkraise/ghr/actions/download@vX.Y.Z
        with:
          name: dist
```

Keep the jobs that share an artifact on ghr's runners: a GitHub-hosted job cannot download an artifact a ghr job stored (see [Limits](#limits)).

- **Reference a release tag.** Replace `vX.Y.Z` with a ghr release, such as the one you run; Settings → Self-hosted storage shows the snippet with it filled in. The actions fetch `ghr-artifact` from that release and check it against the release's `ghr-artifact-checksums.txt` on every run that uses the store. A commit SHA or branch fails with "reference a ghr release tag such as @vX.Y.Z, or set `version`".
- **Inputs.** The upload action takes the inputs of `actions/upload-artifact@v4`; the download action takes those of `actions/download-artifact@v4` except `repository`. Both also take `version` (the release to fetch the tool from, when the reference is not a release tag). `store` and `key` are still accepted, so older workflows run without warnings about unexpected inputs, but they are ignored.
- **Outputs.** The upload sets `object-key` and `artifact-digest`; the download sets `download-path`. The upload's `artifact-id` and `artifact-url` are set only when it went to GitHub, and a download by `artifact-ids` always uses GitHub.
- **Runners.** On ghr's runners the actions fetch `ghr-artifact` for Linux x64. Elsewhere they find no settings file, fetch nothing and run GitHub's actions, so the same workflow also runs on Windows, macOS and GitHub-hosted runners.
- **Container jobs.** A `container:` job running as root, the default, finds the settings file at `/__w/_ghr/artifact-store.json` and uses the store. A container job running as another user, through `options: --user` or the image's `USER`, cannot read it and may not be able to write the step's outputs either: the actions print a warning and hand the job to GitHub's actions. Whether those work for that user is up to GitHub's actions, which write step outputs too (not verified). The store steps run in `bash`, so the job's image needs it: in an image without `bash`, such as plain `alpine`, the upload or download step fails instead of falling back to GitHub.

Adding the actions to a repository that is still off changes nothing: its runners get no settings file, so the actions run GitHub's.

## When the store is not used

- **Uploads go to GitHub.** Without the settings file, in a switched-off repository or on a runner that is not ghr's, the actions run `actions/upload-artifact@v4` and `actions/download-artifact@v4`, pinned by commit, with the same inputs.
- **Downloads fall back.** On a ghr runner that has the settings file, when the bucket holds no artifact that matches, the download action runs `actions/download-artifact@v4` with the same inputs. This covers runs from before the switch, steps that still use GitHub's upload, and artifacts that jobs on other runners uploaded. A download by `run-id` needs `github-token` for that fallback, as GitHub's action does.
- **Turning a repository off** makes ghr delete the settings file from that repository's starting and idle runners at its next poll (every `poll_interval`, 10 seconds by default). A runner already running a job keeps its file until the job ends. The repository's artifacts already in the bucket stay until they expire or cleanup removes them.

## Managing artifacts

- **Artifacts page, Self-hosted tab.** The bucket's use, each repository's switch, state and cleanup, and, under each repository, its artifacts with **Download**, **Copy link** and **Delete**. [Web UI](web-ui.md#self-hosted-artifacts) lists the row states. A copied link is valid for 1 hour, 24 hours or 7 days; anyone with it can download that artifact straight from the server until it expires. The link is a presigned URL signed with the admin key, so its `X-Amz-Credential` parameter carries the admin key's ID in full, not shortened as everywhere else ghr shows key IDs; the secret is never in it. Recent deletions lists both stores.
- **Run page.** A run's Artifacts card lists its GitHub and self-hosted artifacts together, each marked with its store.
- **Cleanup.** At the end of each pass, every 15 minutes, ghr deletes artifacts whose `retention-days` have passed, whatever their run's state, and artifacts without `retention-days` that are older than the repository's cleanup age, except those of runs that have not completed. It deletes at most 100 a pass and never touches objects outside your owner's folder, which the usage counts as other.
- **Command line.** `ghr artifact-store` prints the connection, totals and each repository's state; `ghr status` adds one line with the use and the number of served repositories switched on.

## Limits

- **Mixed runners.** A job on a GitHub-hosted runner, or on any runner that is not ghr's, cannot download an artifact a ghr job stored: it reads only GitHub's storage. The other way round works, since a ghr job's download falls back to GitHub when the bucket has no match. Run the jobs that download an artifact on ghr's runners too.
- **One job key.** Every switched-on repository's runners get the same job key, so each repository's jobs can read and write the others' artifacts, though none can delete. Anyone who can push a workflow to a switched-on repository can read the key. Replacing it under Settings makes ghr rewrite the settings file of every starting and idle runner at its next poll; runners started afterwards get the new key, and a running job keeps the old one. The old key works until you delete it on the server.
- **One runner user.** All of ghr's runners run as the `ghrunner` user, so a job can read other runners' instance directories and their processes' environment (`/proc/<pid>/environ`). Any job on the host, including a non-fork job of a served public repository under `allow_public`, can read the job key while any runner of a switched-on repository exists, from its start to its cleanup, idle time included. This is part of the trust domain the [README](../README.md#security) describes.
- **Eligibility changes.** Switching a repository off, the repository turning public, removing it from `repos`, replacing the job key, and changing or removing the store's connection reach runners this way: runners started afterwards get the new state; at its next poll ghr deletes or rewrites the settings file on the repository's starting and idle runners, warm runners and runners ghr took over after a restart included; a runner already running a job keeps the file its job started with. Turning a repository on reaches only runners started afterwards. Artifacts already stored are unaffected and go by cleanup as usual.
- **Visibility lag.** ghr re-reads whether a repository is public at most every 6 hours, or sooner when a listing of its runs shows it public. A repository made public can still get the settings file on new runners for up to that long. Switch a repository off before you make it public.
- **`http://` endpoints.** Presigned links and the Artifacts page's **Download** send the browser to the endpoint itself, so they work only from the store's network; the Copy link menu and the Download button say "Works only on the store's network". When ghr's web UI is served over https, such as through a TLS proxy, a browser may block the download as mixed content (not verified). Artifacts cross the network unencrypted; requests are signed, so the key's secret itself is not sent.
- **Container jobs as another user.** A container job whose user is not root cannot read the settings file, and may not be able to write the step's outputs, so ghr's actions print a warning from the [Messages](#messages) table and run GitHub's actions instead. Whether GitHub's actions succeed for that user is GitHub's behaviour, not ghr's: they also write step outputs, which may fail the same way (not verified). An upload that reaches GitHub's storage is still found by a later ghr job's download through the GitHub fallback.
- **Duplicate names.** An upload refuses a name the same run attempt already uploaded unless `overwrite: true` is set. Two uploads of one name running at the same moment both pass that check, and the last to finish wins.
- **Re-run attempts.** A run keeps one object per name: a re-run attempt that uploads the same name replaces the earlier attempt's artifact, so every attempt's run page shows the latest upload.
- **Patterns across stores.** The download falls back to GitHub only when the bucket matched nothing, so a `pattern` that matches artifacts in both stores downloads only the bucket's.
- **Shorter keeping.** Without `retention-days`, artifacts go after 7 days, where GitHub keeps them 90 days by default. Check workflows that read older artifacts, such as `workflow_run` workflows, downloads by run ID and nightly jobs reading the last build, and give them `retention-days` or a longer cleanup age.
- **Fork pull requests.** ghr refuses jobs whose event comes from a fork, so a fork's pull request never runs on ghr's runners; on other runners its artifacts go to GitHub. Never enable "Run workflows from fork pull requests" on a repository ghr serves.
- **Other repositories.** The download action cannot fetch another repository's artifacts.
- **Download size.** The download action refuses an artifact with more than 1,000,000 files or more than 64 GiB of files unzipped, and will not write through a symbolic link in the download path that points outside it.
- **Moving the store.** Changing the endpoint or bucket under Settings makes ghr rewrite the settings file of starting and idle runners at its next poll. Artifacts on the old server stay there: the Artifacts page and cleanup see only the new bucket, and a download of an older artifact finds nothing in it and falls back to GitHub. After an upgrade from an earlier release, let ghr remove the old repository variables first, as the [upgrade note](#upgrading-from-an-earlier-release) says.
- **Removing a repository by hand.** Remove repositories through ghr, not by editing `config.yaml`. Removing one through ghr drops its `artifact_store.repos` entry and, as far as the token allows, ghr's old variable and secret from earlier releases. A repository taken out of `repos` by hand keeps its entry, which shows as not served and counts as off, and switches it on again if you add the repository back.
- **Downgrades.** A ghr release from before this feature refuses a config with an `artifact_store` block; remove the block before downgrading.

## Upgrading from an earlier release

Earlier releases handed the store to jobs through a `GHR_ARTIFACT_STORE` repository variable and a `GHR_ARTIFACT_KEY` repository secret. ghr now writes neither, and after it starts it removes its own from each served and watched repository once, as far as the token allows.

- **Move the action references.** Workflows pinned to an older release of the actions keep reading `GHR_ARTIFACT_STORE`; once ghr deletes it, they use GitHub's storage without a message. Move `darkraise/ghr/actions/upload` and `/download` to the new release when you upgrade ghr.
- **New actions on an old ghr.** The new actions find no settings file on an older ghr's runners, so they also use GitHub's storage.
- **Remove the old lines.** Delete `key: ${{ secrets.GHR_ARTIFACT_KEY }}` from the upload and download steps and the `GHR_ARTIFACT_STORE: ${{ vars.GHR_ARTIFACT_STORE }}` line from `env`. The actions ignore both.
- **Let ghr retire the variables, then change the endpoint.** ghr recognises its old variable by the endpoint and bucket it names. Change either only after Events shows that ghr removed each repository's old variable; after the change the old variables no longer match, and ghr leaves them behind.
- **Leftovers.** ghr leaves a variable that names another endpoint or bucket, and a `GHR_ARTIFACT_KEY` secret without its variable. Without the Variables permission it cannot see the variable and deletes nothing, without a warning; when a deletion fails it adds a warning to Events and tries again on later passes. Remove what is left with:

  ```bash
  gh variable delete GHR_ARTIFACT_STORE -R <owner>/<repo>
  gh secret delete GHR_ARTIFACT_KEY -R <owner>/<repo>
  ```

## Messages

| Message | What to do |
|---|---|
| `ghr's artifact store settings may exist but this job's user cannot read them, so this job uses GitHub's artifact storage; run the job as root in its container or on the host` | A container job runs as a user other than root. That user cannot look into the `_ghr` folder, so the warning also appears after ghr removed the file. Drop `options: --user` or the image's `USER` for that job, or run it on the host. |
| `this job's user cannot write the step's outputs to GITHUB_OUTPUT, so this job uses GitHub's artifact storage; run the job as root in its container or on the host` | A container job runs as a user other than root, which cannot write the runner's output file. Drop `options: --user` or the image's `USER` for that job, or run it on the host; GitHub's own actions may fail for that user too. |
| `no settings file: pass --settings or set GHR_ARTIFACT_SETTINGS; ghr's upload and download actions pass the file ghr wrote for its runner` | `ghr-artifact` was run without the actions. Use ghr's upload and download actions, which look for the file first. |
| `ghr's artifact store settings <path> do not exist; this job did not run on a ghr runner switched on for self-hosted storage` | The file was not there when `ghr-artifact` ran. Use ghr's actions, which use GitHub's storage when there is no file. |
| `cannot read ghr's artifact store settings <path>: <error>` | The job's user cannot read the file; run the job as root in its container or on the host. |
| `ghr's artifact store settings <path>: the settings are not valid JSON` | Something in the job changed the file. Run the job again on a fresh runner. |
| `ghr's artifact store settings <path>: settings version <v> is not supported; use a newer ghr-artifact` | ghr is newer than the release the actions reference. Move the action references to the release you run. |
| `ghr's artifact store settings <path>: the endpoint is refused: <reason>` | The file's endpoint breaks [the endpoint rule](#the-endpoint). Set an accepted endpoint under Settings → Self-hosted storage, and reference a release of the actions at least as new as ghr. |
| `ghr's artifact store settings <path>: the settings name no bucket` (or `no region`, `no key_id`, `no secret`) | Something in the job changed the file. Run the job again on a fresh runner. |
| `the endpoint must be https://host[:port], or http://ip[:port] for a private, loopback or link-local IP address` | The endpoint is not an `https://` or `http://` address with a host. Enter it as [the endpoint rule](#the-endpoint) shows. |
| `the endpoint must be only https://host[:port] or http://ip[:port], without a path, query or user info` | Remove the path (a trailing `/` included), query, fragment or user name from the endpoint. |
| `an IPv6 zone is not supported` | Use the server's address without a `%25` zone, such as a private `fd00::/8` address. |
| `use https:// for a host name; http:// is accepted only for a private, loopback or link-local IP address` | Use `https://` with the name, or `http://` with the server's private IP address. |
| `http:// is accepted only for a private, loopback or link-local IP address; use https:// for this address` | The address is public, carrier-grade NAT (`100.64.0.0/10`), `0.0.0.0` or `::`. Use `https://`, or the server's private address. |
| `<name> is not served by ghr; self-hosted storage serves only repositories ghr runs jobs for` | Only repositories listed under Repositories can be switched on. Add the repository there first, or leave its artifacts on GitHub. |
| `spawn <id>: cannot write the artifact store settings: <error>; its job uses GitHub's storage` | ghr could not write the file into a new runner, so that runner's job used GitHub. Check the error, such as a full disk. |
| `cannot remove the artifact store settings from <runner>: <error>; its next job may still use self-hosted storage` | ghr could not delete an idle runner's file after the repository stopped using the store. Stop that runner with `ghr kill <id>`. |
| `cannot update the artifact store settings of <runner>: <error>` | That runner's next job uses the previous settings; the old key works until you delete it on the server. Stop the runner with `ghr kill <id>` to give its job the new settings. |
| `artifact store: cannot remove ghr's old GHR_ARTIFACT_STORE variable or GHR_ARTIFACT_KEY secret from <repo>: <error>; delete them with gh variable delete GHR_ARTIFACT_STORE -R <owner>/<repo> and gh secret delete GHR_ARTIFACT_KEY -R <owner>/<repo>` | Run the two commands, or give the token Variables and Secrets: read and write on the repository; ghr tries again on later passes. |
| `an artifact named <name> was already uploaded by this run attempt; set overwrite: true to replace it` | Give the artifact another name, or set `overwrite: true`. |
| `<name>.zip is damaged: its digest does not match; upload it again` | The object changed after its upload; run the uploading job again. |
