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

1. On a release branch, bump `VERSION` and `herdr-plugin.toml` to the new version.
   Push the branch so CI runs and goes green for that commit; do not merge yet.
2. Tag that same commit `vX.Y.Z` and push the tag.
3. The release workflow revalidates tag/`VERSION`/manifest agreement and the
   commit's green CI, then GoReleaser publishes the four archives
   (`darwin`/`linux` x `amd64`/`arm64`) and `checksums.txt`.
4. Verify the release page lists all five assets and that
   `sh scripts/install.sh` from a checkout of the tag installs successfully.
5. Only then merge (or fast-forward) the default branch to that commit, so
   `herdr plugin install cupsadarius/herdr-pr-glance` never resolves a manifest
   version whose artifacts do not yet exist.

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
