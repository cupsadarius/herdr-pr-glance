package ui

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

// TickMsg schedules local source resolution. Only its accepted result can
// trigger automatic summary polling; discussion requests never follow a tick.
type TickMsg struct{}

// SelectSectionMsg opens a section and applies its cache policy.
type SelectSectionMsg model.Section

// RefreshMsg refreshes the active section, respecting in-flight work and cooldown.
type RefreshMsg struct{}
type SourceResult struct {
	Generation uint64
	Source     model.Source
	Err        error
}
type SummaryResult struct {
	Generation uint64
	Data       model.Snapshot
	Err        error
}
type CacheResult struct {
	Generation uint64
	Section    model.Section
	Entry      model.CacheEntry
	Hit        bool
	Err        error
}
type DiscussionResult struct {
	Generation uint64
	Section    model.Section
	Data       model.Discussion
	Err        error
}

func (m *Model) resolve() tea.Cmd {
	if m.resolving || m.resolver == nil {
		return nil
	}
	m.resolving = true
	r, gen := m.resolver, m.Generation
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s, e := r.Resolve(ctx)
		return SourceResult{gen, s, e}
	}
}
func (m *Model) summary(manual bool) tea.Cmd {
	now := m.now()
	if m.SummaryLoading || m.github == nil || !m.Source.Visible || m.Source.EmptyReason != "" || m.Source.Branch == "" || now.Before(m.CooldownUntil) || (!manual && now.Before(m.NextSummary)) {
		return nil
	}
	m.SummaryLoading = true
	g, s, gen, parent := m.github, m.Source, m.Generation, m.ctx
	if m.Pinned != nil {
		pin := *m.Pinned
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(parent, 30*time.Second)
			defer cancel()
			d, e := g.SnapshotPR(ctx, s, pin)
			return SummaryResult{gen, d, e}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		d, e := g.Snapshot(ctx, s)
		return SummaryResult{gen, d, e}
	}
}
func (m *Model) discussion(s model.Section, readCache bool) tea.Cmd {
	if m.Snapshot.PR == nil || s == model.Overview {
		return nil
	}
	d := m.Discussions[s]
	if d == nil {
		d = &DiscussionState{}
		m.Discussions[s] = d
	}
	if d.Loading {
		if !readCache && d.cacheLoading {
			d.refreshPending = true
		}
		return nil
	}
	if readCache && d.Data != nil && !m.DiscussionStale(s) {
		return nil
	}
	pr, gen, cache, now := *m.Snapshot.PR, m.Generation, m.cache, m.now()
	if readCache && d.Data == nil && cache != nil {
		d.Loading = true
		d.cacheLoading = true
		return func() tea.Msg { e, hit, err := cache.Get(pr, s, now); return CacheResult{gen, s, e, hit, err} }
	}
	if m.github == nil || !m.Source.Visible || now.Before(m.CooldownUntil) {
		return nil
	}
	d.Loading = true
	g, parent, clock := m.github, m.ctx, m.now
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
		defer cancel()
		data, err := g.Discussion(ctx, pr, s)
		if err == nil && !data.Complete {
			err = errors.New("incomplete discussion pagination")
		}
		if err == nil {
			data.FetchedAt = clock()
		}
		return DiscussionResult{gen, s, data, err}
	}
}
