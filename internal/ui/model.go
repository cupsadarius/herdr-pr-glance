// Package ui owns the interactive state and asynchronous refresh policy.
package ui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

type SourceResolver interface {
	Resolve(context.Context) (model.Source, error)
}
type GitHub interface {
	Snapshot(context.Context, model.Source) (model.Snapshot, error)
	Discussion(context.Context, model.PR, model.Section) (model.Discussion, error)
}
type Cache interface {
	Get(model.PR, model.Section, time.Time) (model.CacheEntry, bool, error)
	Put(model.PR, model.Section, model.Discussion, time.Time) error
}
type DiscussionState struct {
	Data           *model.Discussion
	Loading        bool
	Error, Warning error
	cacheLoading   bool
	refreshPending bool
}

// Model is owned by Bubble Tea's Update loop. Commands capture service inputs
// and return results; they never read or mutate this state while running.
type Model struct {
	Source                     model.Source
	Snapshot                   model.Snapshot
	Section                    model.Section
	Generation                 uint64
	Discussions                map[model.Section]*DiscussionState
	SummaryLoading             bool
	SummaryError, SourceError  error
	NextSummary, CooldownUntil time.Time
	// Width and Height come from tea.WindowSizeMsg; Cursor selects a body item
	// and Offset scrolls the body. Expanded is keyed by review-thread ID so an
	// expansion survives a refresh that replaces the thread value.
	Width, Height  int
	Cursor, Offset int
	Expanded       map[string]bool
	// Open and Zoom are host actions injected by the launcher; nil means no-op.
	Open        func(url string) error
	Zoom        func() error
	ActionError error
	resolver    SourceResolver
	github      GitHub
	cache       Cache
	now         func() time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	resolving   bool
	failures    int
}

// New constructs a model. The clock must be safe to call from commands.
func New(r SourceResolver, g GitHub, c Cache, now func() time.Time) *Model {
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Model{resolver: r, github: g, cache: c, now: now, ctx: ctx, cancel: cancel, Section: model.Overview, Discussions: map[model.Section]*DiscussionState{}, Expanded: map[string]bool{}}
}
func (m *Model) Init() tea.Cmd { return func() tea.Msg { return TickMsg{} } }
func (m *Model) SummaryStale() bool {
	return m.SummaryError != nil || (!m.Snapshot.FetchedAt.IsZero() && m.now().Sub(m.Snapshot.FetchedAt) >= time.Minute)
}
func (m *Model) DiscussionStale(s model.Section) bool {
	d := m.Discussions[s]
	return d == nil || d.Data == nil || d.Error != nil || !d.Data.Complete || m.now().Sub(d.Data.FetchedAt) >= model.DiscussionFreshness
}
