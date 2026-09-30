package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

func TestBarRuns(t *testing.T) {
	for _, tc := range []struct {
		name  string
		c     model.CheckCounts
		width int
		want  [4]int
	}{
		{"screenshot PR", model.CheckCounts{Failed: 2, Passed: 33, Neutral: 1, Skipped: 22}, 40, [4]int{1, 0, 24, 15}},
		{"tiny failing share keeps a cell", model.CheckCounts{Failed: 1, Passed: 1000}, 40, [4]int{1, 0, 39, 0}},
		{"only passed fills the bar", model.CheckCounts{Passed: 5}, 30, [4]int{0, 0, 30, 0}},
		{"every group", model.CheckCounts{Failed: 1, Pending: 1, Passed: 1, Unknown: 1}, 23, [4]int{8, 5, 5, 5}},
		{"no checks", model.CheckCounts{}, 40, [4]int{}},
	} {
		got := barRuns(tc.c, tc.width)
		if got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
		sum := got[0] + got[1] + got[2] + got[3]
		if tc.c != (model.CheckCounts{}) && sum != tc.width {
			t.Errorf("%s: runs sum to %d, want %d", tc.name, sum, tc.width)
		}
	}
}

func TestCIBlockListsProblemsAndFoldsTheRest(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width, m.Height = 100, 60
	rows := strings.Join(bodyRows(m.View().Content), "\n")
	for _, want := range []string{"CI  ◷ 1 running · ✗ 4 failing", "× unit tests", "× migrate", "◷ integration tests",
		"▸ ✓ 2 passed · 1 neutral · 1 skipped · 1 unknown"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("missing %q in:\n%s", want, rows)
		}
	}
	for _, folded := range []string{"lint", "build", "docs", "vendor", "mystery"} {
		if strings.Contains(rows, folded) {
			t.Fatalf("%q must be folded:\n%s", folded, rows)
		}
	}
	if strings.Index(rows, "migrate") > strings.Index(rows, "integration tests") {
		t.Fatalf("failing rows come before running rows:\n%s", rows)
	}
	// The bar is one run of glyphs as wide as the content column.
	for _, r := range bodyRows(m.View().Content) {
		if strings.Contains(r, "━") && ansi.StringWidth(strings.TrimSpace(r)) != m.contentWidth() {
			t.Fatalf("bar width %d, want %d: %q", ansi.StringWidth(strings.TrimSpace(r)), m.contentWidth(), r)
		}
	}
}

func TestFoldRowExpandsAndSurvivesRefresh(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width, m.Height = 100, 60
	m.Cursor = 5 // 4 failing + 1 running rows come first; index 5 is the fold row
	apply(m, key("enter"))
	rows := strings.Join(bodyRows(m.View().Content), "\n")
	if !strings.Contains(rows, "▾ ✓ 2 passed") {
		t.Fatalf("fold row must show expanded:\n%s", rows)
	}
	lint, build, docs, vendor, mystery := strings.Index(rows, "✓ lint"), strings.Index(rows, "✓ build"),
		strings.Index(rows, "· docs"), strings.Index(rows, "· vendor"), strings.Index(rows, "? mystery")
	if lint < 0 || !(lint < build && build < docs && docs < vendor && vendor < mystery) {
		t.Fatalf("expanded checks must follow in snapshot order:\n%s", rows)
	}
	apply(m, SummaryResult{Generation: m.Generation, Data: m.Snapshot})
	if !strings.Contains(plainView(m), "▾ ✓ 2 passed") {
		t.Fatal("expansion must survive a refresh")
	}
	apply(m, key("enter"))
	if strings.Contains(plainView(m), "✓ lint") {
		t.Fatal("enter on the fold row must collapse it again")
	}
}

func TestCIHeadingCases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks []model.Check
		counts model.CheckCounts
		want   string
		noFold bool
	}{
		{"failing only", []model.Check{{Name: "a", State: model.CheckFailed}, {Name: "b", State: model.CheckPassed}},
			model.CheckCounts{Failed: 1, Passed: 1}, "CI  ✗ 1 failing", false},
		{"running only", []model.Check{{Name: "a", State: model.CheckPending}},
			model.CheckCounts{Pending: 1}, "CI  ◷ 1 running", true},
		{"all passed", []model.Check{{Name: "a", State: model.CheckPassed}, {Name: "b", State: model.CheckSkipped}},
			model.CheckCounts{Passed: 1, Skipped: 1}, "CI  ✓ 1 passed", false},
		{"only skipped", []model.Check{{Name: "a", State: model.CheckSkipped}},
			model.CheckCounts{Skipped: 1}, "CI  1 skipped", false},
		{"none", nil, model.CheckCounts{}, "CI  No checks", true},
	} {
		m, now := viewHarness()
		overviewFixture(m, *now)
		m.Width = 100
		m.Snapshot.Checks, m.Snapshot.CheckCounts = tc.checks, tc.counts
		plain := plainView(m)
		if !strings.Contains(plain, tc.want) {
			t.Errorf("%s: missing %q in:\n%s", tc.name, tc.want, plain)
		}
		if hasFold := strings.Contains(plain, "▸"); hasFold == tc.noFold {
			t.Errorf("%s: fold row present=%v, want %v", tc.name, hasFold, !tc.noFold)
		}
		if tc.name == "only skipped" && strings.Contains(plain, "✓") {
			t.Errorf("only skipped: nothing passed, so no ✓:\n%s", plain)
		}
	}
}

