# Glance PR — design specification

Status: **Proposed; awaiting document review. Implementation has not started.**

## Purpose

Show the GitHub PR for the branch being worked on inside Herdr, with the useful
information from Supacode's PR sidebar: PR identity, statistics, CI, and reviews.
The user can glance at progress, then open discussions when more detail is needed.

This is an ordinary Herdr plugin: a manifest and a compiled Go executable that
renders data obtained through `gh`. It needs no separately installed application, service,
GitHub App, OAuth flow, or plugin-specific credentials.

## Proposed defaults

| Decision | Initial choice |
| --- | --- |
| Name / plugin ID | Glance PR / `glance.pr` |
| Host | Herdr 0.8.2 or newer |
| Platforms | macOS and Linux |
| Implementation | Go with Bubble Tea v2 for the terminal UI |
| Dependencies | Versioned Go modules, with checked-in `go.mod` and `go.sum` |
| Development checks | `gofmt`, `go vet`, `go test` and race detection |
| Distribution | GoReleaser binaries for macOS/Linux, amd64/arm64 |
| External commands | `git`, authenticated `gh`, running Herdr binary |
| Default presentation | Right split, approximately 44 columns where space permits |
| Alternative presentation | Temporary overlay, or zoom the existing split |
| Local context check | Every 2 seconds while visible |
| Automatic summary refresh | Every 60 seconds while visible |
| Discussion retrieval | Only on section opening or explicit refresh |
| Discussion cache freshness | 5 minutes |

Go and Bubble Tea are the agreed stack for distribution. Width, refresh interval
and cache lifetime remain proposed defaults for review.

Ship prebuilt executables so users need no language toolchain or virtual environment.
Use Bubble Tea's model/update/view architecture for input, rendering and asynchronous
commands; keep subprocesses, cache access and source tracking behind small interfaces.
Use Go's standard library for subprocesses, JSON, files and time handling. Pin the
selected stable Bubble Tea v2 release and any UI dependencies in the Go module files.

## User experience

Opening Glance creates or focuses its existing pane in the current tab, initially
showing Overview. An optional Herdr keybinding invokes the open action;
installation does not overwrite existing keybindings.

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
 ◷ integration tests                 running
 ✓ lint                               passed
 ✓ build                              passed

 r refresh  o browser  z zoom  q close
