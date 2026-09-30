# Glance Pane Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the Glance PR Overview pane as specified in `docs/superpowers/specs/2026-09-30-glance-pane-redesign-design.md`: problems first, passing checks folded, one-line identity header, Bubbles `key`/`help`/`spinner`.

**Architecture:** All work is in `internal/ui`. Rendering stays on the existing `span` / `bodyItem` / `rowTarget` model; new code adds a key map (`keys.go`), a CI block (`checks.go`), a full-help screen, and a spinner driven from the existing 2 s tick. No other package changes.

**Tech Stack:** Go, `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2` v2.2.1 (`key`, `help`, `spinner`), `github.com/charmbracelet/x/ansi`.

---

## Ground rules for every task

- Branch: `feat/pane-redesign`. Commit after each task. **Commit messages end at their last real line: no `Co-Authored-By`, no `Claude-Session`, no generated-with footer.**
- Run from repo root. Full check: `go test ./... && go vet ./... && gofmt -l internal cmd` (the last must print nothing).
- **Import alias:** `internal/ui/input_test.go` declares a package-level test helper `func key(s string)`. Any file in package `ui` that imports `charm.land/bubbles/v2/key` must alias it, or the test build fails with `key already declared through import of package`. Always write `bkey "charm.land/bubbles/v2/key"`.
- Test helpers you will use (already exist): `viewHarness()`, `overviewFixture`, `stackFixture`, `commentsFixture`, `reviewsFixture`, `plainView(m)`, `styledLine(t, content, want)`, `carries(line, sgr)`, `bodyRows(content)`, `apply(m, msg)`, `key("x")`, `click(x, y)`, `wheel(y, up)`, `harness()`.
- The overview fixture has checks: 4 failing (`unit tests`, `deploy preview`, `e2e`, `migrate`), 1 pending (`integration tests`), 2 passed (`lint`, `build`), `docs` neutral, `vendor` skipped, `mystery` unknown; `FetchedAt` is 12 s ago; repo `acme/service`, PR `#3630`.

## Deviations from the spec (recorded on purpose)

- `styleFooter` stays: it already bolds single-rune keys, including `[`, `]` and `?`.
- The footer uses a `footerHints` method on `keyMap`, not `ShortHelp`; `keyMap` does not implement `help.KeyMap`, because only `FullHelpView` is called.
- The spinner starts from the 2 s tick (Task 7 explains why and edits the spec).

## File map

| File | Change |
|---|---|
| `go.mod`, `go.sum` | Add `charm.land/bubbles/v2 v2.2.1` (already added by `go get` on this branch; commit it in Task 1). |
| `internal/ui/keys.go` | **New.** `keyMap`, `defaultKeys`, `(*Model).bindings()`, `FullHelp`, `footerHints`. |
| `internal/ui/keys_test.go` | **New.** Key map, help and footer tests. |
| `internal/ui/input.go` | `handleKey` on `bkey.Matches`; help mode; fold toggle in `activate`; help-aware `handleMouse`. |
| `internal/ui/checks.go` | **New.** `barRuns`, `ciBar`, `ciSummary`, `ciBlock`, `foldRow`, `failing`, `compact`. |
| `internal/ui/checks_test.go` | **New.** `barRuns`, CI block, fold tests. |
| `internal/ui/view.go` | Header (`identityLines`, `metaLine`), tab bar, `overviewBody` order, `reviewLine`, `stackHeading`, full-help body, footer wiring. Remove `stackBody`, `footerHints`/`stackedHints` vars, `reviewDecision`, `headRef`. |
| `internal/ui/style.go` | `badge`, underline active tab, `helpStyles`. |
| `internal/ui/model.go` | Fields `ShowHelp`, `help`, `spin`, `spinning`; init in `New`. |
| `internal/ui/update.go` | Spinner start on `TickMsg`, `spinner.TickMsg` handling, `busy()`. |
| `internal/ui/commands.go` | none |
| `internal/ui/view_test.go`, `input_test.go`, `update_test.go` | Update assertions as listed per task. |

---

### Task 1: Key map and `bkey.Matches` dispatch (no behaviour change)

**Files:**
- Create: `internal/ui/keys.go`
- Create: `internal/ui/keys_test.go`
- Modify: `internal/ui/input.go` (`handleKey`, imports)
- Commit also: `go.mod`, `go.sum`

- [ ] **Step 1: Write the failing test** — `internal/ui/keys_test.go`:

```go
package ui

import (
	"testing"

	bkey "charm.land/bubbles/v2/key"
)

func TestStackBindingsFollowTheStack(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	k := m.bindings()
	for _, b := range []bkey.Binding{k.Next, k.Prev, k.Unpin} {
		if b.Enabled() {
			t.Fatalf("%q must be disabled without a stack", b.Help().Key)
		}
	}
	stackFixture(m, *now)
	k = m.bindings()
	for _, b := range []bkey.Binding{k.Next, k.Prev, k.Unpin} {
		if !b.Enabled() {
			t.Fatalf("%q must be enabled with a stack", b.Help().Key)
		}
	}
}

func TestEveryBindingKeepsItsKeys(t *testing.T) {
	k := defaultKeys
	for _, tc := range []struct {
		b    bkey.Binding
		keys []string
	}{
		{k.Quit, []string{"q", "esc", "ctrl+c"}}, {k.Overview, []string{"1"}}, {k.Comments, []string{"2"}},
		{k.Reviews, []string{"3"}}, {k.Refresh, []string{"r"}}, {k.Open, []string{"o"}}, {k.Zoom, []string{"z"}},
		{k.Down, []string{"j", "down"}}, {k.Up, []string{"k", "up"}}, {k.PageDown, []string{"pgdown"}},
		{k.PageUp, []string{"pgup"}}, {k.Activate, []string{"enter"}}, {k.Next, []string{"]"}},
		{k.Prev, []string{"["}}, {k.Unpin, []string{"\\"}}, {k.Help, []string{"?"}},
	} {
		for _, s := range tc.keys {
			if !bkey.Matches(key(s), tc.b) {
				t.Errorf("%q must match binding %q", s, tc.b.Help().Key)
			}
		}
	}
}
```

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/ui -run 'TestStackBindingsFollowTheStack|TestEveryBindingKeepsItsKeys'`
Expected: build failure, `undefined: defaultKeys` / `m.bindings undefined`.

- [ ] **Step 3: Implement** — create `internal/ui/keys.go`:

```go
package ui

import bkey "charm.land/bubbles/v2/key"

// keyMap lists every key the pane answers to. The same bindings feed the key
// dispatch, the footer and the full help screen, so a key and its hint cannot
// drift apart.
type keyMap struct {
	Quit, Help, Overview, Comments, Reviews, Refresh, Open, Zoom bkey.Binding
	Down, Up, PageDown, PageUp, Activate, Next, Prev, Unpin      bkey.Binding
	// Stack is display-only: [ and ] are separate bindings with different
	// actions, shown as one hint. It has no keys, so it never matches.
	Stack bkey.Binding
}

func binding(keys []string, k, desc string) bkey.Binding {
	return bkey.NewBinding(bkey.WithKeys(keys...), bkey.WithHelp(k, desc))
}

var defaultKeys = keyMap{
	Quit:     binding([]string{"q", "esc", "ctrl+c"}, "q", "close"),
	Help:     binding([]string{"?"}, "?", "help"),
	Overview: binding([]string{"1"}, "1", "overview"),
	Comments: binding([]string{"2"}, "2", "comments"),
	Reviews:  binding([]string{"3"}, "3", "reviews"),
	Refresh:  binding([]string{"r"}, "r", "refresh"),
	Open:     binding([]string{"o"}, "o", "browser"),
	Zoom:     binding([]string{"z"}, "z", "zoom"),
	Down:     binding([]string{"j", "down"}, "j/↓", "next"),
	Up:       binding([]string{"k", "up"}, "k/↑", "previous"),
	PageDown: binding([]string{"pgdown"}, "pgdn", "page down"),
	PageUp:   binding([]string{"pgup"}, "pgup", "page up"),
	Activate: binding([]string{"enter"}, "↵", "expand / pin"),
	Next:     binding([]string{"]"}, "]", "stack: up"),
	Prev:     binding([]string{"["}, "[", "stack: down"),
	Unpin:    binding([]string{"\\"}, "\\", "stack: own PR"),
	Stack:    bkey.NewBinding(bkey.WithHelp("[ ]", "stack")),
}

