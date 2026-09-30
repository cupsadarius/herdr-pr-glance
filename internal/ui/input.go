package ui

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	bkey "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

// ActionErrMsg carries the outcome of a host action (open or zoom) back into
// the Update loop, which owns every state change.
type ActionErrMsg struct{ Err error }

const wheelStep = 3

func selectSection(s model.Section) tea.Cmd {
	return func() tea.Msg { return SelectSectionMsg(s) }
}

// handleKey maps a key press to a command, mutating only navigation state.
func (m *Model) handleKey(k tea.KeyPressMsg) tea.Cmd {
	keys := m.bindings()
	if m.ShowHelp {
		switch {
		case bkey.Matches(k, keys.Help), k.String() == "esc":
			m.ShowHelp = false
		case bkey.Matches(k, keys.Quit):
			m.cancel()
			return tea.Quit
		}
		return nil
	}
	switch {
	case bkey.Matches(k, keys.Quit):
		m.cancel()
		return tea.Quit
	case bkey.Matches(k, keys.Help):
		m.ShowHelp = true
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

// stackStep pins the neighbouring entry of the stack: one position up is the
// pull request built on the shown one, one position down is the one it is
// built on. At either end there is nothing to move to.
func (m *Model) stackStep(delta int) tea.Cmd {
	if m.Snapshot.Stack == nil {
		return nil
	}
	want := m.Snapshot.StackPosition + delta
	for _, e := range m.Snapshot.Stack.Entries {
		if e.Position == want {
			return m.pinPR(e.PR)
		}
	}
	return nil
}

// pinPR shows another pull request of the stack instead of the branch's own,
// and refreshes immediately; the shown one is already pinned enough, and the
// branch's own is what unpinning means.
func (m *Model) pinPR(pr model.PR) tea.Cmd {
	if m.Snapshot.PR != nil && *m.Snapshot.PR == pr {
		return nil
	}
	if m.branchPR != nil && *m.branchPR == pr {
		return m.unpin()
	}
	if m.Pinned == nil && m.Snapshot.PR != nil {
		own := *m.Snapshot.PR
		m.branchPR = &own
	}
	pin := pr
	m.Pinned = &pin
	return m.repin()
}

// unpin returns to the pull request of the working branch.
func (m *Model) unpin() tea.Cmd {
	if m.Pinned == nil {
		return nil
	}
	m.Pinned, m.branchPR = nil, nil
	return m.repin()
}

// repin refreshes after the reader changed which pull request is shown. The
// work in flight is for the previous one, and clearing the schedule makes the
// next tick fetch even when this call cannot: a summary already running, or a
// rate-limit cooldown, must not leave a pinned header over branch data.
func (m *Model) repin() tea.Cmd {
	m.switchPR()
	m.NextSummary = time.Time{}
	return m.summary(true)
}

// activate is what enter and a click do to the selected item: pin a stack
// entry, toggle the CI fold row, or expand a review thread.
func (m *Model) activate() tea.Cmd {
	it, ok := m.selected()
	if !ok {
		return nil
	}
	if it.pin != nil {
		return m.pinPR(*it.pin)
	}
	if it.fold {
		if m.Expanded == nil {
			m.Expanded = map[string]bool{}
		}
		m.Expanded[foldKey] = !m.Expanded[foldKey]
		m.clampReveal()
		return nil
	}
	m.toggleThread(it)
	return nil
}

// handleMouse resolves a click against the layout derived from current state.
func (m *Model) handleMouse(e tea.Mouse) tea.Cmd {
	switch e.Button {
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
	case tea.MouseLeft:
	default:
		return nil
	}
	_, rows := m.layoutView()
	for _, r := range rows {
		if r.y != e.Y || (r.x1 >= 0 && (e.X < r.x0 || e.X >= r.x1)) {
			continue
		}
		if r.url != "" {
			return m.openURL(r.url)
		}
		if r.tab != "" {
			m.ShowHelp = false
			return selectSection(r.tab)
		}
		if r.item >= 0 {
			m.Cursor = r.item
			cmd := m.activate()
			m.clamp()
			return cmd
		}
		return nil
	}
	return nil
}

func runAction(f func() error) tea.Cmd {
	if f == nil {
		return nil
	}
	return func() tea.Msg { return ActionErrMsg{Err: f()} }
}

// openSelected opens the selected item's URL, falling back to the pull request
// itself only when the item carries none. Non-web URLs are ignored outright.
func (m *Model) openSelected() tea.Cmd {
	if m.Open == nil {
		return nil
	}
	return m.openURL(m.selectedURL())
}

// openURL opens a web URL through the host; anything else is ignored.
func (m *Model) openURL(raw string) tea.Cmd {
	if m.Open == nil || raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil
	}
	open := m.Open
	return func() tea.Msg { return ActionErrMsg{Err: open(raw)} }
}

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

// anchor is a key that names an item across snapshots, so a refresh that
// reorders the body keeps the selection on the same thing. Empty means none.
func anchor(it bodyItem) string {
	switch {
	case it.fold:
		return "fold"
	case it.pin != nil:
		return "pr:" + it.pin.Repository + "#" + strconv.Itoa(it.pin.Number)
	}
	return it.url
}

// reanchor moves the cursor to the item with the given anchor, if the body
// still has one. A check that is gone has usually passed and folded away, so
// the selection falls back to the fold row: the bare index could otherwise
// land on a stack row, and enter would pin a different pull request.
func (m *Model) reanchor(want string) {
	if want == "" {
		return
	}
	_, items, _ := m.body(m.contentWidth())
	fold := -1
	for i, it := range items {
		if anchor(it) == want {
			m.Cursor = i
			return
		}
		if it.fold {
			fold = i
		}
	}
	if fold >= 0 && want != "fold" && !strings.HasPrefix(want, "pr:") {
		m.Cursor = fold
	}
}

func (m *Model) selected() (bodyItem, bool) {
	_, items, _ := m.body(m.contentWidth())
	if m.Cursor < 0 || m.Cursor >= len(items) {
		return bodyItem{}, false
	}
	return items[m.Cursor], true
}

func (m *Model) toggleThread(it bodyItem) {
	if it.threadID == "" {
		return
	}
	if m.Expanded == nil {
		m.Expanded = map[string]bool{}
	}
	m.Expanded[it.threadID] = !m.Expanded[it.threadID]
	m.clampReveal()
}

func (m *Model) moveCursor(delta int) {
	m.Cursor += delta
	m.clampReveal()
}

// scroll moves the viewport by rows and leaves the cursor where it is, so the
// rest of an item taller than the body can be read without losing the
// selection. The next cursor move brings the selected item back into view.
func (m *Model) scroll(delta int) {
	lines, _ := renderBody(m.body(m.contentWidth()))
	m.Offset = clampOffset(m.Offset+delta, len(lines), m.bodyHeight())
}

// bodyHeight is the number of body rows the current window leaves.
func (m *Model) bodyHeight() int {
	_, h := m.size()
	high := h - len(fitHeader(m.headerLines(m.contentWidth()+1), h-2)) - 1
	if high < 1 {
		return 1
	}
	return high
}

// clamp keeps the cursor inside the item list and the offset inside the body,
// leaving the reader where they scrolled to. Update calls it after a resize and
// after every change of the data being displayed: neither is a reason to move
// the viewport.
func (m *Model) clamp() {
	lines, _ := m.fit()
	m.Offset = clampOffset(m.Offset, lines, m.bodyHeight())
}

// clampReveal also scrolls the selected item's first line into view. Only the
// inputs that move the selection use it.
func (m *Model) clampReveal() {
	lines, spans := m.fit()
	m.Offset = revealCursor(m.Offset, lines, m.bodyHeight(), spans, m.Cursor)
}

// fit bounds the cursor and reports the body's line count and item spans.
func (m *Model) fit() (int, [][2]int) {
	lead, items, tail := m.body(m.contentWidth())
	if m.Cursor >= len(items) {
		m.Cursor = len(items) - 1
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	lines, spans := renderBody(lead, items, tail)
	return len(lines), spans
}