```

The mockup illustrates content and priority, not exact spacing. Content wraps to
available width and supports terminal resizing. Keyboard operation is complete;
mouse section selection, row selection, and scrolling work where available.

### Overview

- PR number, title, author, URL, and open/draft/merged/closed state.
- Repository, source and target branches, including fork identity where needed.
- Exact commit count, changed-file count, additions, and deletions.
- Review decision: approved, changes requested, review required, or unavailable.
  Reviewer bodies and inline discussions are not downloaded for this summary.
- Individual checks with aggregate counts. Failures first, then pending, then
  successful, neutral, skipped, and unknown results.
- Cancelled, timed-out, action-required and unknown states remain distinguishable.
  An empty check list means “No checks,” not “All passed.”
- `o` opens the selected check URL, falling back to the PR URL.

### Comments

Selecting Comments loads top-level PR conversation comments on demand. Show author,
timestamp, readable body, and browser link. Preserve line breaks and code-block
text; full Markdown rendering is not required. Review submissions and inline code
discussions belong under Reviews.

### Reviews

Selecting Reviews loads submitted reviews and inline review threads on demand.
Show review authors and decisions, plus expandable threads with file path, line
number when available, resolved/outdated state, and all replies. Unresolved
threads appear first; resolved threads remain accessible.

Threads are collapsed initially. Enter or clicking toggles the body and replies.
`o` opens the selected review/thread. Zoom provides room for long discussions.

### Controls

| Input | Action |
| --- | --- |
| `1`, `2`, `3` or click section | Overview, Comments, Reviews |
| Arrows / `j`, `k`; Page Up / Down; mouse wheel | Navigate and scroll |
| Enter / click thread | Expand or collapse |
| `r` | Refresh active section, bypassing cache freshness |
| `o` | Open selected item's URL, falling back to PR URL |
| `z` | Toggle pane zoom through Herdr |
| `q` / Escape | Close Glance; overlay restores previous focus |

## What “current branch” means

1. Capture the invoking working pane and its workspace/tab context.
2. Within that tab, follow the most recently focused regular working pane. Glance
   itself and other plugin-owned panes are not source candidates.
3. Keep that source while Glance has focus. Reading a review must not change the
   tracked repository to the plugin source directory.
4. Resolve the working pane's directory through Herdr, preferring its foreground
   process directory when available, then the pane/workspace directory.
5. Resolve checkout root, symbolic branch, and HEAD with local Git commands.
   Linked worktrees remain distinct checkouts with their own branch context.
6. If the source closes, use a focused regular candidate in that tab, otherwise a
   deterministic first candidate. With no candidate, show “No working pane.”

Each Glance belongs to its tab; it does not move across tabs/workspaces. Suspend
automatic GitHub polling while hidden, and check freshness when visible again.
Each tab can open its own Glance.

Use `gh pr view` in the resolved checkout to discover the associated PR, preserving
gh's remote, fork, host, and branch configuration. Do not match branch names across
arbitrary repositories. Display the state gh returns, including merged/closed;
if none is found, show “No PR for this branch.” Do not create one.

A context change invalidates the visible PR and discussion selection immediately.
Tag asynchronous requests with source generations: old-branch responses cannot
populate a new branch's view. Detached HEAD and non-Git directories have explicit
empty states.

## GitHub access and refresh

All GitHub data comes from `gh`, reusing its authentication and host configuration.
Call Herdr through `HERDR_BIN_PATH` when supplied.

Use `gh pr view --json …` for discovery and supported summary fields, and
`gh api graphql` for exact aggregate counts and paginated connections. Automatic
queries exclude discussion bodies. Never infer exact totals from potentially
truncated commit or check lists.

Only one request per source/section may be in flight; coalesce repeated refresh
input. Run subprocess work as asynchronous Bubble Tea commands with bounded contexts,
returning messages that carry the source generation. Only the update loop mutates
UI state. A branch change schedules a summary without waiting for the normal interval.

The common summary path should need roughly one or two gh invocations per minute
per visible pane, plus pages for unusually large check lists. This is an estimate,
not a rate-limit guarantee: gh may perform multiple requests and GitHub applies
primary and secondary limits. On a detected rate limit, display the reason and
honor the retry time when available; otherwise use a five-minute cooldown. Manual
refresh also respects a known rate-limit cooldown.

Network failures retain same-PR summary data with a stale/error indicator, never
masquerading as “No PR.” Back off automatic retries from 60 seconds to five minutes;
success restores the normal interval. Authentication errors point to `gh auth login`.

## On-demand discussion cache

Per the user's refinement: **automatic polling never fetches comments, review
bodies, threads, or replies—even while a discussion section remains open.**

| Trigger | Behavior |
| --- | --- |
| Open section with fresh cache | Render cache; no GitHub request |
| Open with expired cache | Render cache marked stale; fetch that section |
| Open without cache | Show loading; fetch that section |
| Leave section open past TTL | Keep data and fetch time visible; no automatic fetch |
| Press `r` in discussion | Fetch that section regardless of cache freshness |
| Refresh Overview | Fetch summary/CI only |
| Change PR | Use its separate cache only when its discussion section is opened |
| Fetch fails | Keep cached data if available; show error and stale status |

Store JSON beneath `HERDR_PLUGIN_STATE_DIR/cache`, with schema version, canonical
PR identity, section, fetch timestamp, and payload. Key by canonical PR URL
(including host/repository) plus section, not branch name or PR number alone.
Hash filenames so external identifiers cannot become filesystem paths.

Comments and Reviews have independent entries. Writes are atomic; only complete,
successfully paginated results replace an entry. A later-page failure must not
overwrite a complete cache with partial data. Corrupt or incompatible files behave
as cache misses. Write failures leave fetched data usable in memory with a warning.

Caches contain discussion content: create files/directories with user-private
permissions and store no tokens. Freshness is five minutes, not deletion after five
minutes. Remove entries older than seven days during subsequent cache use; no
background cleanup service runs. Show fetch timestamps in the interface.

Paginate comments, submitted reviews, review threads, and each thread's replies
independently. Fetching every thread does not imply every reply was fetched.

## Implementation structure

| File | Responsibility |
| --- | --- |
| `herdr-plugin.toml` | Metadata, open/overlay actions, pane entrypoints |
| `go.mod`, `go.sum` | Go version and pinned module dependencies |
| `cmd/herdr-pr-glance/main.go` | Launch, interactive view, one-shot snapshot CLI |
| `internal/model/` | Shared source, PR, discussion, and display records |
| `internal/command/` | Argv execution, timeouts, typed errors |
| `internal/herdr/` | Context, source selection, pane launch/zoom |
| `internal/github/` | PR discovery, normalization, GraphQL pagination |
| `internal/cache/` | Atomic persistence, cache entries and cleanup |
| `internal/ui/` | Bubble Tea model/update/view, refresh policy and rendering |
| Package-local `*_test.go` / `testdata/` | Behavior tests and synthetic fixtures |
| `scripts/install.sh`, `VERSION` | Install the exact release binary for this manifest |
| `.goreleaser.yaml`, `.github/workflows/` | Build/test automation and release artifacts |

Commands receive argv arrays; repository content is never interpolated into a shell.
Strip terminal control sequences from remote text and validate browser links as
HTTP(S). Keep credentials and fetched private discussions out of tracked fixtures.

## Distribution and updates

The public install path is intended to be:

```sh
herdr plugin install cupsadarius/herdr-pr-glance
```

Herdr clones the plugin repository and runs its declared build hook. Here that
hook runs `sh scripts/install.sh` to download a prebuilt executable; users do not
compile Go. The script maps the machine to one of four targets: darwin/amd64,
darwin/arm64, linux/amd64, linux/arm64. Unsupported targets receive a clear error.

Keep `VERSION` and the manifest version synchronized. Download the exact `vVERSION`
release from `cupsadarius/herdr-pr-glance` using `gh release download`, including
the matching `.tar.gz` and checksum file. Verify its SHA-256 with `sha256sum` or
`shasum -a 256`, extract only the expected executable, and atomically install it
as `bin/herdr-pr-glance` under the plugin root. Manifest commands invoke that binary
directly. A failure leaves any previous executable intact and fails installation.
The hook never edits the manifest, falls back to `latest`, or downloads on pane launch.

Install prerequisites are Herdr, git, authenticated gh, a POSIX shell, tar and one
of the checksum tools. Runtime uses the installed executable, git, gh and Herdr.
First installation requires network access. All regular GitHub queries still go
through gh; the binary performs no separate token management.

GoReleaser produces all four artifacts and checksums with `CGO_ENABLED=0`; verify
that selected dependencies support these builds. Record the required Go version
in `go.mod` and use that version consistently in CI. Test on native macOS/Linux
where runners permit; cross-compilation alone is not a runtime test.

Source contributors install Go, build `bin/herdr-pr-glance` locally, then run
`herdr plugin link .`. Herdr's link command does not run build hooks. Ignore local
`bin/` and release `dist/`; durable discussion caches stay in Herdr's state directory.

Publish matching artifacts before advancing the default branch's install manifest
to that version. Reinstalling with Herdr's install command updates a managed checkout;
pinning a repository tag should select that tag's exact binary version. There is no
separate self-updater. A locally linked checkout follows the local build workflow.
Release automation is prepared and tested as part of implementation; publication
requires a separate instruction and is not part of the current documentation work.

## Scope boundary

This version reads PR data and opens links. Merging, closing, approving, replying,
resolving threads, requesting reviews, rerunning CI, and full diffs are outside
scope. There is no native Herdr sidebar extension or cross-workspace dashboard.

## Acceptance criteria

- Show the current working branch's PR inside a real Herdr 0.8.2 pane.
- Keep statistics exact for fixtures exceeding a single API page.
- Render CI states without misleading success indicators.
- Overview polling performs no discussion query.
- Opening fresh discussion cache makes no request; manual refresh fetches it,
  subject to known rate-limit cooldowns.
- Discussions spanning multiple thread and reply pages are fully available.
- Branch switches cannot display obsolete asynchronous results.
- Focusing Glance never makes its source checkout the tracked repository.
- Failures retain same-PR data with visible staleness.
- Narrow terminals, Unicode, long paths and control characters do not break the UI.
- Local Go build/link/open works with the installed Herdr version.
- All four release targets build without cgo; supported native runners smoke-test them.
- Installation selects the pinned platform artifact and checks its checksum before
  replacement; wrong checksums, missing releases and unsupported targets fail clearly.
- A release install launches without a language toolchain and never downloads on open.
- Formatting, `go vet ./...`, `go test ./...` and host-supported race checks pass.

## Evidence and integration checks

Installed Herdr reports `0.8.2`; its pane-open help exposes split, overlay, tab,
zoomed and right/down split directions. Go and gh are available locally.
Live plugin integration, GitHub fetching and the release installer are not yet tested.

The website describes some features beyond those exposed by the installed CLI.
Verify exact context shapes and supported arguments against that binary before
implementation. Native non-terminal plugin UI is not part of plugin v1.

- [Herdr plugins](https://herdr.dev/docs/plugins/)
- [Herdr 0.8.2 plugin reference](https://herdr.dev/docs/0.8.2/plugins/)
- [Herdr 0.8.2 socket API](https://herdr.dev/docs/0.8.2/socket-api/)
- [gh pr view](https://cli.github.com/manual/gh_pr_view)
- [gh api and pagination](https://cli.github.com/manual/gh_api)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- [GoReleaser Go builds](https://goreleaser.com/customization/builds/builders/go/)