// bindings returns the key map for the current state: the stack keys only
// exist while the pull request belongs to a stack.
func (m *Model) bindings() keyMap {
	k := defaultKeys
	stacked := m.Snapshot.Stack != nil
	k.Next.SetEnabled(stacked)
	k.Prev.SetEnabled(stacked)
	k.Unpin.SetEnabled(stacked)
	return k
}
```

Then replace `handleKey` in `internal/ui/input.go` (keep the doc comment) and add the import `bkey "charm.land/bubbles/v2/key"`:

```go
// handleKey maps a key press to a command, mutating only navigation state.
func (m *Model) handleKey(k tea.KeyPressMsg) tea.Cmd {
	keys := m.bindings()
	switch {
	case bkey.Matches(k, keys.Quit):
		m.cancel()
		return tea.Quit
	case bkey.Matches(k, keys.Overview):
		return selectSection(model.Overview)
	case bkey.Matches(k, keys.Comments):
		return selectSection(model.Comments)
	case bkey.Matches(k, keys.Reviews):
		return selectSection(model.Reviews)
	case bkey.Matches(k, keys.Refresh):
		return func() tea.Msg { return RefreshMsg{} }
	case bkey.Matches(k, keys.Open):
		return m.openSelected()
	case bkey.Matches(k, keys.Zoom):
		return runAction(m.Zoom)
	case bkey.Matches(k, keys.Down):
		m.moveCursor(1)
	case bkey.Matches(k, keys.Up):
		m.moveCursor(-1)
	case bkey.Matches(k, keys.PageDown):
		m.scroll(m.bodyHeight())
	case bkey.Matches(k, keys.PageUp):
		m.scroll(-m.bodyHeight())
	case bkey.Matches(k, keys.Activate):
		return m.activate()
	case bkey.Matches(k, keys.Next):
		return m.stackStep(1)
	case bkey.Matches(k, keys.Prev):
		return m.stackStep(-1)
	case bkey.Matches(k, keys.Unpin):
		return m.unpin()
	}
	return nil
}
```

- [ ] **Step 4: Run all UI tests**

Run: `go test ./internal/ui`
Expected: PASS (existing `input_test.go` tests prove behaviour is unchanged; `TestStackKeysAreNoOpsWithoutAStack` still passes because disabled bindings never match).

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/ui/keys.go internal/ui/keys_test.go internal/ui/input.go
git commit -m "refactor(ui): dispatch keys through a bubbles key map"
```

---

### Task 2: Full help screen (`?`)

**Files:**
- Modify: `internal/ui/model.go` (fields, `New`)
- Modify: `internal/ui/keys.go` (`FullHelp`)
- Modify: `internal/ui/style.go` (`helpStyles`)
- Modify: `internal/ui/input.go` (`handleKey`, `handleMouse`)
- Modify: `internal/ui/view.go` (`layoutView`)
- Test: `internal/ui/keys_test.go`

Behaviour (spec, Behaviour › Full help): `?` opens; `?` or `esc` closes; `q`/`ctrl+c` quit; all other keys ignored while open; wheel ignored; a tab click closes help and selects the tab; no body row targets while open; `Cursor`/`Offset` untouched.

- [ ] **Step 1: Write the failing tests** — append to `internal/ui/keys_test.go` (add imports `strings`, `tea "charm.land/bubbletea/v2"`, `"github.com/charmbracelet/x/ansi"`, `"github.com/cupsadarius/herdr-pr-glance/internal/model"`):

```go
func TestHelpTogglesAndSwallowsKeys(t *testing.T) {
	m, now := viewHarness()
	stackFixture(m, *now)
	m.Cursor, m.Offset = 2, 0
	apply(m, key("?"))
	if !m.ShowHelp {
		t.Fatal("? must open help")
	}
	plain := plainView(m)
	for _, want := range []string{"refresh", "browser", "stack: up", "page down"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("help misses %q:\n%s", want, plain)
		}
	}
	for _, k := range []string{"j", "r", "1", "enter", "]"} {
		if cmd := apply(m, key(k)); cmd != nil || !m.ShowHelp || m.Cursor != 2 {
			t.Fatalf("%q must be ignored while help is open", k)
		}
	}
	apply(m, key("esc"))
	if m.ShowHelp || m.Cursor != 2 {
		t.Fatal("esc must close help and keep the cursor")
	}
	apply(m, key("?"))
	apply(m, key("?"))
	if m.ShowHelp {
		t.Fatal("? must close help")
	}
	apply(m, key("?"))
	if cmd := apply(m, key("q")); cmd == nil {
		t.Fatal("q must still quit while help is open")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q must return tea.Quit")
	}
}

func TestHelpHidesStackKeysWithoutAStack(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.ShowHelp = true
	if plain := plainView(m); strings.Contains(plain, "stack: up") {
		t.Fatalf("stack keys must not be listed without a stack:\n%s", plain)
	}
}

func TestHelpMouse(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 100
	m.ShowHelp = true
	_, rows := m.layoutView()
	for _, r := range rows {
		if r.tab == "" {
			t.Fatalf("help must emit no body row targets, got %+v", r)
		}
	}
	if apply(m, wheel(20, false)); m.Offset != 0 {
		t.Fatal("the wheel must be ignored while help is open")
	}
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	y, x := -1, -1
	for i, l := range lines {
		if j := strings.Index(l, "Comments"); j >= 0 && strings.Contains(l, "Reviews") {
			y, x = i, j+2
		}
	}
	cmd := apply(m, click(x, y))
	if m.ShowHelp || cmd == nil {
		t.Fatal("a tab click must close help and select the tab")
	}
	if msg, ok := cmd().(SelectSectionMsg); !ok || model.Section(msg) != model.Comments {
		t.Fatalf("tab click produced %#v", msg)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go test ./internal/ui -run 'TestHelp'`
Expected: build failure `m.ShowHelp undefined`.

- [ ] **Step 3: Implement**

`internal/ui/model.go` — add import `"charm.land/bubbles/v2/help"`; add fields to `Model` right after `Expanded map[string]bool`:

```go
	// ShowHelp replaces the body with the full key list; the selection and
	// scroll position underneath are left alone.
	ShowHelp bool
```

and next to `md *markdown`:

```go
	// help renders the full key list; it holds only styles.
	help help.Model
```

In `New`, build the model then set help (replace the single `return &Model{...}` line):

```go
	m := &Model{md: newMarkdown(), resolver: r, github: g, cache: c, now: now, ctx: ctx, cancel: cancel, Section: model.Overview, Discussions: map[model.Section]*DiscussionState{}, Expanded: map[string]bool{}}
	m.help = help.New()
	m.help.Styles = helpStyles()
	return m
```

`internal/ui/style.go` — add import `"charm.land/bubbles/v2/help"` and:

```go
// helpStyles replaces Bubbles' hex defaults with the palette, so the help
// screen follows the terminal theme like the rest of the pane.
func helpStyles() help.Styles {
	return help.Styles{
		Ellipsis: pal.faint, ShortKey: pal.bold, ShortDesc: pal.faint, ShortSeparator: pal.faint,
		FullKey: pal.bold, FullDesc: pal.faint, FullSeparator: pal.faint,
	}
}
```

`internal/ui/keys.go` — add:

```go
// FullHelp groups the bindings for the help screen. Disabled bindings (the
// stack keys without a stack) are skipped by the help renderer.
func (k keyMap) FullHelp() [][]bkey.Binding {
	return [][]bkey.Binding{
		{k.Overview, k.Comments, k.Reviews, k.Help, k.Quit},
		{k.Down, k.Up, k.PageDown, k.PageUp, k.Activate},
		{k.Refresh, k.Open, k.Zoom},
		{k.Next, k.Prev, k.Unpin},
	}
}
```

`internal/ui/input.go` — at the top of `handleKey`, right after `keys := m.bindings()`:

```go
	if m.ShowHelp {
		switch {
		case bkey.Matches(k, keys.Help), k.String() == "esc":
			m.ShowHelp = false
		case k.String() == "q", k.String() == "ctrl+c":
			m.cancel()
			return tea.Quit
		}
		return nil
	}
```

