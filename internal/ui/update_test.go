package ui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

type fakeAPI struct {
	summaries, discussions int
	err                    error
	complete               bool
	seen                   context.Context
}

func (f *fakeAPI) Snapshot(ctx context.Context, s model.Source) (model.Snapshot, error) {
	f.summaries++
	f.seen = ctx
	return model.Snapshot{PR: &model.PR{Host: "github.com", Repository: "a/b", Number: 1}, Title: s.Branch}, f.err
}
func (f *fakeAPI) Discussion(ctx context.Context, p model.PR, s model.Section) (model.Discussion, error) {
	f.discussions++
	return model.Discussion{PR: p, Section: s, Complete: f.complete}, f.err
}

type fakeCache struct {
	entry         model.CacheEntry
	hit           bool
	err, writeErr error
	writes        int
}

func (f *fakeCache) Get(model.PR, model.Section, time.Time) (model.CacheEntry, bool, error) {
	return f.entry, f.hit, f.err
}
func (f *fakeCache) Put(model.PR, model.Section, model.Discussion, time.Time) error {
	f.writes++
	return f.writeErr
}
func harness() (*Model, *fakeAPI, *fakeCache, *time.Time) {
	now := time.Unix(1000, 0)
	api := &fakeAPI{complete: true}
	cache := &fakeCache{}
	m := New(nil, api, cache, func() time.Time { return now })
	return m, api, cache, &now
}
func apply(m *Model, msg tea.Msg) tea.Cmd { _, cmd := m.Update(msg); return cmd }
func finish(m *Model, c tea.Cmd) {
	for c != nil {
		c = apply(m, c())
	}
}
func source(m *Model, branch string, visible bool) tea.Cmd {
	return apply(m, SourceResult{Generation: m.Generation, Source: model.Source{CWD: "/repo", Root: "/repo", Branch: branch, HEAD: branch, Visible: visible}})
}
func TestSummaryPolicy(t *testing.T) {
	m, a, _, now := harness()
	finish(m, source(m, "main", true))
	if a.summaries != 1 || m.Snapshot.Title != "main" {
		t.Fatal("initial summary")
	}
	*now = now.Add(59 * time.Second)
	finish(m, source(m, "main", true))
	if a.summaries != 1 {
		t.Fatal("early poll")
	}
	*now = now.Add(time.Second)
	finish(m, source(m, "main", true))
	if a.summaries != 2 || a.discussions != 0 {
		t.Fatal("summary cadence")
	}
	*now = now.Add(time.Minute)
	finish(m, source(m, "main", false))
	if a.summaries != 2 {
		t.Fatal("hidden polling")
	}
	finish(m, source(m, "main", true))
	if a.summaries != 3 {
		t.Fatal("resume")
	}
}
func TestLateGenerationAndCoalescing(t *testing.T) {
	m, a, _, _ := harness()
	old := source(m, "old", true)
	if apply(m, RefreshMsg{}) != nil {
		t.Fatal("duplicate")
	}
	next := source(m, "new", true)
	if m.Snapshot.PR != nil || m.Section != model.Overview {
		t.Fatal("not invalidated")
	}
	finish(m, next)
	finish(m, old)
	if m.Snapshot.Title != "new" || a.seen.Err() == nil {
		t.Fatal("late result accepted or context not canceled")
	}
}
func TestDiscussionCachePolicy(t *testing.T) {
	for _, mode := range []string{"miss", "fresh", "expired", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			m, a, c, now := harness()
			finish(m, source(m, "main", true))
			if mode != "miss" {
				c.hit = true
				c.entry = model.CacheEntry{FetchedAt: *now, Data: model.Discussion{Complete: true, FetchedAt: *now}}
			}
			if mode == "expired" {
				c.entry.FetchedAt = now.Add(-301 * time.Second)
			}
			if mode == "corrupt" {
				c.hit = false
				c.err = errors.New("corrupt")
			}
			finish(m, apply(m, SelectSectionMsg(model.Reviews)))
			want := 1
			if mode == "fresh" {
				want = 0
			}
			if a.discussions != want || m.Discussions[model.Reviews].Data == nil {
				t.Fatal("opening cache policy")
			}
			*now = time.Unix(1400, 0)
			finish(m, source(m, "main", true))
			if !m.DiscussionStale(model.Reviews) || a.discussions != want {
				t.Fatal("TTL must not poll discussion")
			}
			finish(m, apply(m, RefreshMsg{}))
			if a.discussions != want+1 {
				t.Fatal("manual bypass")
			}
			finish(m, apply(m, SelectSectionMsg(model.Overview)))
			finish(m, apply(m, RefreshMsg{}))
			if a.discussions != want+1 {
				t.Fatal("overview fetched discussion")
			}
		})
	}
}
func TestFailuresPreserveMemory(t *testing.T) {
	m, a, c, _ := harness()
	finish(m, source(m, "main", true))
	c.writeErr = errors.New("disk full")
	finish(m, apply(m, SelectSectionMsg(model.Reviews)))
	old := m.Discussions[model.Reviews].Data
	if old == nil || m.Discussions[model.Reviews].Warning == nil {
		t.Fatal("write failure lost memory or warning")
	}
	a.err = errors.New("network")
	finish(m, apply(m, RefreshMsg{}))
	if m.Discussions[model.Reviews].Data != old || !m.DiscussionStale(model.Reviews) {
		t.Fatal("failure lost cache")
	}
	a.err = nil
	a.complete = false
	finish(m, apply(m, RefreshMsg{}))
	if m.Discussions[model.Reviews].Data != old || c.writes != 1 {
		t.Fatal("partial overwritten")
	}
	finish(m, apply(m, SelectSectionMsg(model.Overview)))
	finish(m, apply(m, SelectSectionMsg(model.Reviews)))
	if m.Discussions[model.Reviews].Data != old {
		t.Fatal("disk miss replaced memory")
	}
}
func TestCooldownAndBackoff(t *testing.T) {
	m, a, _, now := harness()
	finish(m, source(m, "main", true))
	a.err = &model.FetchError{Kind: model.RateLimitError, Err: errors.New("limit"), RetryAt: now.Add(10 * time.Minute)}
	finish(m, apply(m, RefreshMsg{}))
	if m.Snapshot.PR == nil || !m.SummaryStale() {
		t.Fatal("failure must retain summary")
	}
	if apply(m, RefreshMsg{}) != nil {
		t.Fatal("manual bypassed cooldown")
	}
	*now = now.Add(10 * time.Minute)
	a.err = errors.New("network")
	for i := 0; i < 6; i++ {
		finish(m, source(m, "main", true))
		if m.NextSummary.Sub(*now) > 5*time.Minute {
			t.Fatal("unbounded backoff")
		}
		*now = m.NextSummary
	}
	a.err = nil
	finish(m, source(m, "main", true))
	if m.NextSummary.Sub(*now) != time.Minute {
		t.Fatal("success did not reset")
	}
}

