package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

func viewHarness() (*Model, *time.Time) {
	now := time.Unix(1_000_000, 0)
	m := New(nil, &fakeAPI{complete: true}, &fakeCache{}, func() time.Time { return now })
	m.Width, m.Height = 44, 40
	return m, &now
}

func overviewFixture(m *Model, now time.Time) {
	m.Source = model.Source{CWD: "/repo", Root: "/repo", Branch: "feature/retry", Visible: true}
	m.Snapshot = model.Snapshot{
		PR:             &model.PR{Host: "github.com", Repository: "acme/service", Number: 3630, URL: "https://github.com/acme/service/pull/3630"},
		Title:          "Re-request denied approvals when the reviewer list changes",
		Author:         "author",
		State:          "OPEN",
		BaseBranch:     "main",
		HeadBranch:     "feature/retry",
		ReviewDecision: "CHANGES_REQUESTED",
		Commits:        143, ChangedFiles: 8, Additions: 284, Deletions: 76,
		Checks: []model.Check{
			{Name: "unit tests", URL: "https://ci.example.com/unit", State: model.CheckFailed},
			{Name: "deploy preview", URL: "https://ci.example.com/deploy", State: model.CheckTimedOut},
			{Name: "e2e", URL: "https://ci.example.com/e2e", State: model.CheckCancelled},
			{Name: "migrate", URL: "https://ci.example.com/migrate", State: model.CheckActionRequired},
			{Name: "integration tests", URL: "https://ci.example.com/int", State: model.CheckPending},
			{Name: "lint", URL: "https://ci.example.com/lint", State: model.CheckPassed},
			{Name: "build", URL: "https://ci.example.com/build", State: model.CheckPassed},
			{Name: "docs", URL: "https://ci.example.com/docs", State: model.CheckNeutral},
			{Name: "vendor", URL: "https://ci.example.com/vendor", State: model.CheckSkipped},
			{Name: "mystery", URL: "https://ci.example.com/mystery", State: model.CheckUnknown},
		},
		CheckCounts: model.CheckCounts{Failed: 4, Pending: 1, Passed: 2, Neutral: 1, Skipped: 1, Unknown: 1},
		FetchedAt:   now.Add(-12 * time.Second),
	}
}

const longPath = "services/platform/ingest/pipeline/transform/normalize/very/deeply/nested/directory/" +
	"tree/that/keeps/going/and/going/for/a/very/long/time/indeed/so/that/the/left/truncation/is/" +
	"exercised/properly/by/the/renderer/at/every/width/we/care/about/here/handler.go"

func commentsFixture(m *Model, now time.Time) {
	overviewFixture(m, now)
	m.Section = model.Comments
	m.Discussions[model.Comments] = &DiscussionState{Data: &model.Discussion{
		Section:  model.Comments,
		Complete: true,
		Comments: []model.Comment{
			{ID: "c1", Author: "octocat", URL: "https://github.com/acme/service/pull/3630#issuecomment-1",
				CreatedAt: now.Add(-90 * time.Minute),
				Body: "Please look at \x1b[31mthis\x07 snippet:\n\n```go\nfunc main() {\n\tprintln(\"日本語テキスト 🚀\")\n}\n```\n" +
					"Rest of a fairly long paragraph that must wrap cleanly at every rendered width we test."},
			{ID: "c2", Author: "第二の著者", URL: "https://github.com/acme/service/pull/3630#issuecomment-2",
				CreatedAt: now.Add(-2 * time.Hour), Body: "短い返信 🎉"},
		},
		FetchedAt: now.Add(-30 * time.Second),
	}}
}

