# Contributing to ghr

Thanks for your interest in ghr. Bug reports, fixes and improvements are welcome.

## Reporting a bug

Open an issue with the ghr version (`ghr version`), the host's distribution, what you did, what you expected and what happened. The daemon's log (`journalctl -u ghr -n 200`) usually helps; remove tokens and repository names you would rather not share.

Report security problems privately instead, as described in [SECURITY.md](SECURITY.md).

## Before a large change

For anything beyond a small fix, open an issue first to agree on the approach. ghr runs other people's code as root-equivalent on their hosts, so changes to scheduling, cleanup, the installer or the web UI's authentication get a careful review.

## Development setup

ghr is a Go daemon and command (`cmd/ghr`, `internal/`) with a React web UI (`web/`) that is embedded into the binary. You need Go 1.26 and the Node version in [`web/.nvmrc`](web/.nvmrc).

```bash
(cd web && npm ci && npm run build)    # writes web/dist, which the binary embeds
go build -ldflags "-X main.version=dev" -o ghr ./cmd/ghr
```

For work on the UI, `npm run dev` in `web/` starts Vite and proxies `/api` and `/auth` to the daemon at `GHR_DEV_URL` (default `http://localhost:8080`).

## Tests

Run all three suites before opening a pull request; CI runs the same checks.

```bash
gofmt -l .                             # must print nothing
go vet ./...
go test -race ./...
(cd web && npm run lint && npm run typecheck && npm test)
bash deploy/tests/setup_test.sh        # setup.sh, offline: no root, network or systemd
```

Add or update tests with every behaviour change, next to the code they cover.

## Pull requests

- Keep each pull request to one concern, and each commit atomic.
- Write commit messages as `<type>(<scope>): <subject>`, where type is one of `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore` or `perf`, and the subject is imperative, at most 50 characters, with no final period. Explain the what and why in the body when the change is not obvious.
- Update the documentation in `docs/` and the README when behaviour a user sees changes.

## Releases

Every push to `master` that passes CI publishes a release: the next patch version, with `ghr_linux_amd64.tar.gz`, `checksums.txt` and a `setup.sh` that installs that release, with a build provenance attestation for the tarball and `setup.sh`.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
