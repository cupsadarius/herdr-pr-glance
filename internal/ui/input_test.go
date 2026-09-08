package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}
func wheel(y int, up bool) tea.MouseWheelMsg {
	b := tea.MouseWheelDown
	if up {
		b = tea.MouseWheelUp
	}
	return tea.MouseWheelMsg{X: 0, Y: y, Button: b}
}

func TestSectionKeysEmitSelectSection(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	for k, want := range map[string]model.Section{"1": model.Overview, "2": model.Comments, "3": model.Reviews} {
		cmd := apply(m, key(k))
		if cmd == nil {
			t.Fatalf("key %q produced no command", k)
		}
		msg, ok := cmd().(SelectSectionMsg)
		if !ok || model.Section(msg) != want {
			t.Fatalf("key %q produced %#v", k, msg)
		}
	}
}

func TestRefreshKeyEmitsRefreshMsg(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	cmd := apply(m, key("r"))
	if cmd == nil {
		t.Fatal("r produced no command")
	}
	if _, ok := cmd().(RefreshMsg); !ok {
		t.Fatalf("r produced %T", cmd())
	}
}

func TestCursorMovementAndPaging(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Height = 24
	m.View()
	apply(m, key("j"))
	apply(m, key("down"))
	if m.Cursor != 2 {
		t.Fatalf("cursor %d after two downs", m.Cursor)
	}
	apply(m, key("k"))
	if m.Cursor != 1 {
		t.Fatalf("cursor %d after up", m.Cursor)
	}
	for i := 0; i < 50; i++ {
		apply(m, key("j"))
	}
	if m.Cursor != len(m.Snapshot.Checks)-1 {
		t.Fatalf("cursor must clamp to the last item, got %d", m.Cursor)
	}
	for i := 0; i < 50; i++ {
		apply(m, key("up"))
	}
	if m.Cursor != 0 || !strings.Contains(m.View().Content, "× unit tests") {
		t.Fatalf("cursor %d offset %d must return to the first item", m.Cursor, m.Offset)
	}
	apply(m, key("pgdown"))
	if m.Offset == 0 {
		t.Fatal("pgdown did not scroll")
	}
	apply(m, key("pgup"))
	if m.Offset != 0 {
		t.Fatalf("pgup did not return to the top, offset %d", m.Offset)
	}
}

func TestOpenSelectedURL(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	var opened []string
	m.Open = func(u string) error { opened = append(opened, u); return nil }

	m.View()
	finish(m, apply(m, key("o")))
	if len(opened) != 1 || opened[0] != "https://ci.example.com/unit" {
		t.Fatalf("expected the selected check URL, got %v", opened)
	}

	m.Snapshot.Checks[0].URL = "javascript:alert(1)"
	finish(m, apply(m, key("o")))
	if len(opened) != 1 {
		t.Fatalf("non-http URL must be ignored, got %v", opened)
	}

	m.Snapshot.Checks = nil
	m.Cursor = 0
	finish(m, apply(m, key("o")))
	if len(opened) != 2 || opened[1] != m.Snapshot.PR.URL {
		t.Fatalf("expected fallback to the PR URL, got %v", opened)
	}
}

func TestOpenAndZoomHooksReportErrors(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	if cmd := apply(m, key("z")); cmd != nil {
		t.Fatal("nil zoom hook must be a no-op")
	}
	boom := errors.New("herdr refused")
	m.Zoom = func() error { return boom }
	cmd := apply(m, key("z"))
	if cmd == nil {
		t.Fatal("zoom produced no command")
	}
	msg, ok := cmd().(ActionErrMsg)
	if !ok || !errors.Is(msg.Err, boom) {
		t.Fatalf("zoom produced %#v", msg)
	}
	apply(m, msg)
	if !strings.Contains(m.View().Content, "herdr refused") {
		t.Fatalf("action error not surfaced:\n%s", m.View().Content)
	}
}

func TestQuitKeysCancelContextAndQuit(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		m, now := viewHarness()
		overviewFixture(m, *now)
		cmd := apply(m, key(k))
		if cmd == nil {
			t.Fatalf("%s produced no command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s produced %T", k, cmd())
		}
		if m.ctx.Err() == nil {
			t.Fatalf("%s did not cancel the model context", k)
		}
	}
}

func TestClickOnTabSelectsSection(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 100
	content := m.View().Content
	lines := strings.Split(content, "\n")
	y, x := -1, -1
	for i, l := range lines {
		if j := strings.Index(l, "Comments"); j >= 0 && strings.Contains(l, "[Overview]") {
			y, x = i, j+2
		}
	}
	if y < 0 {
		t.Fatalf("no tab line in:\n%s", content)
	}
	cmd := apply(m, click(x, y))
	if cmd == nil {
		t.Fatal("clicking a tab produced no command")
	}
	msg, ok := cmd().(SelectSectionMsg)
	if !ok || model.Section(msg) != model.Comments {
		t.Fatalf("click produced %#v", msg)
	}
	if apply(m, click(x, y+1)) != nil {
		t.Fatal("clicking below the tabs must not select a section")
	}
}

