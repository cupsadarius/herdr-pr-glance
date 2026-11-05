# Split into core + wrappers, and a merge-readiness Overview

Date: 2026-11-05
Status: approved

## Intent

Glance PR is one Go module today: a Bubble Tea TUI fused with the Herdr plugin
that hosts it. The owner wants to run the same pane natively in Rex, and the
current Overview shows too little at a glance (in an all-green PR it is three
lines in a tall, empty pane).

Two changes:

1. **Split** the code into three repositories (siblings under `~/Personal/`):
   - `pr-glance`: the TUI and everything it needs. Knows nothing about any
     multiplexer.
   - `herdr-glance-pr` (this repo, module `github.com/cupsadarius/herdr-pr-glance`):
     the Herdr plugin. A thin wrapper over `pr-glance`.
   - `rex-pr-glance`: a new Rex plugin. A thin wrapper over `pr-glance`.
2. **Redesign the Overview** around one question: "can I merge, and if not,
   what blocks me?", with a recent-activity feed underneath.

## Goals

- One TUI, written once, in `pr-glance`. Each wrapper only says which directory
  to show, when that changes, how to zoom, and how to open the pane.
- Herdr users who installed `cupsadarius/herdr-pr-glance` keep working: same
  plugin id, same actions, same install command.
- Rex users get a pane that follows the block they work in, like the Herdr
  pane follows the working pane.
- Overview answers "can I merge?" in its first lines, names each blocker, and
  shows what happened recently, without a tab switch.

## Non-goals

- No Rex block plugin (`rex server run --block-dir`). Its protocol is private
  and undocumented; the wrapper uses only `init.lua` and the `rex` CLI.
- No write actions against GitHub. Glance stays read-only.
- No wrappers for tmux, zellij or others now. `pr-glance --cwd` covers them.
- No change to the Comments and Reviews tabs beyond what the split forces.
- No custom colour palette: the 16 ANSI colours and standard attributes only.

## Anti-goals

- A wrapper that copies TUI, GitHub or cache logic. If a wrapper needs it,
  it belongs in `pr-glance`.
- A broken Herdr install or a renamed Herdr plugin id.
- Invented numbers. If GitHub does not report a value (for example the count of
  required approvals), the pane does not show a guess.

## Part 1: the split

### `pr-glance` (core)

Module `github.com/cupsadarius/pr-glance`.

- **Public API**, the only packages a wrapper imports:
  - `model`: today's `internal/model` types, unchanged in meaning
    (`model.Source` is what a wrapper produces).
  - `glance`: one entry point, roughly
    `glance.Run(ctx, glance.Options) error`, where `Options` carries:
    - `Resolver`: the existing `ui.SourceResolver` interface
      (`Resolve(ctx) (model.Source, error)`). The TUI already polls it; keep
      that contract.
    - `Zoom func() error`: nil hides the `z` key.
    - `StateDir string`: where the disk cache lives.
    - anything else today's `cmd/herdr-pr-glance/main.go` passes into
      `ui.New` that is not Herdr-specific (browser opener, runner).
- **Internal**: `github`, `cache`, `command`, `ui` move under `internal/` of the
  new module. Wrappers cannot reach them.
- **Binary** `pr-glance`, for use with no multiplexer:
  - `pr-glance [--cwd PATH]`: today's `view --cwd`. Default `PATH` is the
    current directory. Cache in `$XDG_STATE_HOME/pr-glance`
    (default `~/.local/state/pr-glance`).
  - `pr-glance snapshot --cwd PATH`: today's `snapshot`, unchanged output.
- **History**: start `pr-glance` from this repo's full history (push `main`
  there), then delete the Herdr parts. `git blame` keeps working. *(my call)*
- **Release**: own goreleaser config and tags, starting at `v0.2.0`. *(my call)*

### `herdr-glance-pr` (Herdr wrapper)

- Keeps: `internal/herdr` (pane tracking, launcher, recorded panes),
  `herdr-plugin.toml`, `scripts/install.sh`, release workflow, the
  `open` / `overlay` / `view` subcommands, and the Herdr-specific tests and
  testdata.
- Drops: everything that moved to `pr-glance`. Imports it at a tagged version.
- Its `view --cwd` subcommand goes away: `pr-glance --cwd` replaces it. The
  README points Rex users to `rex-pr-glance`.
- Plugin id `glance.pr`, binary name `herdr-pr-glance`, install command: all
  unchanged.

### `rex-pr-glance` (Rex wrapper)