and add a case to the main switch, before `keys.Overview`:

```go
	case bkey.Matches(k, keys.Help):
		m.ShowHelp = true
```

In `handleMouse`, make the wheel cases a no-op while help is open, and close help on a tab click:

```go
	case tea.MouseWheelUp:
		if !m.ShowHelp {
			m.scroll(-wheelStep)
		}
		return nil
	case tea.MouseWheelDown:
		if !m.ShowHelp {
			m.scroll(wheelStep)
		}
		return nil
```

```go
		if r.tab != "" {
			m.ShowHelp = false
			return selectSection(r.tab)
		}
```

`internal/ui/view.go` — add a method and use it in `layoutView`. Add:

```go
// helpLines renders each help group as its own block: the pane is too narrow
// for Bubbles' side-by-side columns.
func (m *Model) helpLines() []string {
	var out []string
	for _, g := range m.bindings().FullHelp() {
		block := m.help.FullHelpView([][]bkey.Binding{g})
		if block == "" {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		for _, l := range strings.Split(block, "\n") {
			out = append(out, " "+l)
		}
	}
	return out
}
```

(add `bkey "charm.land/bubbles/v2/key"` to view.go imports). In `layoutView`, replace the body loop:

```go
	for i := off; i < len(lines) && i-off < bodyHigh; i++ {
		rows = append(rows, rowTarget{y: len(out), x1: -1, item: itemAt(spans, i)})
		out = append(out, ansi.Truncate(lines[i], w, "…"))
	}
```

with:

```go
	if m.ShowHelp {
		for _, l := range m.helpLines() {
			out = append(out, ansi.Truncate(l, w, "…"))
		}
		return strings.Join(append(pad(out, w, h), foot), "\n"), rows
	}
	for i := off; i < len(lines) && i-off < bodyHigh; i++ {
		rows = append(rows, rowTarget{y: len(out), x1: -1, item: itemAt(spans, i)})
		out = append(out, ansi.Truncate(lines[i], w, "…"))
	}
```

(`pad` already cuts to the height, so a long help list is clipped, never overflows.)

- [ ] **Step 4: Run**

Run: `gofmt -w internal/ui && go test ./internal/ui`
Expected: PASS. `TestViewDoesNotMutateModel` still passes (View only reads `help`). (`gofmt -w` realigns the `Model` struct fields.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui
git commit -m "feat(ui): full key help on ?"
```

---

### Task 3: Footer hints come from the key map

**Files:**
- Modify: `internal/ui/keys.go` (`footerHints`)
- Modify: `internal/ui/view.go` (remove `footerHints`/`stackedHints` vars; `footerLine` signature; `layoutView`)
- Test: `internal/ui/keys_test.go`, `internal/ui/view_test.go`

Spec: display order `[ ] stack` (stack only), `r refresh`, `o browser`, `z zoom`, `? help`, `q close`; drop order `o`, `r`, `z`, `[ ]`, `?`; `q close` never dropped. `styleFooter` stays (it already bolds single-rune keys, including `[`, `]`, `?`).

- [ ] **Step 1: Write the failing test** — append to `keys_test.go`:

```go
func TestFooterDropOrder(t *testing.T) {
	for _, tc := range []struct {
		stacked bool
		w       int
		want    string
	}{
		{false, 100, "r refresh  o browser  z zoom  ? help  q close"},
		{false, 41, "r refresh o browser z zoom ? help q close"},
		{false, 31, "r refresh z zoom ? help q close"},
		{false, 21, "z zoom ? help q close"},
		{false, 14, "? help q close"},
		{false, 7, "q close"},
		{true, 100, "[ ] stack  r refresh  o browser  z zoom  ? help  q close"},
		{true, 45, "[ ] stack  r refresh  z zoom  ? help  q close"},
		{true, 34, "[ ] stack  z zoom  ? help  q close"},
		{true, 24, "[ ] stack ? help q close"},
		{true, 20, "? help  q close"},
	} {
		if got := footerLine(tc.w, defaultKeys.footerHints(tc.stacked)); got != tc.want {
			t.Errorf("stacked=%v w=%d: got %q want %q", tc.stacked, tc.w, got, tc.want)
		}
	}
}
```

How the rows were derived (`footerLine` tries the list with two-space, then one-space separators, then drops the highest remaining rank and retries). Hint widths: `[ ] stack`=9, `r refresh`=9, `o browser`=9, `z zoom`=6, `? help`=6, `q close`=7. Example, stacked at 34: all six need 51 even with single spaces; without `o` 41; without `r` the four hints need 28 + 3×2 = 34 with double spaces, which fits.

Update existing tests in `view_test.go`:
- `TestOverviewRendersIdentityStatisticsAndChecks`: `"r refresh  o browser  z zoom  q close"` → `"r refresh  o browser  z zoom  ? help  q close"`.
- `TestStackFooterHint`: at `m.Width = 45` it asserts `[ ] stack` and `q close` survive — keep; it must still pass.

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/ui -run 'TestFooterDropOrder|TestOverviewRendersIdentity|TestStackFooterHint'`
Expected: build failure (`footerHints` method undefined / `footerLine` argument type).

- [ ] **Step 3: Implement**

`keys.go`:

```go
// footerHints pairs the short-help bindings with drop ranks: the higher the
// rank, the sooner a hint goes when the pane is too narrow. q close has rank
// zero and footerLine never drops the last hint, so quitting stays visible.
func (k keyMap) footerHints(stacked bool) []footerHint {
	hint := func(b bkey.Binding, drop int) footerHint {
		h := b.Help()
		return footerHint{text: h.Key + " " + h.Desc, drop: drop}
	}
	hints := []footerHint{hint(k.Refresh, 4), hint(k.Open, 5), hint(k.Zoom, 3), hint(k.Help, 1), hint(k.Quit, 0)}
	if stacked {
		hints = append([]footerHint{hint(k.Stack, 2)}, hints...)
	}
	return hints
}
```

`view.go`: delete the `footerHints` and `stackedHints` package vars and their comments. Change `footerLine`:

```go
// footerLine fits as many control hints as the width allows, closing last.
func footerLine(w int, hints []footerHint) string {
	for {
```

(delete the old `hints := footerHints; if stacked {...}` lines; the rest of the body is unchanged). In `layoutView`:

```go
	foot := styleFooter(footerLine(w, m.bindings().footerHints(m.Snapshot.Stack != nil)))
```

- [ ] **Step 4: Run**

Run: `go test ./internal/ui`
Expected: PASS. `TestRenderBoundsAcrossSizes` asserts `q close` at every size — must still pass. The golden `TestColorLeavesTheLayoutUnchanged` will fail on the footer line: update only the footer line of `goldenOverview44` and `goldenReviews44` at the bottom of `view_test.go` to the new width-44 footer (`r refresh o browser z zoom ? help q close` if one-space fits, per `TestFooterDropOrder`).

- [ ] **Step 5: Commit**

```bash
git add internal/ui
git commit -m "feat(ui): footer hints from the key map, with ? help"
```

---

### Task 4: `barRuns`

**Files:**
- Create: `internal/ui/checks.go`
- Create: `internal/ui/checks_test.go`

- [ ] **Step 1: Write the failing test** — `internal/ui/checks_test.go`:

```go
package ui

import (
	"testing"

	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

func TestBarRuns(t *testing.T) {
	for _, tc := range []struct {
		name  string
		c     model.CheckCounts
		width int
		want  [4]int
	}{
		{"screenshot PR", model.CheckCounts{Failed: 2, Passed: 33, Neutral: 1, Skipped: 22}, 40, [4]int{1, 0, 24, 15}},
		{"tiny failing share keeps a cell", model.CheckCounts{Failed: 1, Passed: 1000}, 40, [4]int{1, 0, 39, 0}},
		{"only passed fills the bar", model.CheckCounts{Passed: 5}, 30, [4]int{0, 0, 30, 0}},
		{"every group", model.CheckCounts{Failed: 1, Pending: 1, Passed: 1, Unknown: 1}, 23, [4]int{8, 5, 5, 5}},
		{"no checks", model.CheckCounts{}, 40, [4]int{}},
	} {
		got := barRuns(tc.c, tc.width)
		if got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
		sum := got[0] + got[1] + got[2] + got[3]
		if tc.c != (model.CheckCounts{}) && sum != tc.width {
			t.Errorf("%s: runs sum to %d, want %d", tc.name, sum, tc.width)
		}
	}
}
```

(Check of the "every group" row: each share is `1*23/4 = 5`, sum 20; the largest run is the first non-empty one, index 0, which absorbs the missing 3 → `8`.)

- [ ] **Step 2: Run to see it fail**

Run: `go test ./internal/ui -run TestBarRuns`
Expected: `undefined: barRuns`.

- [ ] **Step 3: Implement** — `internal/ui/checks.go`:

```go
package ui

import "github.com/cupsadarius/herdr-pr-glance/internal/model"

// barRuns splits width into the failing, running, passed and other runs of the
// CI bar. Each run is proportional to its count and any non-empty group keeps
// at least one cell; the largest run absorbs rounding and the cells the
// minimums borrowed, so the runs always fill the width exactly. Callers only
// draw the bar at widths well above four cells.
func barRuns(c model.CheckCounts, width int) [4]int {
	counts := [4]int{c.Failed, c.Pending, c.Passed, c.Neutral + c.Skipped + c.Unknown}
	total := counts[0] + counts[1] + counts[2] + counts[3]
	var runs [4]int
	if total == 0 || width <= 0 {
		return runs
	}
	sum, largest := 0, -1
	for i, n := range counts {
		if n == 0 {
			continue
		}
		runs[i] = max(1, n*width/total)
		sum += runs[i]
		if largest < 0 || runs[i] > runs[largest] {
			largest = i
		}
	}
	runs[largest] += width - sum
	return runs
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/ui -run TestBarRuns`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/checks.go internal/ui/checks_test.go
git commit -m "feat(ui): split the CI bar into state runs"
```

---

### Task 5: CI block, Review line, section order, stack heading

**Files:**
- Modify: `internal/ui/checks.go` (CI block)
- Modify: `internal/ui/view.go` (`overviewBody`, `bodyItem.fold`, `reviewLine`, `stackHeading`; delete `stackBody`)
- Modify: `internal/ui/input.go` (`activate`)
- Test: `internal/ui/checks_test.go`, `internal/ui/view_test.go`

- [ ] **Step 1: Write the failing tests** — append to `checks_test.go` (add imports `strings`):

```go
func TestCIBlockListsProblemsAndFoldsTheRest(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width, m.Height = 100, 60
	rows := strings.Join(bodyRows(m.View().Content), "\n")
	for _, want := range []string{"CI  ◷ 1 running · ✗ 4 failing", "× unit tests", "× migrate", "◷ integration tests",
		"▸ ✓ 2 passed · 1 neutral · 1 skipped · 1 unknown"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("missing %q in:\n%s", want, rows)
		}
	}
	for _, folded := range []string{"lint", "build", "docs", "vendor", "mystery"} {
		if strings.Contains(rows, folded) {
			t.Fatalf("%q must be folded:\n%s", folded, rows)
		}
	}
	if strings.Index(rows, "migrate") > strings.Index(rows, "integration tests") {
		t.Fatalf("failing rows come before running rows:\n%s", rows)
	}
	// The bar is one run of glyphs as wide as the content column.
	for _, r := range bodyRows(m.View().Content) {
		if strings.Contains(r, "━") && ansi.StringWidth(strings.TrimSpace(r)) != m.contentWidth() {
			t.Fatalf("bar width %d, want %d: %q", ansi.StringWidth(strings.TrimSpace(r)), m.contentWidth(), r)
		}
	}
}

