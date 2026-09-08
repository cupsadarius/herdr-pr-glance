# Releasing herdr-pr-glance

Releases are prepared by automation and published only on an explicit instruction.
Nothing here runs by itself: the release workflow triggers on a pushed `v*` tag.

## Version sources

Three places carry the version and must agree:

- `VERSION` — read by `scripts/install.sh` at plugin build time.
- `herdr-plugin.toml` `version = "..."` — the manifest Herdr reads.
- the git tag — always `v` + the file contents (`0.1.0` -> `v0.1.0`).

`.github/workflows/release.yml` refuses to publish when the three disagree, and
also requires a completed successful CI run for the tagged commit.

## Ordering: assets first, manifest second

The installer downloads the exact release named by the checked-out `VERSION`.
That makes the ordering strict:

1. On a release branch, bump `VERSION` and `herdr-plugin.toml` to the new version
   and merge that commit. CI must be green for it.
2. Tag that commit `vX.Y.Z` and push the tag. GoReleaser builds the four archives
   (`darwin`/`linux` x `amd64`/`arm64`) plus `checksums.txt` and publishes them.
3. Only after the assets exist, advance the default branch so that
   `herdr plugin install cupsadarius/herdr-pr-glance` resolves a manifest whose
   version has matching published artifacts.

Advancing the default branch before the assets are uploaded leaves every fresh
install failing at the build hook, because `gh release download vX.Y.Z` finds no
matching archive. The installer never falls back to `latest`, by design.

A pinned repository tag always installs that tag's exact artifact: the `VERSION`
in the checkout selects the release, so there is no implicit upgrade and no
self-updater.

## Install prerequisites

The build hook needs Herdr, git, an authenticated `gh`, a POSIX shell, `tar`,
`gzip` (GNU tar execs it for `-z`; bsdtar does not, but the check is uniform)
and one of `sha256sum` or `shasum`.

## Local dry run

```sh
goreleaser check
goreleaser release --snapshot --clean
```

The snapshot writes `dist/` (git-ignored) with the four `.tar.gz` archives, each
containing exactly one executable `herdr-pr-glance` at the archive root, and
`checksums.txt`. `dist/<target>/herdr-pr-glance version` prints the built-in
version stamped through `-ldflags -X main.version=...`.

`internal/installtest` runs `scripts/install.sh` against fake `gh`, `uname` and
checksum tools on a restricted PATH, so `go test ./...` covers the installer.