func reviewsFixture(m *Model, now time.Time) {
	overviewFixture(m, now)
	m.Section = model.Reviews
	line := 42
	m.Discussions[model.Reviews] = &DiscussionState{Data: &model.Discussion{
		Section:  model.Reviews,
		Complete: true,
		Reviews: []model.Review{
			{Comment: model.Comment{ID: "r1", Author: "reviewer", URL: "https://github.com/acme/service/pull/3630#pullrequestreview-1", CreatedAt: now.Add(-time.Hour)}, State: "APPROVED"},
		},
		Threads: []model.ReviewThread{
			{ID: "t1", Path: longPath, Line: &line, URL: "https://github.com/acme/service/pull/3630#discussion_r1",
				Comments: []model.Comment{
					{ID: "tc1", Author: "reviewer", Body: "This needs a guard clause before the retry loop.", CreatedAt: now.Add(-time.Hour)},
					{ID: "tc2", Author: "author", Body: "Added in the latest push.", CreatedAt: now.Add(-30 * time.Minute)},
				}},
			{ID: "t2", Path: "internal/ui/view.go", Resolved: true, Outdated: true,
				URL: "https://github.com/acme/service/pull/3630#discussion_r2",
				Comments: []model.Comment{
					{ID: "tc3", Author: "reviewer", Body: "Nit: rename.", CreatedAt: now.Add(-3 * time.Hour)},
				}},
		},
		FetchedAt: now.Add(-30 * time.Second),
	}}
}

func fixtures(now time.Time) map[string]func(*Model) {
	return map[string]func(*Model){
		"overview": func(m *Model) { overviewFixture(m, now) },
		"no-checks": func(m *Model) {
			overviewFixture(m, now)
			m.Snapshot.Checks, m.Snapshot.CheckCounts = nil, model.CheckCounts{}
		},
		"comments":     func(m *Model) { commentsFixture(m, now) },
		"reviews":      func(m *Model) { reviewsFixture(m, now) },
		"reviews-open": func(m *Model) { reviewsFixture(m, now); m.Expanded["t1"] = true },
		"loading": func(m *Model) {
			m.Source = model.Source{Root: "/repo", Branch: "main", Visible: true}
			m.SummaryLoading = true
		},
		"no-pane": func(m *Model) { m.Source = model.Source{EmptyReason: model.NoWorkingPane} },
		"no-pr":   func(m *Model) { m.Snapshot.EmptyReason = model.NoPR },
		"auth-error": func(m *Model) {
			overviewFixture(m, now)
			m.SummaryError = &model.FetchError{Kind: model.AuthenticationError, Err: errors.New("gh: authentication failed for github.com")}
		},
		"cooldown": func(m *Model) { overviewFixture(m, now); m.CooldownUntil = now.Add(97 * time.Second) },
		"warning": func(m *Model) {
			commentsFixture(m, now)
			m.Discussions[model.Comments].Warning = errors.New("disk full while writing cache")
		},
	}
}

func assertBounds(t *testing.T, content string, w, h int) {
	t.Helper()
	lines := strings.Split(content, "\n")
	if len(lines) > h {
		t.Fatalf("rendered %d lines, height %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got > w {
			t.Fatalf("line %d width %d > %d: %q", i, got, w, l)
		}
	}
}

// survivors are strings the narrowest supported pane must still show, so the
// width bound cannot be satisfied by silently truncating the content away.
var survivors = map[string][]string{
	"overview":     {"Re-request", "Overview", "unit tests"},
	"no-checks":    {"Re-request", "Overview", "No checks"},
	"comments":     {"Re-request", "Comments", "@octocat"},
	"reviews":      {"Re-request", "Reviews", "handler.go", "view.go"},
	"reviews-open": {"Re-request", "Reviews", "handler.go", "guard clause"},
	"no-pane":      {"No working pane"},
	"no-pr":        {"No PR for this branch"},
	"cooldown":     {"Re-request", "retry in 97s"},
}

