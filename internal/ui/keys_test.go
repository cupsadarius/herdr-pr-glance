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
