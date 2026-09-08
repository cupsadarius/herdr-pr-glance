package ui

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
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

// stackFixture puts the fixture pull request in the middle of a three-entry
// stack, and teaches the fake API to report the same stack back.
func stackFixture(m *Model, now time.Time) {
	overviewFixture(m, now)
	current := *m.Snapshot.PR
	stack := &model.Stack{Number: 3710, Size: 3, BaseBranch: "main", Entries: []model.StackEntry{
		{Position: 1, PR: model.PR{Host: "github.com", Repository: "acme/service", Number: 3705, NodeID: "PR_3705", URL: "https://github.com/acme/service/pull/3705"},
			Title: "bottom change", State: "MERGED", BaseBranch: "main", HeadBranch: "feature/base", ReviewDecision: "APPROVED"},
		{Position: 2, PR: current, Title: "Re-request denied approvals when the reviewer list changes",
			State: "OPEN", BaseBranch: "feature/base", HeadBranch: "feature/retry", ReviewDecision: "CHANGES_REQUESTED"},
		{Position: 3, PR: model.PR{Host: "github.com", Repository: "acme/service", Number: 3709, NodeID: "PR_3709", URL: "https://github.com/acme/service/pull/3709"},
			Title: "top change", State: "OPEN", Draft: true, BaseBranch: "feature/retry", HeadBranch: "feature/top", ReviewDecision: "REVIEW_REQUIRED"},
	}}
	m.Snapshot.Stack, m.Snapshot.StackPosition = stack, 2
	if a, ok := m.github.(*fakeAPI); ok {
		a.stack, a.position = stack, 2
	}
}

// stackRowLine returns the rendered stack row for a pull request number.
func stackRowLine(t *testing.T, content, number string) string {
	t.Helper()
	for _, l := range strings.Split(content, "\n") {
		if p := ansi.Strip(l); strings.Contains(p, number) && strings.Contains(p, "●") {
			return l
		}
	}
	t.Fatalf("no stack row for %s in:\n%s", number, content)
	return ""
}

func TestStackBlockListsEntriesTopFirstAndMarksTheCurrentOne(t *testing.T) {
	m, now := viewHarness()
	stackFixture(m, *now)
	m.Width, m.Height = 100, 40
	plain := plainView(m)
	if !strings.Contains(plain, "STACK #3710 · 2/3 · base main") {
		t.Fatalf("missing stack heading:\n%s", plain)
	}
	rows := strings.Join(bodyRows(m.View().Content), "\n")
	top, current, bottom := strings.Index(rows, "#3709"), strings.Index(rows, "#3630"), strings.Index(rows, "#3705")
	checks := strings.Index(rows, "CHECKS")
	if top < 0 || current < 0 || bottom < 0 || checks < 0 {
		t.Fatalf("stack rows missing:\n%s", rows)
	}
	if !(top < current && current < bottom && bottom < checks) {
		t.Fatalf("stack must render top first, above the checks:\n%s", rows)
	}
	for _, want := range []string{"review required", "changes requested", "approved"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing review decision %q:\n%s", want, plain)
		}
	}
	// Column 0 is the cursor gutter; the entry marker is the row's own column.
	marker := func(number string) rune {
		r := []rune(ansi.Strip(stackRowLine(t, m.View().Content, number)))
		if len(r) < 2 {
			t.Fatalf("stack row %s is empty", number)
		}
		return r[1]
	}
	if marker("#3630") != '›' {
		t.Fatalf("the shown entry must be marked, got %q", marker("#3630"))
	}
	if marker("#3709") == '›' {
		t.Fatal("only the shown entry is marked")
	}
	m.Snapshot.Stack.Size = 5
	if !strings.Contains(plainView(m), "+2 more") {
		t.Fatalf("a truncated stack must say how many entries are hidden:\n%s", plainView(m))
	}
}

