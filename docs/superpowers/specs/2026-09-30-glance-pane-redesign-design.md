# Glance pane redesign

Date: 2026-09-30
Status: approved in brainstorming, pending spec review

## Goal

Make the Glance PR pane easier to read in a narrow split: put problems first,
stop 30+ passing checks from pushing everything else off screen, remove
repeated header information, and use the Charm components the project already
depends on (plus `charm.land/bubbles/v2`) instead of hand-rolled equivalents.

## Non-goals

- No new GitHub data. Everything rendered comes from `model.Snapshot` as it is
  today (no check timestamps, failure details, or comment counts).
- No change to the Comments and Reviews tab bodies, beyond the shared tab bar
  and footer.
- No `bubbles/viewport`, `list` or `table`. Scrolling, cursor and mouse
  targeting stay on the existing `bodyItem` / `rowTarget` / `clampOffset` /
  `revealCursor` model.
- The palette stays on the sixteen ANSI colours and standard attributes, so the
  pane keeps following the user's terminal theme.

## Layout (Overview tab)

```
 OPEN  #3540  optura-ai/intent         15s ago
feat(mvbc): export the business case as a
live Excel model
@darius-optura · 50 commits · 38 files · +25k -59

Overview  Comments  Reviews        (active tab bold + underlined)

CI  ✗ 2 failing
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━──────
 × ci / tests-e2e
 × Greptile Review
▸ ✓ 33 passed · 1 neutral · 22 skipped

Review  no decision yet

Stack  #3546 · 1/2 · onto main
   #3545  ●  feat(mvbc): compare archived models
 › #3540  ●  feat(mvbc): export the business case

[ ] stack  r refresh  z zoom  ? help  q close
```

### Header

- Line 1: state badge, `#number`, repository on the left; the status spans on
  the right. The status spans are today's `statusSpans()` with one change: the
  age reads `15s ago` instead of `refreshed 15s ago`. `stale`, `pinned`,
  `Loading…` and `rate limited, retry in Ns` stay in the status, unchanged, so
  a pinned PR is still flagged there.
- Line 1 overflow: the repository is truncated first (down to nothing). If
  badge + number + status still do not fit, the status moves to its own wrapped
  line(s) below, as `titleLines` does today.
- Badge: text `OPEN` / `DRAFT` / `MERGED` / `CLOSED` / `UNKNOWN` (the values
  `prState` returns), rendered with Lip Gloss `Padding(0,1)`. OPEN, MERGED and
  CLOSED use an ANSI background (green, magenta, red) with ANSI 0 foreground;
  DRAFT and UNKNOWN are faint text with no background.
- No PR (any empty state): the header is today's `titleLines()` (`GLANCE PR`
  plus status, with the shared `15s ago` age wording) followed by the empty
  message. The `GLANCE PR`
  app name is shown only there.
- Title: bold, wrapped.
- Meta line: `@author · N commits · N files · +adds -dels`. Additions and
  deletions from 10,000 are shortened to whole thousands rounded down
  (`+25k`), and from 1,000,000 to whole millions (`+1M`).
- The head branch, the base branch and the `Review:` line no longer appear in
  the header. Pinned state stays visible through the `pinned` status span.
- Header priorities (`fitHeader`): line 1 (badge, number, repo, status) is
  prio 0 and never drops, as today's title line. A wrapped status line keeps
  `prioError`. The title keeps `prioTitle`, the meta line takes `prioCounts`,
  blank lines `prioBlank`. `prioRepo`, `prioAuthor`, `prioReview` and
  `prioDiff` are removed.

### Tab bar

- `Overview  Comments  Reviews` on one row; the active tab is bold with the
  underline attribute, others faint. No separate rule row and no `[ ]`
  brackets, so the tab bar is still exactly one header line.
- The tab click targets (`rowTarget.tab`) keep working; their x spans shrink by
  the two bracket columns.

### Section order