func TestWheelScrollsBody(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Height = 24
	m.View()
	apply(m, wheel(8, false))
	if m.Offset == 0 {
		t.Fatal("wheel down did not scroll")
	}
	apply(m, wheel(8, true))
	apply(m, wheel(8, true))
	if m.Offset != 0 {
		t.Fatalf("wheel up must clamp at the top, offset %d", m.Offset)
	}
}

func TestClickOnThreadRowSelectsAndToggles(t *testing.T) {
	m, now := viewHarness()
	reviewsFixture(m, *now)
	m.Width, m.Height = 60, 40
	content := m.View().Content
	y := -1
	for i, l := range strings.Split(content, "\n") {
		if strings.Contains(l, "▸") && strings.Contains(l, "handler.go") {
			y = i
		}
	}
	if y < 0 {
		t.Fatalf("no collapsed thread row in:\n%s", content)
	}
	apply(m, click(3, y))
	if m.Cursor != 1 || !m.Expanded["t1"] {
		t.Fatalf("click must select and expand the thread (cursor=%d expanded=%v)", m.Cursor, m.Expanded)
	}
	m.View()
	apply(m, click(3, y))
	if m.Expanded["t1"] {
		t.Fatal("second click must collapse the thread")
	}
}

func TestWindowResizeClampsScroll(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Height = 24
	m.View()
	apply(m, key("pgdown"))
	apply(m, tea.WindowSizeMsg{Width: 44, Height: 60})
	if m.Width != 44 || m.Height != 60 {
		t.Fatalf("size not recorded: %dx%d", m.Width, m.Height)
	}
	if m.Offset != 0 {
		t.Fatalf("growing the window must clamp the offset, got %d", m.Offset)
	}
}

func TestScrollOffsetSurvivesRefreshesAndClicks(t *testing.T) {
	m := tallCommentsModel(t)
	apply(m, key("pgdown"))
	apply(m, key("pgdown"))
	off := m.Offset
	if off == 0 {
		t.Fatal("fixture did not scroll")
	}
	data := *m.Discussions[model.Comments].Data
	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{"summary poll", SummaryResult{Generation: m.Generation, Data: m.Snapshot}},
		{"same size resize", tea.WindowSizeMsg{Width: m.Width, Height: m.Height}},
		{"successful action", ActionErrMsg{}},
		{"discussion refresh", DiscussionResult{Generation: m.Generation, Section: model.Comments, Data: data}},
	} {
		apply(m, tc.msg)
		if m.Offset != off {
			t.Fatalf("%s moved the offset from %d to %d", tc.name, off, m.Offset)
		}
	}
	content := m.View().Content
	y := -1
	for i, l := range strings.Split(content, "\n") {
		if strings.Contains(l, "body line ") {
			y = i
			break
		}
	}
	if y < 0 {
		t.Fatalf("no body row to click:\n%s", content)
	}
	apply(m, click(3, y))
	if m.Offset != off {
		t.Fatalf("clicking inside a tall item moved the offset from %d to %d", off, m.Offset)
	}
	if m.Cursor != 0 {
		t.Fatalf("click selected item %d, want the tall first item", m.Cursor)
	}
}

func TestMouseReplayDoesNotDependOnRendering(t *testing.T) {
	for _, scenario := range []string{"initial", "resize", "scroll", "expansion"} {
		t.Run(scenario, func(t *testing.T) {
			a, now := viewHarness()
			reviewsFixture(a, *now)
			b, _ := viewHarness()
			reviewsFixture(b, *now)
			if scenario != "initial" {
				a.View()
				b.View()
			}
			for _, m := range []*Model{a, b} {
				switch scenario {
				case "resize":
					apply(m, tea.WindowSizeMsg{Width: 30, Height: 40})
				case "scroll":
					apply(m, tea.WindowSizeMsg{Width: 44, Height: 24})
					apply(m, wheel(0, false))
				case "expansion":
					apply(m, key("j"))
					apply(m, key("enter"))
				}
			}
			y := -1
			for i, line := range strings.Split(a.View().Content, "\n") {
				if strings.Contains(line, "view.go") {
					y = i
					break
				}
			}
			if y < 0 {
				t.Fatal("second thread is not visible")
			}
			apply(a, click(3, y))
			apply(b, click(3, y))
			if a.Cursor != 2 || !a.Expanded["t2"] {
				t.Fatalf("rendered click missed second thread: %d %v", a.Cursor, a.Expanded)
			}
			if b.Cursor != a.Cursor || b.Expanded["t2"] != a.Expanded["t2"] {
				t.Fatalf("click without View differs: cursor=%d expanded=%v", b.Cursor, b.Expanded)
			}
		})
	}
}
