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
// exist while the pull request belongs to a stack. Unpin also stays on while
// anything is pinned, so a pin can always be undone.
func (m *Model) bindings() keyMap {
	k := defaultKeys
	stacked := m.Snapshot.Stack != nil
	k.Next.SetEnabled(stacked)
	k.Prev.SetEnabled(stacked)
	k.Unpin.SetEnabled(stacked || m.Pinned != nil)
	return k
}

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
