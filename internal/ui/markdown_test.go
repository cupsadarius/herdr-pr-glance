package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

var errRenderFailed = errors.New("render failed")

// richBody exercises the Markdown a review comment realistically carries: a
// hidden HTML comment, a collapsed section, a heading, emphasis, inline code,
// a list, a fenced block and a table.
const richBody = "<!-- hidden reviewer note: do not ship -->\n" +
	"## Summary\n\n" +
	"<details><summary>Click to expand</summary>\n\nInner detail text.\n\n</details>\n\n" +
	"Some **bold** text with `inline code`.\n\n" +
	"- first bullet\n- second bullet\n\n" +
	"```go\nfunc main() { println(\"hi\") }\n```\n\n" +
	"| col | other |\n| --- | ----- |\n| a   | b     |\n"

// markdownModel puts one body in the Comments section of the usual fixture.
func markdownModel(t *testing.T, body string) *Model {
	t.Helper()
	m, now := viewHarness()
	overviewFixture(m, *now)
	m.Section = model.Comments
	m.Discussions[model.Comments] = &DiscussionState{Data: &model.Discussion{
		Section: model.Comments, Complete: true, FetchedAt: *now,
		Comments: []model.Comment{{ID: "c1", Author: "octocat", Body: body, CreatedAt: now.Add(-time.Hour)}},
	}}
	m.Height = 60
	return m
}

// countRenders replaces the Glamour seam with a counter around it.
func countRenders(m *Model, calls *int) {
	inner := m.md.render
	m.md.render = func(text string, w int) (string, error) {
		*calls++
		return inner(text, w)
	}
}

func TestMarkdownBodyRendersStructure(t *testing.T) {
	for _, w := range []int{30, 44, 100} {
		m := markdownModel(t, richBody)
		m.Width = w
		out := m.View().Content
		assertBounds(t, out, w, m.Height)
		plain := ansi.Strip(out)
		if strings.Contains(plain, "hidden reviewer note") {
			t.Errorf("width %d: an HTML comment reached the pane:\n%s", w, plain)
		}
		for _, want := range []string{"Summary", "Click to expand", "Inner detail text.", "bold",
			"inline code", "• first bullet", "• second bullet", "func main()", "col", "other"} {
			if !strings.Contains(plain, want) {
				t.Errorf("width %d: missing %q in:\n%s", w, want, plain)
			}
		}
		if strings.Contains(plain, "```") || strings.Contains(plain, "<details>") {
			t.Errorf("width %d: raw Markdown syntax survived:\n%s", w, plain)
		}
	}
	m := markdownModel(t, richBody)
	m.Width = 100
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, `func main() { println("hi") }`) {
		t.Errorf("a fenced block must keep its content verbatim when it fits:\n%s", plain)
	}
	if !strings.Contains(plain, "│") {
		t.Errorf("a table must render as a table:\n%s", plain)
	}
}

// TestMarkdownBodyKeepsTheAuthorIndent pins the alignment choice: Glamour's own
// two-column margin is removed so a body sits under its @author line.
func TestMarkdownBodyKeepsTheAuthorIndent(t *testing.T) {
	m := markdownModel(t, "hello body")
	m.Width = 100
	rows := bodyRows(m.View().Content)
	var body string
	for _, r := range rows {
		if strings.Contains(r, "hello body") {
			body = r
			break
		}
	}
	if body == "" {
		t.Fatalf("body missing from:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.HasPrefix(body, " hello body") {
		t.Errorf("body must align with the one-column gutter, got %q", body)
	}
}

func TestMarkdownRendersOncePerBodyAndWidth(t *testing.T) {
	m := markdownModel(t, richBody)
	m.Width = 44
	calls := 0
	countRenders(m, &calls)
	m.View()
	if calls != 1 {
		t.Fatalf("first render made %d Glamour calls, want 1", calls)
	}
	for i := 0; i < 5; i++ {
		m.View()
	}
	if calls != 1 {
		t.Errorf("re-rendering the same body at the same width made %d calls", calls)
	}
	m.Width = 60
	m.View()
	if calls != 2 {
		t.Errorf("a width change must re-render, calls %d", calls)
	}
	m.Width = 44
	m.View()
	if calls != 2 {
		t.Errorf("returning to a rendered width must reuse the cache, calls %d", calls)
	}
}

func TestMarkdownFallsBackToPlainText(t *testing.T) {
	m := markdownModel(t, "a body **with** markdown that must survive a broken renderer")
	m.Width = 44
	m.md.render = func(string, int) (string, error) { return "", errRenderFailed }
	out := m.View().Content
	assertBounds(t, out, 44, m.Height)
	if !strings.Contains(ansi.Strip(out), "a body **with** markdown") {
		t.Errorf("a failed render must fall back to the plain body:\n%s", ansi.Strip(out))
	}
}

func TestHugeBodiesSkipMarkdown(t *testing.T) {
	m := markdownModel(t, strings.Repeat("paste ", maxMarkdownRunes/3))
	m.Width = 44
	calls := 0
	countRenders(m, &calls)
	out := m.View().Content
	assertBounds(t, out, 44, m.Height)
	if calls != 0 {
		t.Errorf("a body past the size guard must skip Glamour, calls %d", calls)
	}
	if !strings.Contains(ansi.Strip(out), "paste paste") {
		t.Errorf("the plain body must still be shown:\n%s", ansi.Strip(out))
	}
}

func TestMarkdownPathDropsRemoteControlSequences(t *testing.T) {
	m := markdownModel(t, "look at \x1b[31mthis\x07 and ‮gg.exe‬ plus zero​width")
	for _, w := range []int{30, 44, 100} {
		m.Width = w
		out := m.View().Content
		for _, r := range []rune{0x07, 0x200b, 0x202c, 0x202e} {
			if strings.ContainsRune(out, r) {
				t.Errorf("width %d: rune %U survived the Markdown path: %q", w, r, out)
			}
		}
		body := styledLine(t, out, "look at this")
		if strings.Contains(body, "\x1b[31m") {
			t.Errorf("width %d: a remote colour survived: %q", w, body)
		}
		if plain := ansi.Strip(out); !strings.Contains(plain, "look at this") {
			t.Errorf("width %d: sanitization ate the text:\n%s", w, plain)
		}
	}
}