func TestRenderBoundsAcrossSizes(t *testing.T) {
	_, now := viewHarness()
	for name, apply := range fixtures(*now) {
		for _, w := range []int{30, 44, 100} {
			for _, h := range []int{10, 40} {
				m, _ := viewHarness()
				apply(m)
				m.Width, m.Height = w, h
				out := m.View().Content
				assertBounds(t, out, w, h)
				if !strings.Contains(out, "q close") {
					t.Fatalf("%s %dx%d: footer missing", name, w, h)
				}
				if w != 30 || h != 40 {
					continue
				}
				for _, want := range survivors[name] {
					if !strings.Contains(out, want) {
						t.Fatalf("%s at %dx%d lost %q:\n%s", name, w, h, want, out)
					}
				}
			}
		}
	}
}

// bodyRows returns the rendered rows below the tab line, which is where the
// section body starts once the header has been laid out.
func bodyRows(content string) []string {
	rows := strings.Split(content, "\n")
	for i, r := range rows {
		if !strings.Contains(r, "Overview") || !strings.Contains(r, "Reviews") {
			continue
		}
		rows = rows[i+1:]
		for len(rows) > 0 && strings.TrimSpace(rows[0]) == "" {
			rows = rows[1:]
		}
		return rows
	}
	return rows
}

func tallCommentsModel(t *testing.T) *Model {
	t.Helper()
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width, m.Height = 44, 40
	var body strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&body, "body line %d of a review that runs well past one screen\n", i)
	}
	finish(m, apply(m, SelectSectionMsg(model.Comments)))
	finish(m, apply(m, DiscussionResult{Generation: m.Generation, Section: model.Comments,
		Data: model.Discussion{Section: model.Comments, Complete: true, FetchedAt: *now,
			Comments: []model.Comment{
				{ID: "c1", Author: "coderabbit", Body: body.String(), CreatedAt: now.Add(-time.Hour)},
				{ID: "c2", Author: "reviewer", Body: "Short follow-up.", CreatedAt: now.Add(-time.Minute)},
			}}}))
	return m
}

func TestItemTallerThanBodyShowsItsHead(t *testing.T) {
	m := tallCommentsModel(t)
	rows := bodyRows(m.View().Content)
	if !strings.Contains(rows[0], "fetched") || !strings.Contains(rows[1], "@coderabbit") {
		t.Fatalf("body must open on the head of the first comment:\n%s", strings.Join(rows[:4], "\n"))
	}
	if m.Offset != 0 {
		t.Fatalf("offset %d: a tall item must not be aligned by its tail", m.Offset)
	}
	if out := m.View().Content; strings.Contains(out, "body line 119") {
		t.Fatalf("the tail of the first comment must be off-screen:\n%s", out)
	}
}

func TestPagingRevealsTheRestOfATallItem(t *testing.T) {
	m := tallCommentsModel(t)
	apply(m, key("pgdown"))
	if m.Offset != m.bodyHeight() {
		t.Fatalf("pgdown moved the offset to %d, want %d", m.Offset, m.bodyHeight())
	}
	out := m.View().Content
	if strings.Contains(out, "@coderabbit") {
		t.Fatalf("pgdown must scroll the author header away:\n%s", out)
	}
	if m.Cursor != 0 {
		t.Fatalf("scrolling must not move the cursor, got %d", m.Cursor)
	}
	if strings.Contains(out, "body line 0 ") || !strings.Contains(out, "body line ") {
		t.Fatalf("pgdown did not reveal the next page:\n%s", out)
	}
	apply(m, key("pgup"))
	if m.Offset != 0 || !strings.Contains(m.View().Content, "@coderabbit") {
		t.Fatalf("pgup must return to the head, offset %d", m.Offset)
	}
}

