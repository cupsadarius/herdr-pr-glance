package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

// sourceIdentity is what makes the tracked pull request a different one. The
// working pane, its foreground directory and its HEAD commit are deliberately
// excluded: focusing a sibling pane in an editor-and-shell layout, moving
// around the checkout, or committing to the branch, all describe one source.
type sourceIdentity struct {
	workspace, tab, root, branch string
	empty                        model.EmptyReason
}

func identify(s model.Source) sourceIdentity {
	return sourceIdentity{s.WorkspaceID, s.TabID, s.Root, s.Branch, s.EmptyReason}
}
func (m *Model) reset() {
	m.cancel()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.Generation++
	m.Snapshot = model.Snapshot{}
	m.Section = model.Overview
	m.Discussions = map[model.Section]*DiscussionState{}
	m.SummaryLoading = false
	m.SummaryError = nil
	m.NextSummary = time.Time{}
	m.failures = 0
	m.Cursor, m.Offset = 0, 0
	m.Expanded = map[string]bool{}
}
func (m *Model) rateLimit(err error) {
	var e *model.FetchError
	if errors.As(err, &e) && e.Kind == model.RateLimitError {
		until := e.RetryAt
		if until.IsZero() || !until.After(m.now()) {
			until = m.now().Add(5 * time.Minute)
		}
		if until.After(m.CooldownUntil) {
			m.CooldownUntil = until
		}
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height = x.Width, x.Height
		m.clamp()
	case tea.KeyPressMsg:
		return m, m.handleKey(x)
	case tea.MouseClickMsg:
		return m, m.handleMouse(x.Mouse())
	case tea.MouseWheelMsg:
		return m, m.handleMouse(x.Mouse())
	case ActionErrMsg:
		m.ActionError = x.Err
		m.clamp()
	case TickMsg:
		return m, tea.Batch(m.resolve(), tea.Tick(2*time.Second, func(time.Time) tea.Msg { return TickMsg{} }))
	case SourceResult:
		m.resolving = false
		if x.Generation != m.Generation {
			return m, nil
		}
		m.SourceError = x.Err
		if x.Err != nil {
			return m, nil
		}
		switch {
		case identify(m.Source) != identify(x.Source):
			m.reset()
		case m.Source.HEAD != x.Source.HEAD:
			// A commit on the same branch belongs to the same pull request, so
			// the section, its discussions and the viewport all stay; only the
			// counts and checks are out of date. Due now, and the generation is
			// untouched so in-flight discussion results still apply.
			m.NextSummary = time.Time{}
		}
		m.Source = x.Source
		return m, m.summary(false)
	case SummaryResult:
		if x.Generation != m.Generation {
			return m, nil
		}
		m.SummaryLoading = false
		m.SummaryError = x.Err
		if x.Err != nil {
			m.rateLimit(x.Err)
			m.failures++
			delay := time.Minute
			for i := 1; i < m.failures && delay < 5*time.Minute; i++ {
				delay *= 2
			}
			if delay > 5*time.Minute {
				delay = 5 * time.Minute
			}
			m.NextSummary = m.now().Add(delay)
			return m, nil
		}
		old := m.Snapshot.PR
		m.Snapshot = x.Data
		m.Snapshot.FetchedAt = m.now()
		m.failures = 0
		m.NextSummary = m.now().Add(time.Minute)
		if (old == nil) != (x.Data.PR == nil) || (old != nil && x.Data.PR != nil && *old != *x.Data.PR) {
			m.cancel()
			m.ctx, m.cancel = context.WithCancel(context.Background())
			m.Generation++
			m.Discussions = map[model.Section]*DiscussionState{}
			m.Section = model.Overview
			m.Cursor, m.Offset = 0, 0
			m.Expanded = map[string]bool{}
		}
		m.clamp()
	case CacheResult:
		if x.Generation != m.Generation {
			return m, nil
		}
		d := m.Discussions[x.Section]
		if d == nil {
			return m, nil
		}
		d.Loading = false
		d.cacheLoading = false
		refresh := d.refreshPending
		d.refreshPending = false
		d.Warning = x.Err
		if x.Hit && x.Entry.Data.Complete {
			data := x.Entry.Data
			data.FetchedAt = x.Entry.FetchedAt
			d.Data = &data
		}
		m.clamp()
		if !refresh && !m.DiscussionStale(x.Section) {
			return m, nil
		}
		return m, m.discussion(x.Section, false)
	case DiscussionResult:
		if x.Generation != m.Generation {
			return m, nil
		}
		d := m.Discussions[x.Section]
		if d == nil {
			return m, nil
		}
		d.Loading = false
		d.Error = x.Err
		if x.Err != nil {
			m.rateLimit(x.Err)
			return m, nil
		}
		d.Data = &x.Data
		m.clamp()
		// Serialize persistence with source acceptance. A command may finish after
		// cancellation, so writing before this generation guard can corrupt a newer
		// cache entry even when the visible result is subsequently discarded.
		if m.cache != nil {
			if warning := m.cache.Put(x.Data.PR, x.Section, x.Data, x.Data.FetchedAt); warning != nil {
				d.Warning = warning
			}
		}
	case SelectSectionMsg:
		s := model.Section(x)
		if s != model.Overview && s != model.Comments && s != model.Reviews {
			return m, nil
		}
		if s == m.Section {
			return m, nil
		}
		m.Section = s
		m.Cursor, m.Offset = 0, 0
		return m, m.discussion(s, true)
	case RefreshMsg:
		if m.Section == model.Overview {
			return m, m.summary(true)
		}
		return m, m.discussion(m.Section, false)
	}
	return m, nil
}