func TestFoldRowExpandsAndSurvivesRefresh(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width, m.Height = 100, 60
	m.Cursor = 5 // 4 failing + 1 running rows come first; index 5 is the fold row
	apply(m, key("enter"))
	rows := strings.Join(bodyRows(m.View().Content), "\n")
	if !strings.Contains(rows, "▾ ✓ 2 passed") {
		t.Fatalf("fold row must show expanded:\n%s", rows)
	}
	lint, build, docs, vendor, mystery := strings.Index(rows, "✓ lint"), strings.Index(rows, "✓ build"),
		strings.Index(rows, "· docs"), strings.Index(rows, "· vendor"), strings.Index(rows, "? mystery")
	if lint < 0 || !(lint < build && build < docs && docs < vendor && vendor < mystery) {
		t.Fatalf("expanded checks must follow in snapshot order:\n%s", rows)
	}
	apply(m, SummaryResult{Generation: m.Generation, Data: m.Snapshot})
	if !strings.Contains(plainView(m), "▾ ✓ 2 passed") {
		t.Fatal("expansion must survive a refresh")
	}
	apply(m, key("enter"))
	if strings.Contains(plainView(m), "✓ lint") {
		t.Fatal("enter on the fold row must collapse it again")
	}
}

func TestCIHeadingCases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks []model.Check
		counts model.CheckCounts
		want   string
		noFold bool
	}{
		{"failing only", []model.Check{{Name: "a", State: model.CheckFailed}, {Name: "b", State: model.CheckPassed}},
			model.CheckCounts{Failed: 1, Passed: 1}, "CI  ✗ 1 failing", false},
		{"running only", []model.Check{{Name: "a", State: model.CheckPending}},
			model.CheckCounts{Pending: 1}, "CI  ◷ 1 running", true},
		{"all passed", []model.Check{{Name: "a", State: model.CheckPassed}, {Name: "b", State: model.CheckSkipped}},
			model.CheckCounts{Passed: 1, Skipped: 1}, "CI  ✓ 1 passed", false},
		{"only skipped", []model.Check{{Name: "a", State: model.CheckSkipped}},
			model.CheckCounts{Skipped: 1}, "CI  1 skipped", false},
		{"none", nil, model.CheckCounts{}, "CI  No checks", true},
	} {
		m, now := viewHarness()
		overviewFixture(m, *now)
		m.Width = 100
		m.Snapshot.Checks, m.Snapshot.CheckCounts = tc.checks, tc.counts
		plain := plainView(m)
		if !strings.Contains(plain, tc.want) {
			t.Errorf("%s: missing %q in:\n%s", tc.name, tc.want, plain)
		}
		if hasFold := strings.Contains(plain, "▸"); hasFold == tc.noFold {
			t.Errorf("%s: fold row present=%v, want %v", tc.name, hasFold, !tc.noFold)
		}
		if tc.name == "only skipped" && strings.Contains(plain, "✓") {
			t.Errorf("only skipped: nothing passed, so no ✓:\n%s", plain)
		}
	}
}

func TestReviewLineAndSectionOrder(t *testing.T) {
	for _, tc := range []struct{ decision, want string }{
		{"APPROVED", "Review  approved"}, {"CHANGES_REQUESTED", "Review  changes requested"},
		{"REVIEW_REQUIRED", "Review  review required"}, {"", "Review  no decision yet"}, {"WEIRD", "Review  no decision yet"},
	} {
		m, now := viewHarness()
		stackFixture(m, *now)
		m.Width, m.Height = 100, 60
		m.Snapshot.ReviewDecision = tc.decision
		rows := strings.Join(bodyRows(m.View().Content), "\n")
		ci, review, stack := strings.Index(rows, "CI  "), strings.Index(rows, tc.want), strings.Index(rows, "Stack  #3710 · 2/3 · onto main")
		if ci < 0 || review < 0 || stack < 0 || !(ci < review && review < stack) {
			t.Fatalf("%q: want CI, then %q, then the stack heading:\n%s", tc.decision, tc.want, rows)
		}
	}
}