func TestStackRowsCarryStateColors(t *testing.T) {
	m, now := viewHarness()
	stackFixture(m, *now)
	m.Width, m.Height = 100, 40
	out := m.View().Content
	if line := styledLine(t, out, "STACK"); !carries(line, sgrBold) || !carries(line, sgrFaint) {
		t.Errorf("the stack heading must be a bold label with a faint rest: %q", line)
	}
	for _, tc := range []struct{ number, sgr string }{{"#3705", sgrMagenta}, {"#3630", sgrGreen}, {"#3709", sgrFaint}} {
		if line := stackRowLine(t, out, tc.number); !carries(line, tc.sgr) {
			t.Errorf("%s must carry %q: %q", tc.number, tc.sgr, line)
		}
	}
	if line := stackRowLine(t, out, "#3630"); !carries(line, sgrBold) {
		t.Errorf("the shown entry must be bold: %q", line)
	}
}

func TestStackFooterHint(t *testing.T) {
	m, now := viewHarness()
	stackFixture(m, *now)
	m.Width, m.Height = 100, 40
	if !strings.Contains(plainView(m), "[ ] stack") {
		t.Fatalf("a stacked pull request must advertise its keys:\n%s", plainView(m))
	}
	m.Snapshot.Stack, m.Snapshot.StackPosition = nil, 0
	plain := plainView(m)
	if strings.Contains(plain, "[ ] stack") || strings.Contains(plain, "STACK") {
		t.Fatalf("an unstacked pull request must show neither hint nor block:\n%s", plain)
	}
}

func fixtures(now time.Time) map[string]func(*Model) {
	return map[string]func(*Model){
		"overview": func(m *Model) { overviewFixture(m, now) },
		"no-checks": func(m *Model) {
			overviewFixture(m, now)
			m.Snapshot.Checks, m.Snapshot.CheckCounts = nil, model.CheckCounts{}
		},
		"stack":        func(m *Model) { stackFixture(m, now) },
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
	"stack":        {"Re-request", "STACK", "#3705"},
	"no-checks":    {"Re-request", "Overview", "No checks"},
	"comments":     {"Re-request", "Comments", "@octocat"},
	"reviews":      {"Re-request", "Reviews", "handler.go", "view.go"},
	"reviews-open": {"Re-request", "Reviews", "handler.go", "This needs a guard"},
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
				plain := ansi.Strip(out)
				if !strings.Contains(plain, "q close") {
					t.Fatalf("%s %dx%d: footer missing", name, w, h)
				}
				if w != 30 || h != 40 {
					continue
				}
				for _, want := range survivors[name] {
					if !strings.Contains(plain, want) {
						t.Fatalf("%s at %dx%d lost %q:\n%s", name, w, h, want, plain)
					}
				}
			}
		}
	}
}