Shape follows community Rex plugins (for example `haxybaxy/rex-worktrunk`):
a Lua module cloned into `~/.config/rex/rex-pr-glance`, loaded from
`init.lua`:

```lua
require("rex-pr-glance").setup{}
rex.bind("ctrl+space>p", "pr_glance")
```

- **`init.lua`**: `setup{}` defines a `pr_glance` action. It opens
  `bin/rex-pr-glance` in a split to the right of the focused block, or focuses
  or closes the pane if one is already open in that window. The action also
  shows in the command palette. Split side and ratio are `setup{}` options
  with defaults (right, 35%). *(my call)*
- **Binary `rex-pr-glance`**: Go, imports `pr-glance/glance`, supplies:
  - **Resolver**: the "working block" is the focused terminal block in the
    pane's own window, never the pane itself; when focus moves to the pane,
    keep the previous working block. Directory comes from the block's `pwd`
    method (`rex block call pwd --block ID`). Changes come from
    `rex events -s SESSION --json`: `pwd_changed` on the working block, and
    `session_view_changed` / `active_window_changed` to re-check focus. Then
    `git` resolves root, branch and HEAD exactly as the Herdr resolver does.
  - **Visible** (`model.Source.Visible`, which gates GitHub fetches): true
    when the pane's window is the session's active window and at least one
    client is attached to the session. Both come from the same session event
    stream (`active_window_changed`, `client_connected`,
    `client_disconnected`). *(my call)*
  - **Zoom**: `rex zoom` on `REX_BLOCK`.
- **Install**: `scripts/install.sh` downloads the release binary for the tag in
  `VERSION` and checks its SHA-256, the same way the Herdr hook does (copy the
  script). Rex has no build hook, so the README says to run it once after
  `git clone`. *(my call)*
- Works on a remote Rex server too, as long as the plugin sits at the same
  path under `$HOME` there (Rex runs key-bound actions on the host of the
  float). Document; do not engineer for it.

### Order of work *(my call)*

1. Create `pr-glance` from this repo; move code; add `glance.Run`; all current
   tests pass there with no behaviour change. Tag `v0.2.0`.
2. Make this repo a wrapper over `pr-glance v0.2.0`. Herdr behaviour and tests
   unchanged. Release.
3. Build `rex-pr-glance` against `pr-glance v0.2.0` (current Overview).
4. Redesign the Overview in `pr-glance` (Part 2). Tag `v0.3.0`. Bump both
   wrappers.

Steps 1–2 change structure only; step 4 changes behaviour only.

## Part 2: merge-readiness Overview

Prototype: `spike/variants/main.go` in this repo, `-v combo`
(`go run ./spike/variants -v combo`). It is throwaway: it shows layout and
tone, not code to keep. Commit it before step 1 so `pr-glance` inherits it;
delete it from this repo in step 2 and from `pr-glance` in step 4. *(my call)*

### Layout (Overview tab, mixed-state example, ~70 columns)

```
 OPEN  #4317  optura-ai/intent                          14s ago
perf(agents): read portfolio tool use cases with lean pipeline queries
@darius-optura · 11 commits · 15 files · +2118 -605
perf/lean-portfolio → main

Overview  Comments  Reviews

Blocked
━━━━━━━━━━━━ ━━━━━━━━━━━━ ━━━━━━━━━━━━ ━━━━━━━━━━━━ ━━━━━━━━━━━━

✗ Checks         2 failing · 1 running · 27 passed
  └ e2e / portfolio
  └ lint
● Approvals      1 approval · changes requested
● Conversations  3 unresolved of 4
● Branch         3 commits behind main
✓ Mergeable      no conflicts

Reviewers ────────────────────────────────────────────────
@ana-m     ✓ approved           2h
@radu      ✗ changes requested  40m
@ioana     ● requested

Threads  3 open ──────────────────────────────────────────
├──agents/portfolio/query.go
│   ├──L88  @radu this N+1 comes back when the filter is empty
│   ╰──L141 @radu can we keep the old index hint?
╰──db/migrations/0142_lean.sql
    ╰──L4   @ana-m needs a down migration

Stack 2/3 ────────────────────────────────────────────────
main
├──✓ #4301 refactor(agents): split portfolio tools
├──● #4317 perf(agents): lean pipeline queries  ← here
╰──● #4320 feat(agents): portfolio use-case cache

Activity ─────────────────────────────────────────────────
✗ 6m  e2e / portfolio failed
│
↑ 12m @darius-optura pushed 2 commits
│
● 38m @radu commented on query.go:141
│
✗ 40m @radu requested changes
│
✓ 2h  @ana-m approved

r refresh  o browser  z zoom  ? help  q close
```