func TestCursorMovesBetweenTallItems(t *testing.T) {
	m := tallCommentsModel(t)
	apply(m, key("j"))
	if m.Cursor != 1 {
		t.Fatalf("cursor %d after j", m.Cursor)
	}
	if out := m.View().Content; !strings.Contains(out, "@reviewer · 1m ago") {
		t.Fatalf("j must bring the next item's header into view:\n%s", out)
	}
	apply(m, key("k"))
	if m.Cursor != 0 {
		t.Fatalf("cursor %d after k", m.Cursor)
	}
	if row := bodyRows(m.View().Content)[0]; !strings.Contains(row, "@coderabbit") {
		t.Fatalf("k must show the head of the first item again, offset %d, first row %q", m.Offset, row)
	}
}

func TestCountsAreSingularAtOne(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 100
	m.Snapshot.Commits, m.Snapshot.ChangedFiles = 1, 1
	if out := m.View().Content; !strings.Contains(out, "1 commit · 1 file") {
		t.Fatalf("counts must be singular at one:\n%s", out)
	}
	m, now = viewHarness()
	reviewsFixture(m, *now)
	m.Width = 100
	out := m.View().Content
	if !strings.Contains(out, "  1 comment") || strings.Contains(out, "1 comments") {
		t.Fatalf("a single reply must read 1 comment:\n%s", out)
	}
	if !strings.Contains(out, "2 comments") {
		t.Fatalf("two replies must still read 2 comments:\n%s", out)
	}
}

func TestThreadRowKeepsFileNameAtEveryWidth(t *testing.T) {
	for _, w := range []int{30, 36, 44} {
		m, now := viewHarness()
		reviewsFixture(m, *now)
		m.Width, m.Height = w, 40
		out := m.View().Content
		assertBounds(t, out, w, 40)
		for _, want := range []string{"handler.go", "view.go"} {
			if !strings.Contains(out, want) {
				t.Fatalf("width %d dropped the thread file name %q:\n%s", w, want, out)
			}
		}
		if !strings.Contains(out, "✓~") {
			t.Fatalf("width %d lost the resolved and outdated state:\n%s", w, out)
		}
	}
}

func TestBidiAndZeroWidthRunesAreStripped(t *testing.T) {
	m, now := viewHarness()
	reviewsFixture(m, *now)
	d := m.Discussions[model.Reviews].Data
	d.Threads[1].Path = "internal/ui/\u202egg.exe\u202c/sa\u200bfe.go"
	d.Reviews[0].Author = "oct\u2066o\u2069cat\ufeff"
	d.Threads[1].Comments[0].Body = "line\u2028break\u2029and\u200fmore, joined \U0001f469\u200d\U0001f4bb stays"
	m.Expanded["t2"] = true
	m.Width = 100
	out := m.View().Content
	for _, r := range []rune{0x200b, 0x200f, 0x2028, 0x2029, 0x202c, 0x202e, 0x2066, 0x2069, 0xfeff} {
		if strings.ContainsRune(out, r) {
			t.Fatalf("rune %U survived sanitization:\n%q", r, out)
		}
	}
	if !strings.Contains(out, "safe.go") || !strings.Contains(out, "@octocat") {
		t.Fatalf("sanitization removed visible text:\n%s", out)
	}
	if !strings.Contains(out, "\U0001f469\u200d\U0001f4bb") {
		t.Fatalf("the zero-width joiner of an emoji sequence must survive:\n%q", out)
	}
}