CI → Review → Stack. (Today Stack is first.) Headings are bold `CI`, `Review`,
`Stack` instead of `CHECKS` / `STACK`.

### CI block

- Heading, by precedence:
  - failing and running: `◷ N running · ✗ N failing` (yellow, red);
  - failing only: `✗ N failing` (red);
  - running only: `◷ N running` (yellow; `◷` is the spinner frame);
  - otherwise, if any passed: `✓ N passed` (green);
  - otherwise, checks exist but all are neutral/skipped/unknown: the faint
    tally from `countsSpans` (e.g. `22 skipped`);
  - no checks: `No checks` (faint), and no bar or rows.
  "Failing" means the existing red states: failed, timed out, cancelled,
  action required.
- Bar: exactly the content width, one run per group in the order failing,
  running, passed, other (neutral+skipped+unknown). Each run's width is
  proportional to its count; any non-zero group gets at least one cell; the
  widths always add up to the content width, with rounding error and the cost
  of one-cell minimums taken from (or given to) the largest run. Glyphs:
  failing `━` red, running `━` yellow, passed `━` green, other `─` faint.
  The widths come from a pure function `barRuns(counts, width) [4]int` so they
  can be tested without parsing ANSI. Hidden when the pane width (`m.size()`)
  is below 24.
- Listed rows: failing checks, then running checks, in snapshot order within
  each group (the snapshot already sorts checks by `checkRank`), using today's `checkRow` (glyph, name, state).
- Fold row: one selectable row summarising every other check
  (`✓ 33 passed · 1 neutral · 22 skipped`), prefixed by `▸` collapsed or `▾`
  expanded. Groups with a zero count are left out. `✓ N passed` is green and
  appears only when something passed; the neutral, skipped and unknown
  counts are faint. Omitted when nothing is folded.
- Expanded: the folded checks follow the fold row as normal check rows, in
  snapshot order.

### Review line

`Review  <decision>` where the decision is `approved` (green),
`changes requested` (red), `review required` (yellow), or `no decision yet`
(faint) for an empty or unknown decision. Replaces `Review: unavailable`.

### Stack block

Shown only when the PR belongs to a stack. Only the heading changes: bold
`Stack` followed by today's faint detail with `base` renamed `onto`
(`#3546 · 1/2 · onto main`). Everything else stays as today: rows in
`stackRow` format (`› #num  ●  title  decision`), top of the stack first, the
`› ` marker on the shown entry, the `+N more` line, selection and pinning.
Because Stack now follows the CI block, `stackRow`'s "selected" test must
compare the cursor with the entry's index in the whole body, not with
`len(items)` of the stack alone.

## Components