### Behaviour

- **Header** (state, number, repo, age, title, author, stats) and **footer**
  stay pinned; the tab bar stays as today. Only the body scrolls. Add
  `head → base` under the stats line.
- **Verdict: GitHub decides.** The verdict comes from GitHub's
  `mergeStateStatus`, so the pane never disagrees with the merge button:
  `CLEAN` (and `HAS_HOOKS`) → `Ready to merge`; `UNSTABLE` → `Ready to merge`
  with a yellow note that non-required checks fail; `BLOCKED`, `BEHIND`,
  `DIRTY` → `Blocked`; `UNKNOWN` (GitHub still computing) → faint
  `Checking mergeability…`. Merged, closed and draft PRs replace the verdict
  with `Merged`, `Closed` or `Draft` and keep the rest. *(owner's decision)*
- **Bar**: one segment per condition row, in the row's colour.
- **Row colours explain the verdict**: red only when that row blocks the merge
  by GitHub's own signals (failing required checks, `CHANGES_REQUESTED` /
  `REVIEW_REQUIRED`, `BEHIND`, `DIRTY`); yellow when it is worth a look but
  does not block (running checks, unresolved threads the repo does not require
  resolved, behind base when the repo does not require an up-to-date branch,
  with "(not required)"); green when fine; faint when unknown. If GitHub says
  `BLOCKED` and no row explains it (for example a rule outside these five),
  add a red line under the verdict: `blocked by a repository rule`.
- **Merge conditions**, in this order:
  - **Checks**: counts from today's `CheckCounts`; failing checks listed
    under it, each one a cursor/click target that opens its URL (today's check
    rows keep their interaction). Passing checks collapse, as today.
  - **Approvals**: approval count and GitHub's `reviewDecision`. Show
    "N of M required" only if GitHub reports M to this user; otherwise no M.
  - **Conversations**: unresolved review threads of total.
  - **Branch**: commits behind base, or "up to date with BASE".
  - **Mergeable**: GitHub's mergeable state (conflicts / clean / unknown).
  - A condition GitHub does not report shows faint `○ … unknown`.
  - Which rows block when GitHub says `BLOCKED` is the builder's inference
    from the signals above (and protection or ruleset data if readable without
    admin rights). It must never paint a row red that GitHub does not require.
- **Reviewers**: one row per requested reviewer or team and per reviewer who
  left a review; latest state per person, with age.
- **Threads**: unresolved threads only, grouped by file in a tree, first
  comment truncated to one line. Each row opens the thread URL. Hidden when
  zero.
- **Stack**: today's stack data and navigation (Enter, click, `]`, `[`, `\`),
  rendered as a tree with the current PR marked. Hidden when not stacked.
- **Activity**: newest first, last ~10 events: pushes, reviews, comments,
  review requests, check failures. Each row opens its URL where one exists.
- Empty sections disappear. An all-green PR reads "Ready to merge" plus five
  green lines, reviewers, and activity: it no longer looks empty.
- Width: content wraps or truncates to the pane width; the layout must hold at
  44 columns (today's README mockup width).

### New GitHub data

All new fields join an existing GraphQL request where possible, so one refresh
stays one or two requests:

- review requests (users and teams) and latest review per reviewer
- `mergeable` / `mergeStateStatus` (drives the verdict)
- commits behind base (for example `baseRef.compare(headRef){behindBy}`)
- required approvals, only if readable without admin rights
- timeline items for Activity
- unresolved thread count and first comment per unresolved thread, for
  Overview (today these come only with the Reviews tab's discussion fetch)

The builder chooses query shape, pagination limits and how Activity merges
check-run failures (which are not timeline items) with timeline events.

## Left to the builder

- Where the git checkout logic (root, branch, HEAD; today in
  `internal/herdr/source.go`) lives so both wrappers and the `pr-glance`
  binary share it. It must live in `pr-glance`, once.

- Exact names and signatures inside `glance.Options`.
- Whether Overview reuses the existing scroll/cursor model (`bodyItem`,
  `rowTarget`) or moves to `bubbles/viewport`; the cursor/click behaviour
  above must survive either way.
- Test layout per repo; each repo gets CI that runs `go test ./...`.
- Wording of empty, loading and error states in new sections, in the tone of
  the existing ones.
