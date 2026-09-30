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