func TestStackCursorMarkerAfterCI(t *testing.T) {
	m, now := viewHarness()
	stackFixture(m, *now)
	m.Width, m.Height = 100, 60
	// 4 failing + 1 running + fold row = 6 items before the stack; the shown
	// entry (#3630) is the second stack row.
	m.Cursor = 7
	row := ansi.Strip(stackRowLine(t, m.View().Content, "#3630"))
	if !strings.HasPrefix(row, "›") || []rune(row)[1] == '›' {
		t.Fatalf("cursor on the shown entry must draw exactly one marker: %q", row)
	}
	m.Cursor = 0
	if row := ansi.Strip(stackRowLine(t, m.View().Content, "#3630")); []rune(row)[1] != '›' {
		t.Fatalf("the shown entry keeps its own marker when the cursor is elsewhere: %q", row)
	}
}
```

(Add `"github.com/charmbracelet/x/ansi"` to `checks_test.go` imports.)

Update existing tests in `view_test.go`:
- `TestStackBlockListsEntriesTopFirstAndMarksTheCurrentOne`: heading `"STACK #3710 · 2/3 · base main"` → `"Stack  #3710 · 2/3 · onto main"`; replace the `checks := strings.Index(rows, "CHECKS")` block and ordering assertion with: `ci := strings.Index(rows, "CI  ")` and `if !(ci < top && top < current && current < bottom)` with message `"the stack renders top first, below CI"`.
- `TestStackHeadingWithoutAKnownPosition`: `"STACK #3710 · ?/3 · base main"` → `"Stack  #3710 · ?/3 · onto main"`.
- `TestCursorOnTheShownEntryShowsOneMarker`: `m.Cursor = 1` → `m.Cursor = 7` with comment `// 6 CI items come first; the shown entry is the second stack row`.
- `TestStackRowsCarryStateColors`: `styledLine(t, out, "STACK")` → `styledLine(t, out, "Stack  #3710")`.
- `TestStackFooterHint`: `strings.Contains(plain, "STACK")` → `strings.Contains(plain, "Stack  #")`.
- `TestOverviewRendersIdentityStatisticsAndChecks`: in the want list replace `"CHECKS", "4 failed", "1 pending", "2 passed"` with `"CI  ◷ 1 running · ✗ 4 failing", "✓ 2 passed"`, and drop `"✓ lint", "✓ build", "· docs", "· vendor", "? mystery"` (now folded). Leave the header entries for Task 6.
- `TestEmptyCheckListSaysNoChecks`: unchanged (still "No checks", still no "passed").
- `TestCheckRowsAndCountsCarryStateColors`: set `m.Expanded["checks"] = true` after the fixture so folded rows render; replace `{"4 failed", sgrRed}, {"1 pending", sgrYellow}, {"CHECKS", sgrBold}` with `{"4 failing", sgrRed}, {"1 running", sgrYellow}, {"CI  ", sgrBold}, {"2 passed", sgrGreen}`.
- `survivors["stack"]`: `"STACK"` → `"Stack  #"`.

