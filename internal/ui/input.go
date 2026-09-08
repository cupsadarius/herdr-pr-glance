package ui

import (
	"net/url"

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
	switch k.String() {
	case "q", "esc", "ctrl+c":
		m.cancel()
		return tea.Quit
	case "1":
		return selectSection(model.Overview)
	case "2":
		return selectSection(model.Comments)
	case "3":
		return selectSection(model.Reviews)
	case "r":
		return func() tea.Msg { return RefreshMsg{} }
	case "o":
		return m.openSelected()
	case "z":
		return runAction(m.Zoom)
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "pgdown":
		m.scroll(m.bodyHeight())
	case "pgup":
		m.scroll(-m.bodyHeight())
	case "enter":
		m.toggleThread()
	}
	return nil
}

// handleMouse resolves a click against the row map recorded by the last render.
func (m *Model) handleMouse(e tea.Mouse) tea.Cmd {
	switch e.Button {
	case tea.MouseWheelUp:
		m.scroll(-wheelStep)
		return nil
	case tea.MouseWheelDown:
		m.scroll(wheelStep)
		return nil
	case tea.MouseLeft:
	default:
		return nil
	}
	for _, r := range m.rows {
		if r.y != e.Y || (r.x1 >= 0 && (e.X < r.x0 || e.X >= r.x1)) {
			continue
		}
		if r.tab != "" {
			return selectSection(r.tab)
		}
		if r.item >= 0 {
			m.Cursor = r.item
			m.toggleThread()
			m.clamp()
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
	raw := m.selectedURL()
	if raw == "" {
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
	if it, ok := m.selected(); ok && it.url != "" {
		return it.url
	}
	if m.Snapshot.PR != nil {
		return m.Snapshot.PR.URL
	}
	return ""
}

func (m *Model) selected() (bodyItem, bool) {
	_, items := m.body(m.contentWidth())
	if m.Cursor < 0 || m.Cursor >= len(items) {
		return bodyItem{}, false
	}
	return items[m.Cursor], true
}

func (m *Model) toggleThread() {
	it, ok := m.selected()
	if !ok || it.threadID == "" {
		return
	}
	if m.Expanded == nil {
		m.Expanded = map[string]bool{}
	}
	m.Expanded[it.threadID] = !m.Expanded[it.threadID]
	m.clamp()
}

func (m *Model) moveCursor(delta int) {
	m.Cursor += delta
	m.clamp()
}

// scroll moves the viewport and drags the cursor along with it, so paging and
// the wheel are not fought by the rule that keeps the cursor visible.
func (m *Model) scroll(delta int) {
	lines, spans := renderBody(m.body(m.contentWidth()))
	high := m.bodyHeight()
	off := m.Offset + delta
	if limit := len(lines) - high; off > limit {
		off = limit
	}
	if off < 0 {
		off = 0
	}
	m.Offset = off
	if m.Cursor < 0 || m.Cursor >= len(spans) {
		return
	}
	if spans[m.Cursor][1] > off && spans[m.Cursor][0] < off+high {
		return
	}
	for i, s := range spans {
		if s[1] > off && s[0] < off+high {
			m.Cursor = i
			return
		}
	}
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
// with the cursor visible. Update calls it after navigation, resize and every
// change of the data being displayed.
func (m *Model) clamp() {
	lead, items := m.body(m.contentWidth())
	if m.Cursor >= len(items) {
		m.Cursor = len(items) - 1
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	lines, spans := renderBody(lead, items)
	m.Offset = clampOffset(m.Offset, len(lines), m.bodyHeight(), spans, m.Cursor)
}