// bodyRows returns the rendered rows below the tab line, which is where the
// section body starts once the header has been laid out.
func bodyRows(content string) []string {
	rows := strings.Split(ansi.Strip(content), "\n")
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
	if out := plainView(m); !strings.Contains(out, "@reviewer · 1m ago") {
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
	if out := plainView(m); !strings.Contains(out, "1 commit · 1 file") {
		t.Fatalf("counts must be singular at one:\n%s", out)
	}
	m, now = viewHarness()
	reviewsFixture(m, *now)
	m.Width = 100
	out := plainView(m)
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
		out = ansi.Strip(out)
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
	out := plainView(m)
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
	out := plainView(m)
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
	if out := plainView(m); !strings.Contains(out, "fork/service:feature/retry → main") {
		t.Fatalf("fork head missing:\n%s", out)
	}
}

func TestEmptyCheckListSaysNoChecks(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Snapshot.Checks, m.Snapshot.CheckCounts = nil, model.CheckCounts{}
	out := plainView(m)
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
			if !strings.Contains(ansi.Strip(out), tc.want) {
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
	out := plainView(m)
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
	if out := plainView(m); !strings.Contains(out, "refreshed 12s ago · rate limited, retry in 97s") {
		t.Fatalf("cooldown must keep the refresh age:\n%s", out)
	}
	m.Snapshot.FetchedAt = now.Add(-90 * time.Second)
	if out := plainView(m); !strings.Contains(out, "refreshed 1m ago · stale · rate limited, retry in 97s") {
		t.Fatalf("cooldown must keep the age and the stale marker:\n%s", out)
	}

	m, now = viewHarness()
	overviewFixture(m, *now)
	m.Snapshot.FetchedAt = now.Add(-90 * time.Second)
	if out := plainView(m); !strings.Contains(out, "stale") {
		t.Fatalf("stale marker missing:\n%s", out)
	}

	m, now = viewHarness()
	commentsFixture(m, *now)
	m.Width = 100
	m.Discussions[model.Comments].Warning = errors.New("disk full")
	if out := plainView(m); !strings.Contains(out, "cache: disk full") {
		t.Fatalf("cache warning missing:\n%s", out)
	}
}

func TestTooNarrow(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width = 12
	if out := plainView(m); !strings.Contains(out, "too narrow") {
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
		if strings.ContainsRune(out, 0x07) {
			t.Fatalf("control characters survived at width %d: %q", w, out)
		}
		out = ansi.Strip(out)
		// Glamour styles the body, so the sanitizer is proven by what the text
		// says, not by the absence of escapes: the remote sequences are gone
		// and the code block still reads exactly as it was written.
		for _, want := range []string{"@octocat", "Please look at this", "func main() {",
			"println(\"日本語テキスト", "🚀", "@第二の著者"} {
			if !strings.Contains(out, want) {
				t.Fatalf("width %d missing %q in:\n%s", w, want, out)
			}
		}
		if strings.Contains(out, "```") {
			t.Fatalf("width %d: the code fence was not rendered:\n%s", w, out)
		}
	}
}

func TestReviewThreadsCollapseExpandAndTruncatePaths(t *testing.T) {
	m, now := viewHarness()
	reviewsFixture(m, *now)
	m.Width = 44
	out := plainView(m)
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
	if wide := plainView(m); !strings.Contains(wide, "2 comments") || !strings.Contains(wide, "(resolved, outdated)") {
		t.Fatalf("full thread metadata missing at width 100:\n%s", wide)
	}
	m.Width = 44
	if strings.Contains(out, "guard clause") {
		t.Fatalf("collapsed thread leaked replies:\n%s", out)
	}

	m.Cursor = 1 // the first thread
	finish(m, apply(m, key("enter")))
	out = plainView(m)
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
	if out := plainView(m); !strings.Contains(out, "▾") || !strings.Contains(out, "guard clause") {
		t.Fatalf("expansion lost in render:\n%s", out)
	}
}

func TestDiscussionAgeAndStaleMarker(t *testing.T) {
	m, now := viewHarness()
	commentsFixture(m, *now)
	m.Width = 100
	if out := plainView(m); !strings.Contains(out, "fetched 30s ago") || strings.Contains(out, "fetched 30s ago (stale)") {
		t.Fatalf("fresh discussion age wrong:\n%s", out)
	}
	m.Discussions[model.Comments].Data.FetchedAt = now.Add(-400 * time.Second)
	if out := plainView(m); !strings.Contains(out, "(stale)") {
		t.Fatalf("stale discussion marker missing:\n%s", out)
	}
}

func TestLongCheckNamesPreserveOutcomes(t *testing.T) {
	for _, width := range []int{20, 44} {
		for _, tc := range []struct {
			state model.CheckState
			label string
		}{
			{model.CheckFailed, "failed"}, {model.CheckCancelled, "cancelled"}, {model.CheckTimedOut, "timed out"}, {model.CheckActionRequired, "action required"}, {model.CheckUnknown, "unknown"}, {model.CheckNeutral, "neutral"}, {model.CheckSkipped, "skipped"},
		} {
			state := tc.state
			for _, name := range []string{"integration / ubuntu-latest / go-1.25 / database", "日本語\x1b[31m検査\x07 / database"} {
				styledRow := checkRow(model.Check{Name: name, State: state}, width-1)
				row := ansi.Strip(styledRow)
				want := tc.label
				if state == model.CheckActionRequired && width == 20 {
					want = "action req"
				}
				if !strings.Contains(row, want) {
					t.Errorf("width %d: lost %s: %q", width, want, row)
				}
				if ansi.StringWidth(styledRow) > width-1 || strings.ContainsAny(row, "\x1b\x07\n") {
					t.Errorf("invalid row: %q", styledRow)
				}
				if !strings.Contains(row, "inte") && !strings.Contains(row, "日本") {
					t.Errorf("name missing: %q", row)
				}
			}
		}
	}
}

func TestDraftDoesNotHideLifecycle(t *testing.T) {
	for _, tc := range []struct {
		state string
		draft bool
		want  string
	}{{"CLOSED", true, "CLOSED"}, {"MERGED", true, "MERGED"}, {"OPEN", true, "DRAFT"}, {"OPEN", false, "OPEN"}} {
		if got := prState(model.Snapshot{State: tc.state, Draft: tc.draft}); got != tc.want {
			t.Errorf("%s draft=%v: got %s want %s", tc.state, tc.draft, got, tc.want)
		}
	}
}

func TestViewDoesNotMutateModel(t *testing.T) {
	m, now := viewHarness()
	reviewsFixture(m, *now)
	m.Expanded["t1"] = true // render reply bodies too, the most work a view does
	// Capture field values before rendering, including navigation maps. md is
	// exempt: it is the Markdown memoization cache, which View is allowed to
	// fill because View is the only place that knows the width. It holds no
	// state the rest of the program can observe.
	before := reflect.ValueOf(*m)
	values := make([]string, before.NumField())
	for i := range values {
		values[i] = fmt.Sprintf("%#v", before.Field(i))
	}
	m.View()
	m.View()
	after := reflect.ValueOf(*m)
	for i, value := range values {
		if name := after.Type().Field(i).Name; name == "md" {
			continue
		}
		if got := fmt.Sprintf("%#v", after.Field(i)); got != value {
			t.Errorf("View changed %s", after.Type().Field(i).Name)
		}
	}
}

// SGR parameters the palette is allowed to emit: bold, faint, underline,
// reverse and the eight base ANSI foregrounds, all resolved by the terminal's
// own theme. Anything else means a hard-coded colour slipped in.
const (
	sgrBold    = "1"
	sgrFaint   = "2"
	sgrReverse = "7"
	sgrRed     = "31"
	sgrGreen   = "32"
	sgrYellow  = "33"
	sgrMagenta = "35"
	sgrCyan    = "36"
)

var sgrPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

// carries reports whether any escape sequence on the line sets the parameter;
// lipgloss packs several of them into one sequence.
func carries(line, param string) bool {
	for _, seq := range sgrPattern.FindAllString(line, -1) {
		for _, p := range strings.Split(seq[2:len(seq)-1], ";") {
			if p == param {
				return true
			}
		}
	}
	return false
}

// plainView renders the view and strips styling, so tests that care about
// layout keep comparing the text the terminal shows.
func plainView(m *Model) string { return ansi.Strip(m.View().Content) }

// styledLine returns the first rendered line whose plain text contains want.
func styledLine(t *testing.T, content, want string) string {
	t.Helper()
	for _, l := range strings.Split(content, "\n") {
		if strings.Contains(ansi.Strip(l), want) {
			return l
		}
	}
	t.Fatalf("no line containing %q in:\n%s", want, content)
	return ""
}

func TestColorLeavesTheLayoutUnchanged(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"overview", goldenOverview44}, {"reviews", goldenReviews44},
	} {
		m, now := viewHarness()
		fixtures(*now)[tc.name](m)
		m.Width, m.Height = 44, 40
		if got := plainView(m); got != tc.want {
			t.Errorf("%s: styling changed the layout\n--- got ---\n%s\n--- want ---\n%s", tc.name, got, tc.want)
		}
	}
}