// blockedAPI deliberately ignores cancellation to exercise the result-generation
// guard as well as context cancellation. The test controls completion order.
type blockedAPI struct {
	started chan context.Context
	release chan struct{}
}

func (b *blockedAPI) Snapshot(ctx context.Context, s model.Source) (model.Snapshot, error) {
	if s.Branch == "old" {
		b.started <- ctx
		<-b.release
	}
	return model.Snapshot{Title: s.Branch, PR: &model.PR{Host: "github.com", Repository: "a/b", Number: 1}}, nil
}
func (b *blockedAPI) Discussion(context.Context, model.PR, model.Section) (model.Discussion, error) {
	panic("unexpected discussion")
}
func TestConcurrentObsoleteCompletion(t *testing.T) {
	b := &blockedAPI{make(chan context.Context, 1), make(chan struct{})}
	m := New(nil, b, nil, nil)
	old := source(m, "old", true)
	done := make(chan tea.Msg, 1)
	go func() { done <- old() }()
	ctx := <-b.started
	finish(m, source(m, "new", true))
	if ctx.Err() != context.Canceled {
		t.Fatal("obsolete request not canceled")
	}
	close(b.release)
	finish(m, apply(m, <-done))
	if m.Snapshot.Title != "new" {
		t.Fatal("obsolete result replaced current PR")
	}
}
func TestCacheDisplayedBeforeRefreshAndLateDiscussion(t *testing.T) {
	m, a, c, now := harness()
	finish(m, source(m, "main", true))
	c.hit = true
	c.entry = model.CacheEntry{FetchedAt: now.Add(-301 * time.Second), Data: model.Discussion{Complete: true}}
	load := apply(m, SelectSectionMsg(model.Comments))
	fetch := apply(m, load())
	if m.Discussions[model.Comments].Data == nil || !m.DiscussionStale(model.Comments) || !m.Discussions[model.Comments].Loading || a.discussions != 0 {
		t.Fatal("stale cache must be visible before request completes")
	}
	if apply(m, RefreshMsg{}) != nil {
		t.Fatal("discussion not coalesced")
	}
	late := fetch()
	finish(m, source(m, "other", true))
	finish(m, apply(m, late))
	if len(m.Discussions) != 0 || m.Section != model.Overview {
		t.Fatal("obsolete discussion accepted")
	}
	if a.discussions != 1 {
		t.Fatal("branch change fetched discussion")
	}
}
func TestIndependentSectionsAndSummaryWhileReviewsOpen(t *testing.T) {
	m, a, _, now := harness()
	finish(m, source(m, "main", true))
	finish(m, apply(m, SelectSectionMsg(model.Reviews)))
	finish(m, apply(m, SelectSectionMsg(model.Comments)))
	if a.discussions != 2 || len(m.Discussions) != 2 {
		t.Fatal("section data not independent")
	}
	finish(m, apply(m, SelectSectionMsg(model.Reviews)))
	*now = now.Add(time.Minute)
	finish(m, source(m, "main", true))
	if a.discussions != 2 || m.Section != model.Reviews || m.Discussions[model.Reviews].Data == nil {
		t.Fatal("summary changed discussion")
	}
}
func TestFallbackCooldownAndDiscussionCooldown(t *testing.T) {
	m, a, _, now := harness()
	finish(m, source(m, "main", true))
	a.err = &model.FetchError{Kind: model.RateLimitError, Err: errors.New("secondary limit")}
	finish(m, apply(m, SelectSectionMsg(model.Reviews)))
	if m.CooldownUntil.Sub(*now) != 5*time.Minute || m.Discussions[model.Reviews].Error == nil {
		t.Fatal("missing fallback cooldown")
	}
	if apply(m, RefreshMsg{}) != nil {
		t.Fatal("discussion refresh bypassed cooldown")
	}
	finish(m, apply(m, SelectSectionMsg(model.Overview)))
	if apply(m, RefreshMsg{}) != nil {
		t.Fatal("summary refresh bypassed shared cooldown")
	}
}

