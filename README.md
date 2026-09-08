# Glance PR

Glance PR is a [Herdr](https://herdr.dev) plugin that shows the GitHub pull
request for the branch you are working on, in a pane next to your terminal. It
tracks the working pane in the current tab, resolves its checkout and branch
with git, and reads PR identity, statistics, CI checks, review decision,
comments and review threads through your authenticated `gh`. It is read-only
against GitHub: it displays data and opens links.

```text
 GLANCE PR                  refreshed 12s ago
 acme/service · feature/retry

 #3630  OPEN
 Re-request denied approvals
 @author       feature/retry → main

 12 commits  ·  8 files
 +284  -76
 Review: Changes requested

 [Overview]  Comments  Reviews

 CHECKS      1 failed · 1 pending · 2 passed
 × unit tests                         failed
 ◷ integration tests                 pending
 ✓ lint                               passed
 ✓ build                              passed

 r refresh  o browser  z zoom  q close
```

The mockup shows content and priority, not exact spacing. Content wraps to the
pane width and follows terminal resizing.

## Prerequisites

Runtime:

- Herdr 0.8.2 or newer
- `git`
- `gh`, authenticated (`gh auth login`)

Installation needs all of the above — the hook downloads release assets with
`gh`, so `gh` must already be authenticated — plus a POSIX shell, `tar`, `gzip`
(GNU tar shells out to it for `-z`) and either `sha256sum` or `shasum`.
Supported platforms are macOS and Linux on amd64 and arm64.

## Install

```sh
herdr plugin install cupsadarius/herdr-pr-glance
```

Herdr clones the repository and runs the declared build hook. The hook
downloads the prebuilt binary for the exact `vVERSION` tag recorded in the
checkout's `VERSION` file, verifies its SHA-256 against the release checksum
file, and installs it as `bin/herdr-pr-glance` under the plugin root. No Go
toolchain is required. The first install needs network access; opening the pane
never downloads anything. A failed download or checksum mismatch leaves any
previous binary in place and fails the install. Herdr previews the source and
the build command and asks for confirmation unless you pass `-y`.

No release has been published yet, so this is the intended install path rather
than one that has been run end to end.

### Reinstall or pin a version

```sh
herdr plugin install cupsadarius/herdr-pr-glance --ref vX.Y.Z
```

That installs the exact binary published for that tag. There is no self-updater;
run the install command again to move to a newer version.

### Build from source

```sh
go build -o bin/herdr-pr-glance ./cmd/herdr-pr-glance
herdr plugin link .
```

`herdr plugin link` does not run the build hook, so build first and rebuild
after every change.

## Optional keybinding

The plugin never edits your Herdr configuration. Add a binding yourself if you
want one:

```toml
[[keys.command]]
key = "prefix+g"
type = "plugin_action"
command = "glance.pr.open"
```

`glance.pr.open` opens or focuses the Glance pane in the current tab.
`glance.pr.overlay` shows it as a temporary zoomed overlay instead of a split;
Herdr's overlay placement restores the previous focus and zoom when the overlay
closes.

## Controls

| Input | Action |
| --- | --- |
| `1`, `2`, `3` or click a section | Overview, Comments, Reviews |
| Arrows / `j`, `k`; Page Up / Down; mouse wheel | Navigate and scroll |
| Enter / click a thread | Expand or collapse |
| `r` | Refresh the active section, ignoring cache freshness |
| `o` | Open the selected item's URL, falling back to the PR URL |
| `z` | Toggle pane zoom through Herdr |
| `q`, Escape or `ctrl+c` | Close Glance |

Only the up and down arrows are bound; left and right do nothing.

### Sections

- **Overview** — PR number, title, author, state, branches, exact counts of
  commits, changed files, additions and deletions, review decision, and CI checks with
  aggregate counts. An empty check list means "No checks", not "all passed".
- **Comments** — top-level PR conversation comments, loaded on demand.
- **Reviews** — submitted reviews and inline review threads, loaded on demand.
  Unresolved threads come first; threads start collapsed.

### Refresh behaviour

- The working pane, checkout and branch are re-resolved locally every 2 seconds
  while the pane is visible, so pane and branch switches show up quickly.
- The Overview summary refreshes automatically every 60 seconds while the pane
  is visible. Polling is suspended while the tab is hidden.
- Comments and Reviews are never fetched automatically. They load when you open
  the section, or when you press `r`.
- Cached discussions are considered fresh for 5 minutes. Opening a section with
  a fresh cache makes no GitHub request; an expired cache is shown marked stale
  while a fetch runs.
- Failures keep the previous data for the same PR and mark it stale.

## Cache and state

Discussion payloads are stored as JSON under
`HERDR_PLUGIN_STATE_DIR/cache/*.json`, one entry per PR and section, with
user-private permissions (directory `0700`, files `0600`). Filenames are hashes
of the canonical PR URL and section. Entries older than 7 days are removed
during later cache use; no background cleanup runs. The cache holds discussion
text only — it stores no tokens, and all GitHub access goes through `gh`.

`HERDR_PLUGIN_STATE_DIR/panes.json` (next to `cache/`, not inside it) records which Glance pane belongs to
which tab, so reopening focuses the existing pane instead of creating another.

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| "No PR for this branch" | gh found no PR for the resolved checkout and branch. Push the branch and open a PR; Glance never creates one. |
| Authentication error | Run `gh auth login`. The pane shows the same hint. |
| "rate limited, retry in Ns" | GitHub applied a rate limit. Glance honours the retry time when GitHub supplies one, otherwise waits five minutes. Manual refresh respects the same cooldown. |
| "stale" marker | The last refresh failed or the data is past its interval. The shown data is the last good result for the same PR. |
| "No working pane" | No git checkout could be resolved in this tab. Focus a pane whose directory is inside a git checkout. |
| Install fails on the Herdr version | The manifest requires Herdr 0.8.2 or newer. Upgrade Herdr. |
| Install fails with `unsupported operating system:` or `unsupported architecture:` | Only macOS and Linux, on amd64 and arm64, have prebuilt binaries. Build from source instead. |
| Install fails with `gzip is required` or `no SHA-256 tool found` | Install `gzip`, and `sha256sum` or `shasum`. |
| Checksum mismatch | The download did not match the release checksum. Retry; if it repeats, do not use the artifact and report it. |
| Suspect the plugin, not the pane | Run `bin/herdr-pr-glance snapshot --cwd PATH` outside Herdr to print the resolved source and PR data for a checkout. |
| Nothing appears at all | Check `herdr plugin log list`. |

## Supported binary targets

`.goreleaser.yaml` builds four archives plus `checksums.txt`: darwin and linux,
amd64 and arm64.

| Target | Test suite run natively | Release binary executed natively |
| --- | --- | --- |
| darwin/arm64 | yes — CI `macos-latest`, and locally | yes — a GoReleaser snapshot binary was run locally |
| darwin/amd64 | no — cross-built only | no |
| linux/amd64 | yes — CI `ubuntu-latest` | no |
| linux/arm64 | yes — in a local aarch64 Docker container, including a CGO-free build; no CI runner | no |

`.github/workflows/ci.yml` runs a two-entry matrix, `macos-latest` (arm64) and
`ubuntu-latest` (amd64), and on each runner executes `gofmt`, `go vet`,
`go test ./...`, `go test -race ./...` and a `go mod tidy` cleanliness check.
Release archives are cross-compiled by GoReleaser with `CGO_ENABLED=0` on
`ubuntu-latest` (`.github/workflows/release.yml`), so race instrumentation is
never part of a shipped binary.

Cross-compilation is not a runtime test. No release has been published yet, so
the four archives describe what the release workflow will produce, not files
you can download today.

## Scope

Glance PR only reads from GitHub. Merging, closing, approving, replying, resolving
threads, requesting reviews, rerunning CI and full diffs are out of scope.

Stacked pull request support is deferred; see [docs/followups.md](docs/followups.md).

Maintainers: see [docs/releasing.md](docs/releasing.md) for the release process.
