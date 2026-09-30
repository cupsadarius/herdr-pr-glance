package ui

import (
	"strings"
	"testing"

	bkey "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
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
	for _, k := range []string{"j", "r", "1", "enter", "]", "pgdown"} {
		if cmd := apply(m, key(k)); cmd != nil || !m.ShowHelp || m.Cursor != 2 || m.Offset != 0 {
			t.Fatalf("%q must be ignored while help is open", k)
		}
	}
	if cmd := apply(m, key("esc")); cmd != nil {
		t.Fatal("esc must not return a command")
	}
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
	if cmd := apply(m, key("ctrl+c")); cmd == nil {
		t.Fatal("ctrl+c must still quit while help is open")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c must return tea.Quit")
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
	if y < 0 {
		t.Fatal("tab row not found")
	}
	cmd := apply(m, click(x, y))
	if m.ShowHelp || cmd == nil {
		t.Fatal("a tab click must close help and select the tab")
	}
	if msg, ok := cmd().(SelectSectionMsg); !ok || model.Section(msg) != model.Comments {
		t.Fatalf("tab click produced %#v", msg)
	}
}

func TestHelpShowsInEmptyStates(t *testing.T) {
	m, _ := viewHarness()
	apply(m, key("?"))
	plain := plainView(m)
	for _, want := range []string{"refresh", "page down"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("help misses %q in the empty state:\n%s", want, plain)
		}
	}
	apply(m, key("esc"))
	if plain := plainView(m); !strings.Contains(plain, "Loading…") {
		t.Fatalf("closing help must show the empty message again:\n%s", plain)
	}
}

func TestHelpKeepsTabsAndFitsHeight(t *testing.T) {
	m, now := viewHarness()
	stackFixture(m, *now)
	m.Width, m.Height = 44, 24
	m.ShowHelp = true
	plain := plainView(m)
	for _, want := range []string{"stack: own PR", "zoom"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("help misses %q:\n%s", want, plain)
		}
	}
	tabRow := false
	for _, l := range strings.Split(plain, "\n") {
		if strings.Contains(l, "Overview") && strings.Contains(l, "Reviews") {
			tabRow = true
		}
	}
	if !tabRow {
		t.Fatalf("tab line missing:\n%s", plain)
	}
	assertBounds(t, m.View().Content, 44, 24)
}

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