Update existing tests in `input_test.go`:
- `TestCursorMovementAndPaging`: the cursor now clamps at the last overview item, which is 5 (4 failing + 1 running + fold row), not `len(m.Snapshot.Checks)-1`. Replace that expected value with `5`, with a comment `// 4 failing, 1 running, then the fold row`.
- `TestClickAndOpenOnAStackRow`: before pressing `o`, set `m.Cursor = 6` (the first stack row, #3709, follows the 6 CI items) with a comment saying so.

Add to `checks_test.go` (the spec says `o` on the fold row does nothing):

```go
func TestOpenOnTheFoldRowDoesNothing(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	opened := ""
	m.Open = func(u string) error { opened = u; return nil }
	m.Cursor = 5 // the fold row
	if cmd := apply(m, key("o")); cmd != nil {
		cmd()
	}
	if opened != "" {
		t.Fatalf("o on the fold row must not open anything, opened %q", opened)
	}
}
```

- [ ] **Step 2: Run to see them fail**

Run: `go test ./internal/ui`
Expected: FAIL on the new tests and the updated assertions (old layout still renders `CHECKS`, `STACK`).

- [ ] **Step 3: Implement**

Append to `internal/ui/checks.go` (extend imports to `strconv`, `strings`, `lipgloss "charm.land/lipgloss/v2"`, `model`):

```go
// foldKey marks the fold row in Model.Expanded. Review-thread keys are GitHub
// node IDs, which never equal it.
const foldKey = "checks"

// minBarWidth is the narrowest pane that still draws the CI bar.
const minBarWidth = 24

// failing reports the states the pane treats as needing attention, the same
// four the snapshot counts as failed.
func failing(s model.CheckState) bool {
	switch s {
	case model.CheckFailed, model.CheckTimedOut, model.CheckCancelled, model.CheckActionRequired:
		return true
	}
	return false
}

// ciBar draws barRuns as coloured glyph runs; "other" uses a thinner rule so it
// reads as background.
func ciBar(c model.CheckCounts, w int) string {
	runs := barRuns(c, w)
	glyphs := [4]string{"━", "━", "━", "─"}
	styles := [4]lipgloss.Style{pal.red, pal.yellow, pal.green, pal.faint}
	var b strings.Builder
	for i, n := range runs {
		b.WriteString(styled(styles[i], strings.Repeat(glyphs[i], n)))
	}
	return b.String()
}

// ciSummary is the heading's verdict: the worst thing first.
func (m *Model) ciSummary() []span {
	c := m.Snapshot.CheckCounts
	running := span{text: m.pendingGlyph() + " " + strconv.Itoa(c.Pending) + " running", style: pal.yellow}
	failed := span{text: "✗ " + strconv.Itoa(c.Failed) + " failing", style: pal.red}
	switch {
	case len(m.Snapshot.Checks) == 0:
		return []span{{text: "No checks", style: pal.faint}}
	case c.Failed > 0 && c.Pending > 0:
		return []span{running, {text: " · ", style: pal.faint}, failed}
	case c.Failed > 0:
		return []span{failed}
	case c.Pending > 0:
		return []span{running}
	case c.Passed > 0:
		return []span{{text: "✓ " + strconv.Itoa(c.Passed) + " passed", style: pal.green}}
	default:
		return countsSpans(c)
	}
}

// pendingGlyph is the running-check glyph; Task 7 turns it into the spinner.
func (m *Model) pendingGlyph() string { return "◷" }

// ciBlock renders the CI heading, the bar and the check rows. Failing and
// running checks are listed; the rest fold into one selectable summary row
// that enter expands in place.
func (m *Model) ciBlock(w, paneW int) ([]string, []bodyItem) {
	s := m.Snapshot
	heading := append([]span{{text: "CI", style: pal.bold}, {text: "  ", style: pal.none}}, m.ciSummary()...)
	head := wrapSpans(heading, w)
	if len(s.Checks) == 0 {
		return head, nil
	}
	if paneW >= minBarWidth {
		head = append(head, ciBar(s.CheckCounts, w))
	}
	var items []bodyItem
	row := func(c model.Check) bodyItem {
		return bodyItem{lines: []string{m.checkLine(c, w)}, url: c.URL}
	}
	var folded []model.Check
	for _, c := range s.Checks {
		if failing(c.State) {
			items = append(items, row(c))
		}
	}
	for _, c := range s.Checks {
		switch {
		case c.State == model.CheckPending:
			items = append(items, row(c))
		case !failing(c.State):
			folded = append(folded, c)
		}
	}
	if len(folded) == 0 {
		return head, items
	}
	items = append(items, bodyItem{lines: []string{m.foldRow(w)}, fold: true})
	if m.Expanded[foldKey] {
		for _, c := range folded {
			items = append(items, row(c))
		}
	}
	return head, items
}

// checkLine is a check row; Task 7 swaps the running glyph for the spinner.
func (m *Model) checkLine(c model.Check, w int) string { return checkRow(c, w) }

// foldRow summarises the folded checks. A green tick appears only when
// something actually passed.
func (m *Model) foldRow(w int) string {
	c := m.Snapshot.CheckCounts
	arrow := "▸"
	if m.Expanded[foldKey] {
		arrow = "▾"
	}
	spans := []span{{text: arrow + " ", style: pal.bold}}
	sep := false
	add := func(n int, text string, style lipgloss.Style) {
		if n <= 0 {
			return
		}
		if sep {
			spans = append(spans, span{text: " · ", style: pal.faint})
		}
		spans = append(spans, span{text: text, style: style})
		sep = true
	}
	add(c.Passed, "✓ "+strconv.Itoa(c.Passed)+" passed", pal.green)
	add(c.Neutral, strconv.Itoa(c.Neutral)+" neutral", pal.faint)
	add(c.Skipped, strconv.Itoa(c.Skipped)+" skipped", pal.faint)
	add(c.Unknown, strconv.Itoa(c.Unknown)+" unknown", pal.faint)
	return truncateSpans(spans, w)
}
```

`internal/ui/view.go`:

1. Add `fold bool` to `bodyItem`, after `pin *model.PR`:

```go
	// fold marks the CI summary row; activating it expands the folded checks.
	fold bool
```

2. Replace `overviewBody` and delete `stackBody` entirely:

```go
// overviewBody lays out CI, then the review decision, then the stack. Lines
// that introduce a group ride on the next item as its head, so the cursor and
// the row map skip them; whatever is left over trails the last item.
func (m *Model) overviewBody(w int) ([]string, []bodyItem, []string) {
	paneW, _ := m.size()
	var items []bodyItem
	var pending []string
	add := func(it bodyItem) {
		it.head = append(pending, it.head...)
		pending = nil
		items = append(items, it)
	}
	head, ci := m.ciBlock(w, paneW)
	pending = append(pending, head...)
	for _, it := range ci {
		add(it)
	}
	pending = append(pending, "", m.reviewLine(w))
	if s := m.Snapshot.Stack; s != nil {
		pending = append(pending, "", m.stackHeading(w))
		for i := len(s.Entries) - 1; i >= 0; i-- {
			e := s.Entries[i]
			pin := e.PR
			// The cursor gutter already points at the row it selects, so the
			// entry marker stands down there. The index is the item's place in
			// the whole body, which now starts with the CI rows.
			add(bodyItem{lines: []string{m.stackRow(e, w, m.Cursor == len(items))}, url: e.PR.URL, pin: &pin})
		}
		if hidden := s.Size - len(s.Entries); hidden > 0 {
			pending = append(pending, styled(pal.faint, fmt.Sprintf("+%d more", hidden)))
		}
	}
	return nil, items, pending
}

// reviewLine states the review decision in words.
func (m *Model) reviewLine(w int) string {
	text := "no decision yet"
	switch strings.ToUpper(strings.ReplaceAll(clean(m.Snapshot.ReviewDecision), " ", "_")) {
	case "APPROVED":
		text = "approved"
	case "CHANGES_REQUESTED":
		text = "changes requested"
	case "REVIEW_REQUIRED":
		text = "review required"
	}
	return truncateSpans([]span{{text: "Review", style: pal.bold}, {text: "  ", style: pal.none},
		{text: text, style: decisionStyle(m.Snapshot.ReviewDecision)}}, w)
}

// stackHeading names the stack, the shown entry's position and the trunk.
func (m *Model) stackHeading(w int) string {
	s := m.Snapshot
	return truncateSpans([]span{{text: "Stack", style: pal.bold}, {text: fmt.Sprintf("  #%d · %s/%d · onto %s",
		s.Stack.Number, position(s.StackPosition), s.Stack.Size, clean(s.Stack.BaseBranch)), style: pal.faint}}, w)
}
```

Note `decisionStyle` already maps unknown decisions to faint, so `"WEIRD"` reads `no decision yet` in faint.

3. `internal/ui/input.go`, in `selectedURL`, make the fold row open nothing instead of falling back to the PR URL:

```go
func (m *Model) selectedURL() string {
	it, ok := m.selected()
	if ok && it.fold {
		return ""
	}
	if ok && it.url != "" {
		return it.url
	}
	if m.Snapshot.PR != nil {
		return m.Snapshot.PR.URL
	}
	return ""
}
```

4. `internal/ui/input.go`, in `activate`, before `m.toggleThread(it)`:

```go
	if it.fold {
		if m.Expanded == nil {
			m.Expanded = map[string]bool{}
		}
		m.Expanded[foldKey] = !m.Expanded[foldKey]
		m.clampReveal()
		return nil
	}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/ui`
Expected: all pass except `TestColorLeavesTheLayoutUnchanged` (golden layout). Do not update the goldens yet; add this as the first line of `TestColorLeavesTheLayoutUnchanged` so the suite stays green until Task 8:

```go
	t.Skip("layout goldens are regenerated in the last task of the pane redesign")
```

as the first line of `TestColorLeavesTheLayoutUnchanged`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui
git commit -m "feat(ui): problems-first CI block, review line and stack after CI"
```

---

### Task 6: Header — badge, identity line, meta line, tab bar

**Files:**
- Modify: `internal/ui/style.go` (`badge`, `activeTab`)
- Modify: `internal/ui/view.go` (`headerLines`, `identityLines`, `metaLine`, `statusSpans`, `tabLine`, priorities; delete `headRef`, `reviewDecision`, `prioRepo`, `prioAuthor`, `prioReview`, `prioDiff`, `prioState`)
- Modify: `internal/ui/checks.go` (`compact`)
- Test: `internal/ui/view_test.go`, `internal/ui/checks_test.go`

- [ ] **Step 1: Write the failing tests** — append to `checks_test.go`:

```go
func TestCompactCounts(t *testing.T) {
	for n, want := range map[int]string{0: "0", 9999: "9999", 10000: "10k", 25365: "25k", 999999: "999k", 1000000: "1M", 2500000: "2M"} {
		if got := compact(n); got != want {
			t.Errorf("compact(%d) = %q, want %q", n, got, want)
		}
	}
}
```

Append to `view_test.go`:

```go
func TestIdentityLine(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 100
	first := strings.Split(plainView(m), "\n")[0]
	if !strings.HasPrefix(first, " OPEN  #3630  acme/service") || !strings.HasSuffix(strings.TrimRight(first, " "), "12s ago") {
		t.Fatalf("line 1 must be badge, number, repo … age: %q", first)
	}
	for state, want := range map[string]string{"OPEN": " OPEN ", "MERGED": " MERGED ", "CLOSED": " CLOSED ", "": " UNKNOWN "} {
		m.Snapshot.State, m.Snapshot.Draft = state, false
		if first := strings.Split(plainView(m), "\n")[0]; !strings.HasPrefix(first, want) {
			t.Errorf("state %q: line 1 %q, want prefix %q", state, first, want)
		}
	}
	m.Snapshot.State, m.Snapshot.Draft = "OPEN", true
	if first := strings.Split(plainView(m), "\n")[0]; !strings.HasPrefix(first, " DRAFT ") {
		t.Errorf("draft: %q", first)
	}
}

func TestIdentityLineOverflow(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 30
	lines := strings.Split(plainView(m), "\n")
	if !strings.HasPrefix(lines[0], " OPEN  #3630") || !strings.Contains(lines[0], "12s ago") {
		t.Fatalf("at 30 columns the repo gives way, the status stays: %q", lines[0])
	}
	m.Width = 20
	m.CooldownUntil = now.Add(97 * time.Second)
	plain := plainView(m)
	lines = strings.Split(plain, "\n")
	if !strings.HasPrefix(lines[0], " OPEN  #3630") || strings.Contains(lines[0], "acme") {
		t.Fatalf("line 1 keeps badge and number: %q", lines[0])
	}
	// The status wraps over several lines at 20 columns, so check its tail.
	if !strings.Contains(plain, "97s") || !strings.Contains(plain, "rate") {
		t.Fatalf("a status that cannot share line 1 wraps below it:\n%s", plain)
	}
}

func TestMetaLineAndTabs(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 100
	m.Snapshot.Additions = 25365
	plain := plainView(m)
	for _, want := range []string{"@author · 143 commits · 8 files · +25k -76", "Overview  Comments  Reviews"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in:\n%s", want, plain)
		}
	}
	for _, gone := range []string{"GLANCE PR", "[Overview]", "Review:", "feature/retry → main", "refreshed"} {
		if strings.Contains(plain, gone) {
			t.Fatalf("%q must be gone from the PR header:\n%s", gone, plain)
		}
	}
	m.Pinned = &model.PR{Number: 1}
	if !strings.Contains(strings.Split(plainView(m), "\n")[0], "pinned") {
		t.Fatal("a pinned PR must say so on line 1")
	}
}
```

Update existing tests in `view_test.go`:
- `TestOverviewRendersIdentityStatisticsAndChecks` want list: remove `"GLANCE PR"`, `"refreshed 12s ago"`, `"feature/retry"` (both), `"feature/retry → main"`, `"Review: Changes requested"`, `"[Overview]"`; add `"12s ago"`, `"Review  changes requested"`, `"Overview"`.
- Delete `TestForkHeadIdentity` (head ref no longer rendered; spec removes the branch from the header).
- `TestErrorCooldownStaleAndCacheWarning`: `"refreshed 12s ago · rate limited, retry in 97s"` → `"12s ago · rate limited, retry in 97s"`; `"refreshed 1m ago · stale · rate limited, retry in 97s"` → `"1m ago · stale · rate limited, retry in 97s"`.
- `TestHeaderTabsAndFooterCarryTheirStyles`: replace the table with:

```go
	for _, tc := range []struct{ want, sgr string }{
		{"12s ago", sgrFaint}, {"acme/service", sgrFaint},
		{"#3630", sgrBold}, {"OPEN", "42"}, {"OPEN", "30"},
		{"Re-request denied", sgrBold}, {"@author", sgrCyan},
		{"+284", sgrGreen}, {"-76", sgrRed},
		{"Review  approved", sgrGreen},
		{"Overview", "4"}, {"Overview", sgrBold},
		{"q close", sgrBold}, {"q close", sgrFaint},
	} {
```

and the MERGED/REVIEW_REQUIRED follow-up with `{"MERGED", "45"}` (`carries(line, "45")`) and `styledLine(t, out, "review required")` carrying `sgrYellow`.
- `survivors["cooldown"]`: `"retry in 97s"` → `"97s"` (at 30 columns the longer status wraps between `retry` and `in 97s`).

Update existing tests in `input_test.go`:
- `TestClickOnTabSelectsSection`: the tab-line search `strings.Contains(l, "[Overview]")` → `strings.Contains(l, "Overview")`.
- `TestCursorMovementAndPaging` and `TestWheelScrollsBody`: both set `m.Height = 24`, and the shorter header now fits the whole overview in 24 rows, so nothing scrolls. Change both to `m.Height = 14`.
- `TestOnlyThePaletteReachesTheTerminal`: extend `allowed` with background colours, after the 90–97 loop:

```go
	for n := 40; n <= 47; n++ {
		allowed[strconv.Itoa(n)] = true
	}
```

and update the comment above the test to say the badge uses the eight ANSI backgrounds.
- `TestEmptyStates`, `TestErrorsAndEmptyStatesCarryTheirStyles`: unchanged (no-PR screens still use `titleLines`, which now shows `12s ago` instead of `refreshed …` — only matters where a test asserts on the age text).

- [ ] **Step 2: Run to see them fail**

Run: `go test ./internal/ui`
Expected: build failure `undefined: compact`, then failures on the new header tests.

- [ ] **Step 3: Implement**

`checks.go`:

```go
// compact shortens a large diff count: whole thousands from 10,000 and whole
// millions from 1,000,000, always rounded down.
func compact(n int) string {
	switch {
	case n >= 1_000_000:
		return strconv.Itoa(n/1_000_000) + "M"
	case n >= 10_000:
		return strconv.Itoa(n/1_000) + "k"
	default:
		return strconv.Itoa(n)
	}
}
```

`style.go` — in `newPalette`, `activeTab: bold.Reverse(true)` → `activeTab: bold.Underline(true)`. Add:

```go
// badge renders the PR state as a padded label. Open, merged and closed sit on
// their ANSI background; draft and unknown stay faint text.
func badge(state string) string {
	base := lipgloss.NewStyle().Padding(0, 1)
	bg := map[string]string{"OPEN": "2", "MERGED": "5", "CLOSED": "1"}
	if c, ok := bg[state]; ok {
		return base.Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color(c)).Render(state)
	}
	return base.Faint(true).Render(state)
}
```

Update the `palette` doc comment: "…the sixteen ANSI colours (foreground, and background for the state badge) and the attributes every terminal implements…".

`view.go`:

1. Priorities — replace the const block:

```go
// Header priorities: 0 never drops, higher numbers are dropped first when the
// terminal is too short to show the whole header plus one body row.
const (
	prioError = iota + 1
	prioTitle
	prioCounts
	prioBlank
)
```

2. `statusSpans`: `add(pal.faint, "refreshed "+ago(now.Sub(m.Snapshot.FetchedAt))+" ago")` → `add(pal.faint, ago(now.Sub(m.Snapshot.FetchedAt))+" ago")`.

3. Replace `headerLines`:

```go
func (m *Model) headerLines(w int) []headerLine {
	s := m.Snapshot
	out := m.identityLines(w)
	add := func(prio int, text string) { out = append(out, headerLine{text: text, prio: prio}) }
	for _, l := range styleWrapped(pal.bold, clean(s.Title), w) {
		add(prioTitle, l)
	}
	for _, l := range wrapSpans(m.metaSpans(), w) {
		add(prioCounts, l)
	}
	add(prioBlank, "")
	text, spans := tabLine(m.Section)
	out = append(out, headerLine{text: text, tabs: spans})
	for _, l := range m.errorLines(w) {
		add(prioError, l)
	}
	add(prioBlank, "")
	return out
}

