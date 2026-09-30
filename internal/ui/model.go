// Package ui owns the interactive state and asynchronous refresh policy.
package ui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

type SourceResolver interface {
	Resolve(context.Context) (model.Source, error)
}
type GitHub interface {
	Snapshot(context.Context, model.Source) (model.Snapshot, error)
	SnapshotPR(context.Context, model.Source, model.PR) (model.Snapshot, error)
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
	Source   model.Source
	Snapshot model.Snapshot
	// Pinned is the stack entry being shown instead of the branch's own pull
	// request; nil means the branch's own.
	Pinned                     *model.PR
	Section                    model.Section
	Generation                 uint64
	Discussions                map[model.Section]*DiscussionState
	SummaryLoading             bool
	SummaryError, SourceError  error
	NextSummary, CooldownUntil time.Time
	// Width and Height come from tea.WindowSizeMsg; Cursor selects a body item
	// and Offset scrolls the body. Expanded is keyed by review-thread ID, or by
	// foldKey for the CI fold row, so an expansion survives a refresh that
	// replaces the thread value.
	Width, Height  int
	Cursor, Offset int
	Expanded       map[string]bool
	// ShowHelp replaces the body with the full key list; the selection and
	// scroll position underneath are left alone.
	ShowHelp bool
	// Open and Zoom are host actions injected by the launcher; nil means no-op.
	Open        func(url string) error
	Zoom        func() error
	ActionError error
	// md memoizes Markdown rendering; it is a cache, not observable state.
	md *markdown
	// help renders the full key list; it holds only styles.
	help help.Model
	// spin animates running checks and loading sections. spinning is true
	// while a spinner tick is in flight, so only one chain ever runs.
	spin      spinner.Model
	spinning  bool
	resolver  SourceResolver
	github    GitHub
	cache     Cache
	now       func() time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	resolving bool
	failures  int
	// branchPR remembers the working branch's own pull request while another
	// entry of its stack is pinned, so selecting it again unpins.
	branchPR *model.PR
}

// New constructs a model. The clock must be safe to call from commands.
func New(r SourceResolver, g GitHub, c Cache, now func() time.Time) *Model {
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{md: newMarkdown(), resolver: r, github: g, cache: c, now: now, ctx: ctx, cancel: cancel, Section: model.Overview, Discussions: map[model.Section]*DiscussionState{}, Expanded: map[string]bool{}}
	m.help = help.New()
	m.help.Styles = helpStyles()
	// The spinner renders its bare frame; the span around it picks the colour.
	m.spin = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle()))
	return m
}
func (m *Model) Init() tea.Cmd { return func() tea.Msg { return TickMsg{} } }
func (m *Model) SummaryStale() bool {
	return m.SummaryError != nil || (!m.Snapshot.FetchedAt.IsZero() && m.now().Sub(m.Snapshot.FetchedAt) >= time.Minute)
}
func (m *Model) DiscussionStale(s model.Section) bool {
	d := m.Discussions[s]
	return d == nil || d.Data == nil || d.Error != nil || !d.Data.Complete || m.now().Sub(d.Data.FetchedAt) >= model.DiscussionFreshness
}
