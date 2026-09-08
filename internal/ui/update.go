package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

func sameSource(a, b model.Source) bool { a.Visible = false; b.Visible = false; return a == b }
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
		if !sameSource(m.Source, x.Source) {
			m.reset()
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
		}
	case CacheResult:
		if x.Generation != m.Generation {
			return m, nil
		}
		d := m.Discussions[x.Section]
		if d == nil {
			return m, nil
		}
		d.Loading = false
		d.Warning = x.Err
		if x.Hit && x.Entry.Data.Complete {
			data := x.Entry.Data
			data.FetchedAt = x.Entry.FetchedAt
			d.Data = &data
		}
		if !m.DiscussionStale(x.Section) {
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
		if x.Warning != nil {
			d.Warning = x.Warning
		}
		if x.Err != nil {
			m.rateLimit(x.Err)
			return m, nil
		}
		d.Data = &x.Data
	case SelectSectionMsg:
		s := model.Section(x)
		if s != model.Overview && s != model.Comments && s != model.Reviews {
			return m, nil
		}
		if s == m.Section {
			return m, nil
		}
		m.Section = s
		return m, m.discussion(s, true)
	case RefreshMsg:
		if m.Section == model.Overview {
			return m, m.summary(true)
		}
		return m, m.discussion(m.Section, false)
	}
	return m, nil
}