// identityLines is line 1: badge, number and repository on the left, the
// status on the right. The repository gives way first; a status that still
// does not fit wraps onto its own lines below, as the empty-state title does.
// Without a pull request it is the empty-state title itself.
func (m *Model) identityLines(w int) []headerLine {
	s := m.Snapshot
	if s.PR == nil {
		return m.titleLines(w)
	}
	left := badge(prState(s)) + " " + styled(pal.bold, fmt.Sprintf("#%d", s.PR.Number))
	leftW := ansi.StringWidth(left)
	status := m.statusSpans()
	statusW := ansi.StringWidth(spanText(status))
	if statusW > 0 && leftW+1+statusW > w {
		out := []headerLine{{text: ansi.Truncate(left, w, "…")}}
		for _, l := range wrapSpans(status, w) {
			out = append(out, headerLine{text: l, prio: prioError})
		}
		return out
	}
	room := w - leftW - 2
	if statusW > 0 {
		room -= statusW + 1
	}
	if repo := clean(s.PR.Repository); room >= 1 && repo != "" {
		shown := ansi.Truncate(repo, room, "…")
		left += "  " + styled(pal.faint, shown)
		leftW += 2 + ansi.StringWidth(shown)
	}
	if statusW == 0 {
		return []headerLine{{text: left}}
	}
	return []headerLine{{text: left + strings.Repeat(" ", w-leftW-statusW) + renderSpans(status)}}
}

// metaSpans is the author and the size of the change.
func (m *Model) metaSpans() []span {
	s := m.Snapshot
	return []span{
		{text: "@" + clean(s.Author), style: pal.cyan},
		{text: " · " + plural(s.Commits, "commit") + " · " + plural(s.ChangedFiles, "file") + " · ", style: pal.faint},
		{text: "+" + compact(s.Additions), style: pal.green},
		{text: " ", style: pal.none},
		{text: "-" + compact(s.Deletions), style: pal.red},
	}
}
```

4. `tabLine`: remove the bracket wrapping — replace

```go
		label, style := t.label, pal.faint
		if t.section == active {
			label, style = "["+label+"]", pal.activeTab
		}
```

with

```go
		label, style := t.label, pal.faint
		if t.section == active {
			style = pal.activeTab
		}