func TestCheckRowsAndCountsCarryStateColors(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width, m.Height = 100, 40
	out := m.View().Content
	for _, tc := range []struct{ name, sgr string }{
		{"unit tests", sgrRed}, {"deploy preview", sgrRed}, {"e2e", sgrRed}, {"migrate", sgrRed},
		{"integration tests", sgrYellow}, {"lint", sgrGreen}, {"build", sgrGreen},
		{"docs", sgrFaint}, {"vendor", sgrFaint}, {"mystery", sgrFaint},
		{"4 failed", sgrRed}, {"1 pending", sgrYellow}, {"CHECKS", sgrBold},
	} {
		if line := styledLine(t, out, tc.name); !carries(line, tc.sgr) {
			t.Errorf("%q must carry %q: %q", tc.name, tc.sgr, line)
		}
	}
	if line := styledLine(t, out, "lint"); carries(line, sgrRed) {
		t.Errorf("a passing check must not be red: %q", line)
	}
}

func TestHeaderTabsAndFooterCarryTheirStyles(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width, m.Height = 100, 40
	m.Snapshot.ReviewDecision = "APPROVED"
	out := m.View().Content
	for _, tc := range []struct{ want, sgr string }{
		{"GLANCE PR", sgrBold}, {"refreshed 12s ago", sgrFaint},
		{"acme/service", sgrFaint}, {"feature/retry", sgrCyan},
		{"#3630", sgrBold}, {"OPEN", sgrGreen},
		{"Re-request denied", sgrBold}, {"@author", sgrCyan},
		{"+284", sgrGreen}, {"-76", sgrRed},
		{"Review:", sgrFaint}, {"Approved", sgrGreen},
		{"[Overview]", sgrReverse}, {"[Overview]", sgrBold},
		{"q close", sgrBold}, {"q close", sgrFaint},
	} {
		if line := styledLine(t, out, tc.want); !carries(line, tc.sgr) {
			t.Errorf("%q must carry %q: %q", tc.want, tc.sgr, line)
		}
	}
	m.Snapshot.State, m.Snapshot.ReviewDecision = "MERGED", "REVIEW_REQUIRED"
	out = m.View().Content
	if line := styledLine(t, out, "MERGED"); !carries(line, sgrMagenta) {
		t.Errorf("a merged PR must be magenta: %q", line)
	}
	if line := styledLine(t, out, "Review required"); !carries(line, sgrYellow) {
		t.Errorf("a pending review decision must be yellow: %q", line)
	}
}