type fakeResolver struct{ calls int }

func (r *fakeResolver) Resolve(ctx context.Context) (model.Source, error) {
	r.calls++
	if _, ok := ctx.Deadline(); !ok {
		panic("unbounded resolution")
	}
	return model.Source{CWD: "/repo", Root: "/repo", Branch: "main", Visible: true}, nil
}
func TestInitAndLocalChecksCoalesce(t *testing.T) {
	m, a, _, _ := harness()
	r := &fakeResolver{}
	m.resolver = r
	batch := apply(m, m.Init()())()
	cmds, ok := batch.(tea.BatchMsg)
	if !ok || len(cmds) != 2 {
		t.Fatalf("expected resolver and two-second wakeup, got %T", batch)
	}
	if !m.resolving {
		t.Fatal("source resolution not tracked")
	}
	if duplicate := m.resolve(); duplicate != nil {
		t.Fatal("overlapping local checks")
	}
	finish(m, cmds[0])
	if r.calls != 1 || a.summaries != 1 || m.Source.Branch != "main" || m.resolving {
		t.Fatal("initial asynchronous source resolution failed")
	}
}

func TestManualRefreshDuringFreshCacheRead(t *testing.T) {
	m, a, c, now := harness()
	finish(m, source(m, "main", true))
	c.hit = true
	c.entry = model.CacheEntry{FetchedAt: *now, Data: model.Discussion{Complete: true, FetchedAt: *now}}
	read := apply(m, SelectSectionMsg(model.Reviews))
	if read == nil {
		t.Fatal("missing cache read")
	}
	if apply(m, RefreshMsg{}) != nil || apply(m, RefreshMsg{}) != nil {
		t.Fatal("refresh should wait for in-flight read")
	}
	fetch := apply(m, read())
	if fetch == nil {
		t.Fatal("manual refresh lost behind fresh cache")
	}
	if m.Discussions[model.Reviews].Data == nil || !m.Discussions[model.Reviews].Loading {
		t.Fatal("cache should remain visible while refreshing")
	}
	if apply(m, RefreshMsg{}) != nil {
		t.Fatal("duplicate network request")
	}
	finish(m, fetch)
	if a.discussions != 1 || m.Discussions[model.Reviews].Loading {
		t.Fatalf("requests=%d", a.discussions)
	}
}

func sourceAt(m *Model, branch, cwd, head string, visible bool) tea.Cmd {
	return apply(m, SourceResult{Generation: m.Generation, Source: model.Source{
		WorkspaceID: "w1", TabID: "t1", PaneID: "p1",
		CWD: cwd, Root: "/repo", Branch: branch, HEAD: head, Visible: visible}})
}