func TestOverviewRendersIdentityStatisticsAndChecks(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 100
	out := m.View().Content
	for _, want := range []string{
		"GLANCE PR", "refreshed 12s ago", "acme/service", "feature/retry", "#3630", "OPEN",
		"Re-request denied approvals", "@author", "feature/retry → main", "143 commits", "8 files",
		"+284", "-76", "Review: Changes requested", "[Overview]", "Comments", "Reviews",
		"CHECKS", "4 failed", "1 pending", "2 passed",
		"× unit tests", "× deploy preview", "× e2e", "× migrate", "◷ integration tests",
		"✓ lint", "✓ build", "· docs", "· vendor", "? mystery",
		"r refresh  o browser  z zoom  q close",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestForkHeadIdentity(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Snapshot.HeadRepository = "fork/service"
	m.Width = 100
	if out := m.View().Content; !strings.Contains(out, "fork/service:feature/retry → main") {
		t.Fatalf("fork head missing:\n%s", out)
	}
}

func TestEmptyCheckListSaysNoChecks(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Snapshot.Checks, m.Snapshot.CheckCounts = nil, model.CheckCounts{}
	out := m.View().Content
	if !strings.Contains(out, "No checks") || strings.Contains(out, "passed") {
		t.Fatalf("expected No checks and no success claim:\n%s", out)
	}
}

func TestEmptyStates(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		apply      func(*Model)
	}{
		{"pane", "No working pane", func(m *Model) { m.Source.EmptyReason = model.NoWorkingPane }},
		{"dir", "No directory", func(m *Model) { m.Source.EmptyReason = model.NoDirectory }},
		{"git", "Not a Git repository", func(m *Model) { m.Source.EmptyReason = model.NotGit }},
		{"head", "Detached HEAD", func(m *Model) { m.Source.EmptyReason = model.DetachedHEAD }},
		{"pr", "No PR for this branch", func(m *Model) { m.Snapshot.EmptyReason = model.NoPR }},
		{"loading", "Loading…", func(m *Model) { m.SummaryLoading = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := viewHarness()
			tc.apply(m)
			out := m.View().Content
			if !strings.Contains(out, tc.want) {
				t.Fatalf("missing %q in:\n%s", tc.want, out)
			}
			assertBounds(t, out, m.Width, m.Height)
		})
	}
}

func TestErrorCooldownStaleAndCacheWarning(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 100
	m.SummaryError = &model.FetchError{Kind: model.AuthenticationError, Err: errors.New("bad credentials")}
	out := m.View().Content
	if !strings.Contains(out, "authentication") || !strings.Contains(out, "bad credentials") || !strings.Contains(out, "run: gh auth login") {
		t.Fatalf("authentication error not explained:\n%s", out)
	}
	if !strings.Contains(out, "stale") {
		t.Fatalf("failed summary must be marked stale:\n%s", out)
	}

	m, now = viewHarness()
	overviewFixture(m, *now)
	m.Width = 100
	m.CooldownUntil = now.Add(97 * time.Second)
	if out := m.View().Content; !strings.Contains(out, "refreshed 12s ago · rate limited, retry in 97s") {
		t.Fatalf("cooldown must keep the refresh age:\n%s", out)
	}
	m.Snapshot.FetchedAt = now.Add(-90 * time.Second)
	if out := m.View().Content; !strings.Contains(out, "refreshed 1m ago · stale · rate limited, retry in 97s") {
		t.Fatalf("cooldown must keep the age and the stale marker:\n%s", out)
	}

	m, now = viewHarness()
	overviewFixture(m, *now)
	m.Snapshot.FetchedAt = now.Add(-90 * time.Second)
	if out := m.View().Content; !strings.Contains(out, "stale") {
		t.Fatalf("stale marker missing:\n%s", out)
	}

	m, now = viewHarness()
	commentsFixture(m, *now)
	m.Width = 100
	m.Discussions[model.Comments].Warning = errors.New("disk full")
	if out := m.View().Content; !strings.Contains(out, "cache: disk full") {
		t.Fatalf("cache warning missing:\n%s", out)
	}
}

func TestTooNarrow(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 12
	if out := m.View().Content; !strings.Contains(out, "too narrow") {
		t.Fatalf("expected too narrow, got:\n%s", out)
	}
}

func TestCommentsSanitizeWrapAndPreserveCodeBlocks(t *testing.T) {
	m, now := viewHarness()
	commentsFixture(m, *now)
	for _, w := range []int{30, 44, 100} {
		m.Width = w
		out := m.View().Content
		assertBounds(t, out, w, m.Height)
		if strings.ContainsRune(out, 0x1b) || strings.ContainsRune(out, 0x07) {
			t.Fatalf("control characters survived at width %d: %q", w, out)
		}
		for _, want := range []string{"@octocat", "```", "func main() {", "🚀", "@第二の著者"} {
			if !strings.Contains(out, want) {
				t.Fatalf("width %d missing %q in:\n%s", w, want, out)
			}
		}
		if !strings.Contains(out, "    println") {
			t.Fatalf("width %d: tab not expanded inside code block:\n%s", w, out)
		}
	}
}

func TestReviewThreadsCollapseExpandAndTruncatePaths(t *testing.T) {
	m, now := viewHarness()
	reviewsFixture(m, *now)
	m.Width = 44
	out := m.View().Content
	assertBounds(t, out, 44, 40)
	if !strings.Contains(out, "@reviewer  APPROVED") {
		t.Fatalf("review decision missing:\n%s", out)
	}
	if !strings.Contains(out, "▸") || strings.Contains(out, "▾") {
		t.Fatalf("threads must start collapsed:\n%s", out)
	}
	if !strings.Contains(out, "handler.go:42") || !strings.Contains(out, "…") {
		t.Fatalf("long path must be left-truncated with the file name visible:\n%s", out)
	}
	if !strings.Contains(out, "2 comments") || !strings.Contains(out, "✓~ 1c") {
		t.Fatalf("thread metadata missing at width 44:\n%s", out)
	}
	m.Width = 100
	if wide := m.View().Content; !strings.Contains(wide, "2 comments") || !strings.Contains(wide, "(resolved, outdated)") {
		t.Fatalf("full thread metadata missing at width 100:\n%s", wide)
	}
	m.Width = 44
	if strings.Contains(out, "guard clause") {
		t.Fatalf("collapsed thread leaked replies:\n%s", out)
	}

	m.Cursor = 1 // the first thread
	finish(m, apply(m, key("enter")))
	out = m.View().Content
	if !strings.Contains(out, "▾") || !strings.Contains(out, "guard clause") || !strings.Contains(out, "Added in the latest push.") {
		t.Fatalf("expanded thread must show replies:\n%s", out)
	}
	assertBounds(t, out, 44, 40)
}

func TestExpansionSurvivesRefreshAndCursorClamps(t *testing.T) {
	m, now := viewHarness()
	reviewsFixture(m, *now)
	m.Cursor = 1
	finish(m, apply(m, key("enter")))
	if !m.Expanded["t1"] {
		t.Fatal("thread not expanded")
	}
	m.Cursor = 2

	replacement := *m.Discussions[model.Reviews].Data
	replacement.Reviews = nil
	replacement.Threads = replacement.Threads[:1]
	replacement.FetchedAt = *now
	finish(m, apply(m, DiscussionResult{Generation: m.Generation, Section: model.Reviews, Data: replacement}))

	if !m.Expanded["t1"] {
		t.Fatal("expansion must survive a refresh with the same thread ID")
	}
	if m.Cursor != 0 {
		t.Fatalf("cursor must clamp when data shrinks, got %d", m.Cursor)
	}
	if out := m.View().Content; !strings.Contains(out, "▾") || !strings.Contains(out, "guard clause") {
		t.Fatalf("expansion lost in render:\n%s", out)
	}
}

func TestDiscussionAgeAndStaleMarker(t *testing.T) {
	m, now := viewHarness()
	commentsFixture(m, *now)
	m.Width = 100
	if out := m.View().Content; !strings.Contains(out, "fetched 30s ago") || strings.Contains(out, "fetched 30s ago (stale)") {
		t.Fatalf("fresh discussion age wrong:\n%s", out)
	}
	m.Discussions[model.Comments].Data.FetchedAt = now.Add(-400 * time.Second)
	if out := m.View().Content; !strings.Contains(out, "(stale)") {
		t.Fatalf("stale discussion marker missing:\n%s", out)
	}
}