| File | Responsibility |
|---|---|
| `internal/ui/keys.go` (new) | `keyMap` of `key.Binding`s for every existing action (`q`/`esc`/`ctrl+c`, `1-3`, `r`, `o`, `z`, `j`/`down`, `k`/`up`, `pgup/pgdown`, `enter`, `[` `]`, `\`) plus `?`. Implements `ShortHelp` / `FullHelp`. Stack bindings are disabled when there is no stack. |
| `internal/ui/input.go` | `handleKey` switches on `key.Matches` instead of raw strings; behaviour otherwise unchanged. |
| `internal/ui/style.go` | Adds `badge(state)` and the tab styles. Palette unchanged. `styleFooter` removed. |
| `internal/ui/checks.go` (new) | CI block: heading, bar, listed rows, fold row, expanded rows. Moves check rendering out of `view.go`. |
| `internal/ui/view.go` | Header, tab bar, section order, Review line, stack heading, full-help body. `footerLine` keeps its drop-rank fitting but takes its hints from the `ShortHelp` bindings. |
| `internal/ui/model.go` | Adds `help help.Model`, `spin spinner.Model` and `ShowHelp bool`. |
| `go.mod` | `go get charm.land/bubbles/v2` (v2.2.x). |

### Bubbles usage

- `key`: every binding, its keys and its help text.
- Footer: `ShortHelp` returns, in display order, `[ ] stack` (only with a
  stack), `r refresh`, `o browser`, `z zoom`, `? help`, `q close`.
  `footerLine`'s existing fitting stays: when the pane is too narrow, hints are
  dropped by rank, and `q close` is never dropped. Drop order (first dropped
  first): `o`, `r`, `z`, `[ ]`, `?`. `[ ] stack` is a display-only binding
  (help text only, never matched) because `[` and `]` are separate bindings
  with different actions. The drop ranks live in `keys.go` next to the
  bindings. The `help` component's own short view is
  not used, because it truncates from the right and would lose `q` first.
- `help`: `FullHelpView` renders the full help screen (`?`).
- `spinner` (`spinner.MiniDot`): replaces the static `◷` glyph on running
  checks, the CI heading when running, and the `Loading…` lead on the
  Comments/Reviews tabs.
- `progress` is not used: it draws one colour per bar, adds a percentage by
  default and animates, none of which fit a static four-group bar. The bar is
  plain Lip Gloss strings (see CI block).
- Bubbles' default `help` and `spinner` styles use hex/adaptive colours; they
  are overridden with the palette's ANSI styles (keys bold, descriptions faint,
  spinner yellow).

## Behaviour

- Fold row: selectable `bodyItem`. `enter` or a click toggles
  `Expanded["checks"]`. The key cannot collide with review-thread IDs, which
  are GitHub node IDs. Expansion survives refreshes. `o` on the fold row does
  nothing.
- Failing, running and expanded check rows keep today's behaviour: selectable,
  `o` or a click opens the check URL.
- Spinner ticks: the existing 2s `TickMsg` handler starts the spinner when the
  pane is busy (a check is pending or a discussion section is loading) and no
  spinner tick is already in flight (`spinning`). Result handlers do not
  schedule spinner ticks, so their commands stay unchanged. The spinner
  therefore starts within 2s of work appearing; its first frame shows
  immediately. Each spinner tick reschedules only while the pane is still
  busy; once nothing is pending it returns no command and clears `spinning`,
  so an idle pane gets no extra ticks. A duplicate concurrent tick is dropped
  by the spinner's own tag check.
- Full help (`?`): replaces the body only; header and footer stay. `?` or
  `esc` closes it; `q` and `ctrl+c` still quit; all other keys are ignored
  while it is open. `esc` quits only when help is closed. While help is open,
  `layoutView` emits no body row targets, so a click cannot activate a hidden
  item. The mouse wheel is ignored. A click on a tab closes help and selects
  that tab. Cursor and scroll offset are unchanged on return.
- Narrow panes: below 24 columns of pane width the bar is hidden and the fold
  row truncates with `…`. Below `minWidth` the existing narrow message is
  shown.
- Errors render as today (header lines at `prioError`). Rate-limit cooldown
  stays in the line-1 status spans. Empty states are covered under Header.

## Testing

- `view_test.go`: update existing assertions; add cases for section order,
  badge text per state, line-1 overflow (repo truncated, then status wrapped),
  pinned/stale/cooldown still in the status, listed vs folded checks (failed
  and pending listed, the rest folded), fold row omitted when nothing folds,
  expanded order, each CI heading case, Review line per decision, stack cursor
  marker when Stack follows CI, and no body row targets while help is open.
- Footer: drop order with the new `? help` hint, and `q close` still shown at
  `minWidth`, with and without a stack.
- `barRuns` unit test: widths sum to the width, one-cell minimum, excess taken
  from the largest run, empty groups get zero.
- `keys_test.go`: table test that each binding triggers the same action as
  the old raw-string switch; `?` toggles help; `esc` closes help before
  quitting; stack keys disabled without a stack.
- `update_test.go`: spinner ticks are scheduled only while checks are pending
  or a section is loading.
- `go test ./...`, `gofmt -l`, `go vet ./...`.
- Manual: build into `bin/`, `herdr plugin link .` from this checkout, and
  compare a screenshot against this layout.