func TestErrorsAndEmptyStatesCarryTheirStyles(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width, m.Height = 100, 40
	m.SummaryError = &model.FetchError{Kind: model.AuthenticationError, Err: errors.New("bad credentials")}
	m.CooldownUntil = now.Add(97 * time.Second)
	out := m.View().Content
	for _, tc := range []struct{ want, sgr string }{
		{"bad credentials", sgrRed}, {"run: gh auth login", sgrYellow},
		{"stale", sgrYellow}, {"retry in 97s", sgrYellow},
	} {
		if line := styledLine(t, out, tc.want); !carries(line, tc.sgr) {
			t.Errorf("%q must carry %q: %q", tc.want, tc.sgr, line)
		}
	}

	m, now = viewHarness()
	commentsFixture(m, *now)
	m.Width = 100
	m.Discussions[model.Comments].Warning = errors.New("disk full")
	out = m.View().Content
	if line := styledLine(t, out, "cache: disk full"); !carries(line, sgrYellow) {
		t.Errorf("a cache warning must be yellow: %q", line)
	}
	if line := styledLine(t, out, "@octocat"); !carries(line, sgrCyan) || !carries(line, sgrFaint) {
		t.Errorf("a comment head must be cyan author and faint time: %q", line)
	}

	m, _ = viewHarness()
	m.Source.EmptyReason = model.NoWorkingPane
	if line := styledLine(t, m.View().Content, "No working pane"); !carries(line, sgrFaint) {
		t.Errorf("an empty state must be faint: %q", line)
	}
}

