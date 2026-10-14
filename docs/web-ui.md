# Web UI

ghr serves a browser UI with every page and action the `ghr` command offers. The example config turns it on at `0.0.0.0:8080`; `GHR_WEB_LISTEN=host:port` makes `setup.sh` write another address on a first install.

| Page | What it shows |
|---|---|
| Dashboard | runner capacity, API budget, host load, an activity timeline, repositories, disk use and recent events |
| Runners | every runner with its state and diagnostic log |
| Actions | workflow runs of the served and watched repositories, with each run's job graph, annotations and searchable step logs |
| History | finished jobs with their durations and conclusions |
| Artifacts | the month's GitHub storage quota and each repository's artifacts, caches, retention and cleanup; see [Artifact storage](artifact-storage.md) |
| Repositories | add and remove repositories, and set each one's caps, labels and pause |
| Settings | scheduling, runner limits, disk, history and web access; the GitHub token, the storage policy, the runner software and the account; toolchains, package caches and Docker's disk use |

The UI updates live over one long-lived request and falls back to polling when it cannot hold it open.

## Password

Until a password is set, the UI asks for a one-time setup code along with the new password. `setup.sh` prints the code when it finishes, and `ghr code` prints it again on the host; a restart of ghr or `ghr web reset-password` replaces it. Setting the password during install (`GHR_WEB_PASSWORD` for scripts) makes the code unnecessary. The first-run wizard at `/setup` stays reachable until you finish it.

- `ghr status` warns while the UI has no password.
- `ghr web set-password` (12 to 1024 characters) sets it over SSH.
- `ghr web reset-password` deletes the password, logs every browser out and prints a new setup code; set a new password with `ghr web set-password`, or in the browser with that code.
- Five wrong passwords, at login or when changing it, from one address within 15 minutes lock that address out for 15 minutes.
- Changing the password logs out every other browser and session.
- A session ends after 24 hours without use, and after 30 days in any case. A tab kept in view keeps its session in use.
- Sessions live in memory: every restart, including each `setup.sh` upgrade, logs you out.

## Listen address and hostnames

Settings → Web access changes the listen address and hostnames, and the **Restart ghr** banner applies them. A reload does not start or move the listener; it only warns `web settings changed; restart ghr to apply`. The UI refuses an address the browser could not reach it on. A restart waits while ghr installs, removes or clears toolchains and caches, has such operations queued, prunes, or updates the runner software; running jobs keep running. If the address fails at start, ghr serves the UI on the last address a browser used and shows why.

A config written before the web UI existed has no `web:` block. Add one to `/etc/ghr/config.yaml`, set `listen`, and run `systemctl restart ghr`:

```yaml
web:
  listen: 0.0.0.0:8080
  hosts: [ghr.lan]
```

ghr answers only IP addresses, `localhost` and the names in `web.hosts`, which blocks DNS rebinding.

## Behind a reverse proxy

On the network the UI is plain HTTP: the password and the session cookie cross it unencrypted. Put a TLS-terminating proxy such as nginx-proxy-manager in front, and bind `listen` to a LAN-only address rather than `0.0.0.0` if the host has more than one network.

- Add the proxy's hostname to `web.hosts` and restart ghr. The proxy must forward the original `Host` header, as nginx-proxy-manager does by default.
- Every browser reaches ghr from the proxy's address. List the proxy's address, or its network as a CIDR prefix such as `10.0.0.0/24`, in `web.trusted_proxies` and restart ghr. The lockout after five wrong passwords then counts each browser by the address the proxy reports in `X-Forwarded-For`, and the proxy is held only to the total connection limit, not the per-address one. Without it a lockout shuts out everyone behind the proxy at once.
- ghr reads `X-Forwarded-For` from the right and takes the first address that is not a trusted proxy, so a proxy may append to a header the browser sent, as nginx-proxy-manager does, or replace it. The proxy must set the header: one that passes the browser's own header on unchanged, as plain nginx does without `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`, lets the browser choose the address it is counted under. List only proxies, since a browser at a trusted address can do the same.
- Live updates use `/api/changes`. ghr marks it `X-Accel-Buffering: no`, which nginx honours, and sends a ping every 15 seconds, inside nginx's default 60-second read timeout, so nginx-proxy-manager needs no extra settings. Another proxy must pass the response through unbuffered and allow 15 seconds of silence.