```

5. Delete `headRef` and `reviewDecision` (now unused; `go vet` / the compiler will flag leftovers). Keep `prState`, `titleLines`, `fitStatus`, `appName` (used by the empty state).

- [ ] **Step 4: Run**

Run: `go test ./internal/ui && go vet ./internal/ui`
Expected: PASS (golden test still skipped). `TestRenderBoundsAcrossSizes` must pass at 30 and 44 columns with heights 10 and 40 — it proves no line exceeds the width, including the badge line.

- [ ] **Step 5: Commit**

```bash
git add internal/ui
git commit -m "feat(ui): one-line identity header with state badge"
```

---

### Task 7: Spinner

**Files:**
- Modify: `internal/ui/model.go` (fields, `New`)
- Modify: `internal/ui/update.go` (`TickMsg`, `spinner.TickMsg`, `busy`)
- Modify: `internal/ui/checks.go` (`pendingGlyph`, `checkLine`)
- Modify: `internal/ui/view.go` (`checkRow` split, `sectionLead`)
- Test: `internal/ui/update_test.go`, `internal/ui/view_test.go`, `internal/ui/checks_test.go`

Design note (deviation from the spec's wording, same intent): the spinner is started from the existing 2 s `TickMsg` handler, which already returns a `tea.Batch`, instead of from the result handlers. Adding a tick to every result's command would change commands the existing tests drive through `finish()`, which does not expand batches. The spinner therefore starts within 2 s of work appearing, shows its first frame immediately, and after the first tick reschedules itself every frame while `busy()`. When nothing is pending, `spinner.TickMsg` returns no command and the spinner stops. Update the spec's Behaviour › Spinner bullet to say this in the same commit.

- [ ] **Step 1: Write the failing tests** — append to `update_test.go` (add import `"charm.land/bubbles/v2/spinner"` if not present):

```go
func TestSpinnerTicksOnlyWhileBusy(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now) // has one pending check
	if !m.busy() {
		t.Fatal("a pending check makes the pane busy")
	}
	apply(m, TickMsg{})
	if !m.spinning {
		t.Fatal("the 2s tick must start the spinner while busy")
	}
	if cmd := apply(m, spinner.TickMsg{ID: m.spin.ID()}); cmd == nil {
		t.Fatal("a spinner tick must reschedule while busy")
	}
	m.Snapshot.Checks = []model.Check{{Name: "lint", State: model.CheckPassed}}
	m.Snapshot.CheckCounts = model.CheckCounts{Passed: 1}
	if cmd := apply(m, spinner.TickMsg{ID: m.spin.ID()}); cmd != nil || m.spinning {
		t.Fatal("the spinner must stop once nothing is pending")
	}
	apply(m, TickMsg{})
	if m.spinning {
		t.Fatal("an idle 2s tick must not start the spinner")
	}
}
```

Note: `viewHarness` is in `view_test.go`, same package — usable here.

Append to `checks_test.go`:

```go
func TestRunningRowsUseTheSpinnerFrame(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 100
	frame := m.spin.View()
	plain := plainView(m)
	if !strings.Contains(plain, frame+" integration tests") || !strings.Contains(plain, frame+" 1 running") {
		t.Fatalf("running rows and heading must show spinner frame %q:\n%s", frame, plain)
	}
}
```

Update expectations that contain `◷` for running checks to use the spinner's first frame `⠋` (MiniDot frame 0):
- `checks_test.go`: `"CI  ◷ 1 running · ✗ 4 failing"` → `"CI  ⠋ 1 running · ✗ 4 failing"`, `"◷ integration tests"` → `"⠋ integration tests"`, `"CI  ◷ 1 running"` → `"CI  ⠋ 1 running"`.
- `view_test.go` `TestOverviewRendersIdentityStatisticsAndChecks`: `"◷ integration tests"` → `"⠋ integration tests"`, `"CI  ◷ 1 running · ✗ 4 failing"` → `"CI  ⠋ 1 running · ✗ 4 failing"`.
- `TestLongCheckNamesPreserveOutcomes` calls the package function `checkRow` directly — it keeps the static glyph and is unchanged.

- [ ] **Step 2: Run to see them fail**

Run: `go test ./internal/ui`
Expected: build failure `m.busy undefined` / `m.spinning undefined`.

- [ ] **Step 3: Implement**

`model.go` — import `"charm.land/bubbles/v2/spinner"` and `"charm.land/lipgloss/v2"`; add fields after `help help.Model`:

```go
	// spin animates running checks and loading sections. spinning is true
	// while a spinner tick is in flight, so only one chain ever runs.
	spin     spinner.Model
	spinning bool
```

In `New`, after the help lines:

```go
	// The spinner renders its bare frame; the span around it picks the colour.
	m.spin = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle()))
```

`update.go` — import `"charm.land/bubbles/v2/spinner"`. Add:

```go
// busy reports whether anything on screen is still in progress: a running
// check, or a discussion section being fetched.
func (m *Model) busy() bool {
	if m.Snapshot.CheckCounts.Pending > 0 {
		return true
	}
	for _, d := range m.Discussions {
		if d != nil && d.Loading {
			return true
		}
	}
	return false
}
```

In `Update`, replace the `TickMsg` case:

```go
	case TickMsg:
		cmds := []tea.Cmd{m.resolve(), tea.Tick(2*time.Second, func(time.Time) tea.Msg { return TickMsg{} })}
		if m.busy() && !m.spinning {
			m.spinning = true
			cmds = append(cmds, m.spin.Tick)
		}
		return m, tea.Batch(cmds...)
	case spinner.TickMsg:
		if !m.busy() {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(x)
		return m, cmd
```

`checks.go` — replace the two Task 5 stubs:

```go
// pendingGlyph is the spinner's current frame.
func (m *Model) pendingGlyph() string { return m.spin.View() }

// checkLine is a check row whose running glyph is the spinner frame.
func (m *Model) checkLine(c model.Check, w int) string {
	if c.State == model.CheckPending {
		return checkRowGlyph(c, w, m.spin.View())
	}
	return checkRow(c, w)
}
```

`view.go` — split `checkRow` so the glyph can be injected; keep `checkRow`'s signature (tests call it):

```go
func checkRow(c model.Check, w int) string { return checkRowGlyph(c, w, checkGlyph(c.State)) }

func checkRowGlyph(c model.Check, w int, glyph string) string {
	left := glyph + " " + strings.ReplaceAll(clean(c.Name), "\n", " ")
	// … rest of the old checkRow body unchanged, starting at `right := …`
}
```

In `sectionLead`, `out = append(out, styled(pal.faint, "Loading…"))` → `out = append(out, styled(pal.faint, m.spin.View()+" Loading…"))`. Update any test asserting the discussion lead `"Loading…"` exactly at line start — `strings.Contains(…, "Loading…")` checks still pass.

- [ ] **Step 4: Run**

Run: `go test ./internal/ui && go test ./...`
Expected: PASS. If an `update_test.go` test that sends `TickMsg` now sees an extra batched command, it is because its fixture has pending checks or a loading section; adjust that test's expectation, not the implementation.

- [ ] **Step 5: Commit** (include the spec wording change)

```bash
git add internal/ui docs/superpowers/specs/2026-09-30-glance-pane-redesign-design.md
git commit -m "feat(ui): spinner for running checks and loading sections"
```

---

### Task 8: Goldens, narrow widths, full check, manual run

**Files:**
- Modify: `internal/ui/view_test.go` (`goldenOverview44`, `goldenReviews44`, remove the `t.Skip`)

- [ ] **Step 1: Regenerate goldens.** Remove the `t.Skip` line from `TestColorLeavesTheLayoutUnchanged`. Run:

`go test ./internal/ui -run TestColorLeavesTheLayoutUnchanged`

It prints `--- got ---` for each fixture. **Read the got output against the spec's Layout section** before pasting: line 1 badge/number/repo/age; wrapped bold title; meta line; blank; tab bar without brackets; `CI` heading; bar; four `×` rows; running row with `⠋`; fold row `▸ ✓ 2 passed · 1 neutral · 1 skipped · 1 unknown` (truncated with `…` at 44 if needed); blank; `Review  changes requested`; footer. For reviews: same header, Reviews tab active, body unchanged from before. If anything differs from the spec, fix the code, not the golden. Then paste each got block into its constant.

- [ ] **Step 2: Narrow-width check.** Add to `view_test.go`:

```go
func TestBarHiddenInNarrowPanes(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 23
	if strings.Contains(plainView(m), "━") {
		t.Fatal("no bar below 24 columns")
	}
	m.Width = 24
	if !strings.Contains(plainView(m), "━") {
		t.Fatal("bar from 24 columns")
	}
}
```

Update `survivors["overview"]` to `{"Re-request", "Overview", "unit tests", "CI  "}`.

- [ ] **Step 3: Full check**

Run: `go test ./... && go vet ./... && gofmt -l internal cmd`
Expected: all tests pass, vet silent, gofmt prints nothing.

- [ ] **Step 4: Commit**

```bash
git add internal/ui
git commit -m "test(ui): goldens and narrow-pane bar for the redesigned pane"
```

- [ ] **Step 5: Manual run in Herdr** (needs the user's screen; report and ask for a screenshot)

```bash
go build -o bin/herdr-pr-glance ./cmd/herdr-pr-glance
herdr plugin uninstall glance.pr   # the GitHub install; ask the user first
herdr plugin link .
```

Then ask the user to press ⌘G in a tab whose branch has a PR and send a screenshot. Compare against the spec layout. Afterwards, restoring the release install is `herdr plugin unlink glance.pr && herdr plugin install cupsadarius/herdr-pr-glance`.