func TestReviewRowsAndThreadsCarryTheirStyles(t *testing.T) {
	m, now := viewHarness()
	reviewsFixture(m, *now)
	m.Width, m.Height = 100, 40
	out := m.View().Content
	for _, tc := range []struct{ want, sgr string }{
		{"@reviewer", sgrCyan}, {"APPROVED", sgrGreen},
		{"handler.go", sgrCyan}, {"handler.go", sgrBold}, {":42", sgrFaint},
		{"▸", sgrBold}, {"(resolved, outdated)", sgrGreen},
	} {
		if line := styledLine(t, out, tc.want); !carries(line, tc.sgr) {
			t.Errorf("%q must carry %q: %q", tc.want, tc.sgr, line)
		}
	}
	m.Width = 30
	if line := styledLine(t, m.View().Content, "✓~"); !carries(line, sgrGreen) {
		t.Errorf("the compact resolved flag must be green: %q", line)
	}
	m.Width = 100
	m.Cursor = 1
	if line := styledLine(t, m.View().Content, "handler.go"); !strings.HasPrefix(line, "\x1b[1m›") {
		t.Errorf("the selection marker must be bold: %q", line)
	}
}

// markdownFixtures render a comment or reply body, which Glamour styles with
// its own 256-colour theme; the palette rule below is about the pane's chrome.
var markdownFixtures = map[string]bool{"comments": true, "reviews-open": true, "warning": true}

// TestOnlyThePaletteReachesTheTerminal keeps the view inside the 16 ANSI
// colours and the four attributes, so it follows the user's terminal theme,
// and proves the width bounds above are asserted on styled output.
func TestOnlyThePaletteReachesTheTerminal(t *testing.T) {
	allowed := map[string]bool{"": true, "0": true, "1": true, "2": true, "4": true, "7": true}
	for n := 30; n <= 37; n++ {
		allowed[strconv.Itoa(n)] = true
	}
	for n := 90; n <= 97; n++ {
		allowed[strconv.Itoa(n)] = true
	}
	_, now := viewHarness()
	for name, apply := range fixtures(*now) {
		for _, w := range []int{30, 44, 100} {
			m, _ := viewHarness()
			apply(m)
			m.Width, m.Height = w, 40
			out := m.View().Content
			assertBounds(t, out, w, 40)
			if !strings.Contains(out, "\x1b[") {
				t.Errorf("%s at width %d rendered no styling at all", name, w)
			}
			if strings.ContainsRune(out, 0x07) {
				t.Errorf("%s at width %d emitted a BEL", name, w)
			}
			rest := out
			for _, seq := range sgrPattern.FindAllString(out, -1) {
				rest = strings.Replace(rest, seq, "", 1)
				if markdownFixtures[name] {
					continue
				}
				for _, param := range strings.Split(seq[2:len(seq)-1], ";") {
					if !allowed[param] {
						t.Errorf("%s at width %d emitted SGR parameter %q", name, w, param)
					}
				}
			}
			if strings.ContainsRune(rest, 0x1b) {
				t.Errorf("%s at width %d emitted a non-SGR escape sequence: %q", name, w, rest)
			}
		}
	}
}

// Golden renderings captured from main at 3835746, before any styling.
const goldenOverview44 = `GLANCE PR                  refreshed 12s ago
acme/service · feature/retry

#3630  OPEN
Re-request denied approvals when the
reviewer list changes
@author  feature/retry → main

143 commits · 8 files
+284  -76
Review: Changes requested

[Overview]  Comments  Reviews

 CHECKS  4 failed · 1 pending · 2 passed · 1
 neutral · 1 skipped · 1 unknown
›× unit tests                         failed
 × deploy preview                  timed out
 × e2e                             cancelled
 × migrate                   action required
 ◷ integration tests                 pending
 ✓ lint                               passed
 ✓ build                              passed
 · docs                              neutral
 · vendor                            skipped
 ? mystery                           unknown













r refresh  o browser  z zoom  q close`

const goldenReviews44 = `GLANCE PR                  refreshed 12s ago
acme/service · feature/retry

#3630  OPEN
Re-request denied approvals when the
reviewer list changes
@author  feature/retry → main

143 commits · 8 files
+284  -76
Review: Changes requested

Overview  Comments  [Reviews]

 fetched 30s ago
›@reviewer  APPROVED
 ▸ …are/about/here/handler.go:42  2 comments
 
 ▸ internal/ui/view.go  ✓~ 1c
 



















r refresh  o browser  z zoom  q close`