func TestReviewLineAndSectionOrder(t *testing.T) {
	for _, tc := range []struct{ decision, want string }{
		{"APPROVED", "Review  approved"}, {"CHANGES_REQUESTED", "Review  changes requested"},
		{"REVIEW_REQUIRED", "Review  review required"}, {"", "Review  no decision yet"}, {"WEIRD", "Review  no decision yet"},
	} {
		m, now := viewHarness()
		stackFixture(m, *now)
		m.Width, m.Height = 100, 60
		m.Snapshot.ReviewDecision = tc.decision
		rows := strings.Join(bodyRows(m.View().Content), "\n")
		ci, review, stack := strings.Index(rows, "CI  "), strings.Index(rows, tc.want), strings.Index(rows, "Stack  #3710 · 2/3 · onto main")
		if ci < 0 || review < 0 || stack < 0 || !(ci < review && review < stack) {
			t.Fatalf("%q: want CI, then %q, then the stack heading:\n%s", tc.decision, tc.want, rows)
		}
	}
}

func TestStackCursorMarkerAfterCI(t *testing.T) {
	m, now := viewHarness()
	stackFixture(m, *now)
	m.Width, m.Height = 100, 60
	// 4 failing + 1 running + fold row = 6 items before the stack; the shown
	// entry (#3630) is the second stack row.
	m.Cursor = 7
	row := ansi.Strip(stackRowLine(t, m.View().Content, "#3630"))
	if !strings.HasPrefix(row, "›") || []rune(row)[1] == '›' {
		t.Fatalf("cursor on the shown entry must draw exactly one marker: %q", row)
	}
	m.Cursor = 0
	if row := ansi.Strip(stackRowLine(t, m.View().Content, "#3630")); []rune(row)[1] != '›' {
		t.Fatalf("the shown entry keeps its own marker when the cursor is elsewhere: %q", row)
	}
}

func TestOpenOnTheFoldRowDoesNothing(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	opened := ""
	m.Open = func(u string) error { opened = u; return nil }
	m.Cursor = 5 // the fold row
	if cmd := apply(m, key("o")); cmd != nil {
		cmd()
	}
	if opened != "" {
		t.Fatalf("o on the fold row must not open anything, opened %q", opened)
	}
}

func TestSelectionFollowsTheFoldRowAcrossARefresh(t *testing.T) {
	m, now := viewHarness()
	stackFixture(m, *now)
	m.Width, m.Height = 100, 60
	m.Cursor = 5 // the fold row; the first stack row follows it
	next := m.Snapshot
	next.Checks = append([]model.Check(nil), m.Snapshot.Checks...)
	for i := range next.Checks {
		if next.Checks[i].Name == "integration tests" {
			next.Checks[i].State = model.CheckPassed
		}
	}
	next.CheckCounts.Pending, next.CheckCounts.Passed = 0, 3
	apply(m, SummaryResult{Generation: m.Generation, Data: next})
	apply(m, key("enter"))
	if m.Pinned != nil || !m.Expanded[foldKey] {
		t.Fatalf("enter after the refresh must toggle the fold row, not pin: cursor %d pinned %+v", m.Cursor, m.Pinned)
	}
}

func TestClickOnTheFoldRowTogglesIt(t *testing.T) {
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Width, m.Height = 100, 60
	y := -1
	for i, l := range strings.Split(plainView(m), "\n") {
		if strings.Contains(l, "▸") {
			y = i
		}
	}
	if apply(m, click(3, y)); y < 0 || !m.Expanded[foldKey] {
		t.Fatalf("clicking the fold row (row %d) must expand it", y)
	}
}

func TestCompactCounts(t *testing.T) {
	for n, want := range map[int]string{0: "0", 9999: "9999", 10000: "10k", 25365: "25k", 999999: "999k", 1000000: "1M", 2500000: "2M"} {
		if got := compact(n); got != want {
			t.Errorf("compact(%d) = %q, want %q", n, got, want)
		}
	}
}