func TestCwdChangeInsideCheckoutChangesNothing(t *testing.T) {
	m, a, _, _ := harness()
	finish(m, sourceAt(m, "main", "/repo", "abc", true))
	finish(m, apply(m, SelectSectionMsg(model.Reviews)))
	m.Offset, m.Cursor = 3, 1
	gen, summaries, discussions := m.Generation, a.summaries, a.discussions

	finish(m, sourceAt(m, "main", "/repo/docs", "abc", true))

	if m.Section != model.Reviews || m.Discussions[model.Reviews] == nil {
		t.Fatalf("cd inside the checkout lost the open section: %q", m.Section)
	}
	if m.Generation != gen || m.Snapshot.PR == nil {
		t.Fatalf("cd inside the checkout invalidated the snapshot (generation %d -> %d)", gen, m.Generation)
	}
	if a.summaries != summaries || a.discussions != discussions {
		t.Fatalf("cd inside the checkout fetched: summaries %d, discussions %d", a.summaries, a.discussions)
	}
	if m.Offset != 3 || m.Cursor != 1 {
		t.Fatalf("cd inside the checkout moved the viewport: offset %d cursor %d", m.Offset, m.Cursor)
	}
	if m.Source.CWD != "/repo/docs" {
		t.Fatalf("the new working directory was not recorded: %q", m.Source.CWD)
	}
}

func TestNewCommitRefreshesSummaryWithoutReset(t *testing.T) {
	m, a, _, now := harness()
	finish(m, sourceAt(m, "main", "/repo", "abc", true))
	finish(m, apply(m, SelectSectionMsg(model.Reviews)))
	gen, discussions := m.Generation, a.discussions
	*now = now.Add(5 * time.Second)

	finish(m, sourceAt(m, "main", "/repo", "def", true))

	if a.summaries != 2 {
		t.Fatalf("a new commit must refresh the summary exactly once, got %d fetches", a.summaries)
	}
	if m.Section != model.Reviews || m.Discussions[model.Reviews] == nil || a.discussions != discussions {
		t.Fatalf("a new commit discarded the open discussion (section %q, fetches %d)", m.Section, a.discussions)
	}
	if m.Generation != gen {
		t.Fatalf("a new commit bumped the generation %d -> %d, discarding in-flight work", gen, m.Generation)
	}
}

func TestBranchChangeStillResets(t *testing.T) {
	m, _, _, _ := harness()
	finish(m, sourceAt(m, "main", "/repo", "abc", true))
	finish(m, apply(m, SelectSectionMsg(model.Reviews)))
	gen := m.Generation

	finish(m, sourceAt(m, "feature", "/repo", "xyz", true))

	if m.Section != model.Overview || len(m.Discussions) != 0 || m.Generation <= gen {
		t.Fatalf("a branch change must reset: section %q, %d discussions, generation %d -> %d",
			m.Section, len(m.Discussions), gen, m.Generation)
	}
}

func sourceInPane(m *Model, pane, root, branch string) tea.Cmd {
	return apply(m, SourceResult{Generation: m.Generation, Source: model.Source{
		WorkspaceID: "w1", TabID: "t1", PaneID: pane,
		CWD: root, Root: root, Branch: branch, HEAD: "abc", Visible: true}})
}

func TestWorkingPaneSwitchWithinOneCheckoutChangesNothing(t *testing.T) {
	m, a, _, _ := harness()
	finish(m, sourceInPane(m, "p1", "/repo", "main"))
	finish(m, apply(m, SelectSectionMsg(model.Reviews)))
	m.Offset, m.Cursor = 3, 1
	gen, summaries, discussions := m.Generation, a.summaries, a.discussions

	finish(m, sourceInPane(m, "p2", "/repo", "main"))

	if m.Section != model.Reviews || m.Discussions[model.Reviews] == nil {
		t.Fatalf("focusing a sibling pane lost the open section: %q", m.Section)
	}
	if m.Generation != gen || m.Snapshot.PR == nil {
		t.Fatalf("focusing a sibling pane invalidated the snapshot (generation %d -> %d)", gen, m.Generation)
	}
	if a.summaries != summaries || a.discussions != discussions {
		t.Fatalf("focusing a sibling pane fetched: summaries %d, discussions %d", a.summaries, a.discussions)
	}
	if m.Offset != 3 || m.Cursor != 1 {
		t.Fatalf("focusing a sibling pane moved the viewport: offset %d cursor %d", m.Offset, m.Cursor)
	}
	if m.Source.PaneID != "p2" {
		t.Fatalf("the newly focused pane was not recorded: %q", m.Source.PaneID)
	}
}

func TestWorkingPaneSwitchToAnotherCheckoutResets(t *testing.T) {
	m, _, _, _ := harness()
	finish(m, sourceInPane(m, "p1", "/repo", "main"))
	finish(m, apply(m, SelectSectionMsg(model.Reviews)))
	gen := m.Generation

	finish(m, sourceInPane(m, "p2", "/other", "main"))

	if m.Section != model.Overview || len(m.Discussions) != 0 || m.Generation <= gen {
		t.Fatalf("another checkout must reset: section %q, %d discussions, generation %d -> %d",
			m.Section, len(m.Discussions), gen, m.Generation)
	}
}
