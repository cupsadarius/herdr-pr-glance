// Throwaway spike: three Overview layouts rendered from fake data.
// go run ./spike/variants -v signals|ready|feed -s busy|green -w 64
package main

import (
	"flag"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"charm.land/lipgloss/v2/tree"
)

type check struct{ name, state string }
type review struct{ who, state, ago string }
type thread struct {
	path   string
	line   int
	who    string
	body   string
	ago    string
	solved bool
}
type event struct{ ago, kind, who, text string }
type stackPR struct {
	n            int
	title, state string
	here         bool
}

type data struct {
	number                      int
	repo, title, author, state  string
	commits, files, add, del    int
	checks                      []check
	reviews                     []review // includes requested reviewers with state "PENDING"
	threads                     []thread
	events                      []event
	stack                       []stackPR
	behind                      int
	conflicts                   bool
	head, base, fetched, pushed string
}

var (
	base   = lipgloss.NewStyle()
	bold   = base.Bold(true)
	faint  = base.Faint(true)
	red    = base.Foreground(lipgloss.Color("1"))
	green  = base.Foreground(lipgloss.Color("2"))
	yellow = base.Foreground(lipgloss.Color("3"))
	mag    = base.Foreground(lipgloss.Color("5"))
	cyan   = base.Foreground(lipgloss.Color("6"))
)

func color(state string) lipgloss.Style {
	switch state {
	case "failed", "CHANGES_REQUESTED", "bad":
		return red
	case "pending", "PENDING", "REVIEW_REQUIRED", "warn":
		return yellow
	case "passed", "APPROVED", "ok":
		return green
	}
	return faint
}

func glyph(state string) string {
	switch state {
	case "failed", "CHANGES_REQUESTED", "bad":
		return "✗"
	case "pending", "PENDING", "REVIEW_REQUIRED", "warn":
		return "●"
	case "passed", "APPROVED", "ok":
		return "✓"
	case "COMMENTED":
		return "💬"
	}
	return "○"
}

func header(d data, w int) string {
	badge := bold.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("2")).Padding(0, 1).Render(d.state)
	left := badge + " " + bold.Render(fmt.Sprintf("#%d", d.number)) + "  " + faint.Render(d.repo)
	right := faint.Render(d.fetched)
	gap := max(1, w-lipgloss.Width(left)-lipgloss.Width(right))
	title := bold.Width(w).Render(d.title)
	meta := cyan.Render("@"+d.author) + faint.Render(fmt.Sprintf(" · %d commits · %d files · ", d.commits, d.files)) +
		green.Render(fmt.Sprintf("+%d", d.add)) + " " + red.Render(fmt.Sprintf("-%d", d.del))
	branch := faint.Render(d.head + " → " + d.base)
	return lipgloss.JoinVertical(lipgloss.Left, left+strings.Repeat(" ", gap)+right, title, meta, branch)
}

func footer(w int) string {
	keys := [][2]string{{"r", "refresh"}, {"o", "browser"}, {"tab", "section"}, {"?", "help"}, {"q", "close"}}
	var parts []string
	for _, k := range keys {
		parts = append(parts, bold.Render(k[0])+" "+faint.Render(k[1]))
	}
	return strings.Join(parts, "  ")
}

func counts(d data) (fail, pend, pass, other int) {
	for _, c := range d.checks {
		switch c.state {
		case "failed":
			fail++
		case "pending":
			pend++
		case "passed":
			pass++
		default:
			other++
		}
	}
	return
}

func open(d data) (n int) {
	for _, t := range d.threads {
		if !t.solved {
			n++
		}
	}
	return
}

func approvals(d data) (n int) {
	for _, r := range d.reviews {
		if r.state == "APPROVED" {
			n++
		}
	}
	return
}

func section(title string, w int) string {
	t := bold.Render(title) + " "
	return t + faint.Render(strings.Repeat("─", max(0, w-lipgloss.Width(t))))
}

func stackTree(d data) string {
	if len(d.stack) == 0 {
		return ""
	}
	t := tree.Root(faint.Render(d.base)).Enumerator(tree.RoundedEnumerator).EnumeratorStyle(faint)
	for _, s := range d.stack {
		label := fmt.Sprintf("#%d %s", s.n, s.title)
		st := faint
		if s.here {
			st, label = bold, label+cyan.Render("  ← here")
		}
		t.Child(color(s.state).Render(glyph(s.state)) + " " + st.Render(label))
	}
	return t.String()
}

// ── Variant A: signal tiles + "needs you" ────────────────────────────────────

func signals(d data, w int) string {
	fail, pend, pass, _ := counts(d)
	ci := "ok"
	ciDetail := fmt.Sprintf("%d passed", pass)
	if fail > 0 {
		ci, ciDetail = "bad", fmt.Sprintf("%d failing", fail)
	} else if pend > 0 {
		ci, ciDetail = "warn", fmt.Sprintf("%d running", pend)
	}
	rv := "warn"
	rvDetail := fmt.Sprintf("%d/2 approvals", approvals(d))
	for _, r := range d.reviews {
		if r.state == "CHANGES_REQUESTED" {
			rv, rvDetail = "bad", "changes req."
		}
	}
	if approvals(d) >= 2 {
		rv = "ok"
	}
	th := "ok"
	thDetail := "all resolved"
	if n := open(d); n > 0 {
		th, thDetail = "warn", fmt.Sprintf("%d open", n)
	}
	mg, mgDetail := "ok", "clean"
	if d.conflicts {
		mg, mgDetail = "bad", "conflicts"
	} else if d.behind > 0 {
		mg, mgDetail = "warn", fmt.Sprintf("%d behind", d.behind)
	}

	cols := 4
	if w < 60 {
		cols = 2
	}
	tw := w/cols - 0
	tile := func(label, state, detail string) string {
		c := color(state)
		body := c.Bold(true).Render(glyph(state)+" "+label) + "\n" + c.Render(detail)
		return base.Border(lipgloss.RoundedBorder()).BorderForeground(c.GetForeground()).
			Width(tw).Padding(0, 1).Render(body)
	}
	tiles := []string{tile("CI", ci, ciDetail), tile("Review", rv, rvDetail), tile("Threads", th, thDetail), tile("Merge", mg, mgDetail)}
	var rows []string
	for i := 0; i < len(tiles); i += cols {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, tiles[i:i+cols]...))
	}
	out := []string{header(d, w), "", lipgloss.JoinVertical(lipgloss.Left, rows...), ""}

	var need []string
	for _, c := range d.checks {
		if c.state == "failed" {
			need = append(need, red.Render("✗ ")+c.name+faint.Render("  check failed"))
		}
	}
	for _, t := range d.threads {
		if !t.solved {
			need = append(need, yellow.Render("● ")+cyan.Render(fmt.Sprintf("%s:%d", t.path, t.line))+faint.Render("  @"+t.who+" "+t.ago))
		}
	}
	for _, r := range d.reviews {
		if r.state == "PENDING" {
			need = append(need, faint.Render("○ waiting on ")+cyan.Render("@"+r.who))
		}
	}
	if len(need) == 0 {
		need = []string{green.Render("✓ nothing blocks this PR")}
	}
	out = append(out, section("Needs attention", w))
	out = append(out, need...)
	if s := stackTree(d); s != "" {
		out = append(out, "", section(fmt.Sprintf("Stack %d/%d", stackPos(d), len(d.stack)), w), s)
	}
	out = append(out, "", section("Latest", w))
	for _, e := range d.events[:min(4, len(d.events))] {
		out = append(out, faint.Render(fmt.Sprintf("%-4s", e.ago))+" "+cyan.Render("@"+e.who)+" "+e.text)
	}
	return strings.Join(out, "\n")
}

func stackPos(d data) int {
	for i, s := range d.stack {
		if s.here {
			return i + 1
		}
	}
	return 0
}

// ── Variant B: merge-readiness checklist + reviewers table + threads tree ───

func ready(d data, w int) string {
	fail, pend, pass, other := counts(d)
	type gate struct{ state, label, detail string }
	ciState, ciDet := "ok", fmt.Sprintf("%d passed · %d skipped/neutral", pass, other)
	if fail > 0 {
		ciState, ciDet = "bad", fmt.Sprintf("%d failing · %d running · %d passed", fail, pend, pass)
	} else if pend > 0 {
		ciState = "warn"
	}
	rvState := "warn"
	if approvals(d) >= 2 {
		rvState = "ok"
	}
	thState := "ok"
	if open(d) > 0 {
		thState = "warn"
	}
	upState, upDet := "ok", "up to date with "+d.base
	if d.behind > 0 {
		upState, upDet = "warn", fmt.Sprintf("%d commits behind %s", d.behind, d.base)
	}
	cfState, cfDet := "ok", "no conflicts"
	if d.conflicts {
		cfState, cfDet = "bad", "conflicts with "+d.base
	}
	gates := []gate{
		{ciState, "Checks", ciDet},
		{rvState, "Approvals", fmt.Sprintf("%d of 2 required", approvals(d))},
		{thState, "Conversations", fmt.Sprintf("%d unresolved of %d", open(d), len(d.threads))},
		{upState, "Branch", upDet},
		{cfState, "Mergeable", cfDet},
	}
	done := 0
	for _, g := range gates {
		if g.state == "ok" {
			done++
		}
	}
	// segmented bar: one segment per gate
	seg := (w - len(gates) + 1) / len(gates)
	var bar []string
	for _, g := range gates {
		bar = append(bar, color(g.state).Render(strings.Repeat("━", seg)))
	}
	verdict := yellow.Bold(true).Render(fmt.Sprintf("%d of %d to merge", done, len(gates)))
	if done == len(gates) {
		verdict = green.Bold(true).Render("Ready to merge")
	}
	out := []string{header(d, w), "", verdict, strings.Join(bar, " "), ""}
	for _, g := range gates {
		c := color(g.state)
		out = append(out, c.Render(glyph(g.state))+" "+bold.Width(15).Render(g.label)+c.Render(g.detail))
	}
	if fail > 0 {
		for _, c := range d.checks {
			if c.state == "failed" {
				out = append(out, faint.Render("  └ ")+red.Render(c.name))
			}
		}
	}

	out = append(out, "", section("Reviewers", w))
	tb := table.New().Border(lipgloss.HiddenBorder()).BorderTop(false).BorderBottom(false).
		BorderLeft(false).BorderRight(false).BorderColumn(false).BorderHeader(false).Width(w).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := base.PaddingRight(2)
			if row < 0 || row >= len(d.reviews) {
				return s
			}
			switch col {
			case 0:
				return s.Inherit(cyan)
			case 1:
				return s.Inherit(color(d.reviews[row].state))
			}
			return s.Inherit(faint)
		})
	for _, r := range d.reviews {
		label := strings.ToLower(strings.ReplaceAll(r.state, "_", " "))
		if r.state == "PENDING" {
			label = "requested"
		}
		tb.Row("@"+r.who, glyph(r.state)+" "+label, r.ago)
	}
	out = append(out, tb.String())

	if len(d.threads) > 0 {
		out = append(out, "", section(fmt.Sprintf("Threads  %d open", open(d)), w))
		byFile := map[string]*tree.Tree{}
		var order []string
		root := tree.New().Enumerator(tree.RoundedEnumerator).EnumeratorStyle(faint)
		for _, t := range d.threads {
			if t.solved {
				continue
			}
			f, ok := byFile[t.path]
			if !ok {
				f = tree.Root(cyan.Render(t.path)).Enumerator(tree.RoundedEnumerator).EnumeratorStyle(faint)
				byFile[t.path] = f
				order = append(order, t.path)
			}
			body := t.body
			if lipgloss.Width(body) > w-22 {
				body = body[:w-23] + "…"
			}
			f.Child(faint.Render(fmt.Sprintf("L%-4d", t.line)) + cyan.Render("@"+t.who) + " " + body)
		}
		for _, p := range order {
			root.Child(byFile[p])
		}
		out = append(out, root.String())
	}
	if s := stackTree(d); s != "" {
		out = append(out, "", section(fmt.Sprintf("Stack %d/%d", stackPos(d), len(d.stack)), w), s)
	}
	return strings.Join(out, "\n")
}

// ── Variant C: status strip + activity timeline ──────────────────────────────

func feed(d data, w int) string {
	fail, pend, pass, _ := counts(d)
	pill := func(st lipgloss.Style, s string) string {
		return st.Reverse(true).Bold(true).Padding(0, 1).Render(s)
	}
	ci := pill(green, fmt.Sprintf("✓ CI %d", pass))
	if fail > 0 {
		ci = pill(red, fmt.Sprintf("✗ CI %d/%d", fail, len(d.checks)))
	} else if pend > 0 {
		ci = pill(yellow, fmt.Sprintf("● CI %d", pend))
	}
	rv := pill(yellow, fmt.Sprintf("◐ %d/2 ✓", approvals(d)))
	if approvals(d) >= 2 {
		rv = pill(green, "✓ approved")
	}
	th := pill(green, "✓ threads")
	if n := open(d); n > 0 {
		th = pill(yellow, fmt.Sprintf("💬 %d open", n))
	}
	mg := pill(green, "⇡ clean")
	if d.conflicts {
		mg = pill(red, "⚠ conflicts")
	} else if d.behind > 0 {
		mg = pill(yellow, fmt.Sprintf("⇣ %d behind", d.behind))
	}
	strip := strings.Join([]string{ci, rv, th, mg}, " ")
	out := []string{header(d, w), "", strip, ""}
	if s := stackTree(d); s != "" {
		out = append(out, s, "")
	}
	out = append(out, activity(d, w, true)...)
	return strings.Join(out, "\n")
}

func activity(d data, w int, gaps bool) []string {
	out := []string{section("Activity", w)}
	gut := faint.Render("│")
	for i, e := range d.events {
		var dot string
		switch e.kind {
		case "approve":
			dot = green.Render("✓")
		case "changes", "ci-fail":
			dot = red.Render("✗")
		case "comment":
			dot = yellow.Render("●")
		case "push":
			dot = mag.Render("↑")
		default:
			dot = faint.Render("○")
		}
		out = append(out, dot+" "+faint.Render(fmt.Sprintf("%-4s", e.ago))+cyan.Render("@"+e.who)+" "+e.text)
		if gaps && i < len(d.events)-1 {
			out = append(out, gut)
		}
	}
	return out
}

// ── Variant D: B + C's activity ──────────────────────────────────────────────

func combo(d data, w int) string {
	return ready(d, w) + "\n\n" + strings.Join(activity(d, w, true), "\n")
}

func main() {
	v := flag.String("v", "signals", "signals|ready|feed")
	s := flag.String("s", "busy", "busy|green")
	w := flag.Int("w", 64, "width")
	flag.Parse()
	d := busy
	if *s == "green" {
		d = allGreen
	}
	if *v == "combo" {
		if _, err := tea.NewProgram(&pane{d: d}).Run(); err != nil {
			fmt.Println(err)
		}
		return
	}
	render := map[string]func(data, int) string{"signals": signals, "ready": ready, "feed": feed, "combo": combo}[*v]
	lipgloss.Println(render(d, *w) + "\n\n" + footer(*w))
}

var busy = data{
	number: 4317, repo: "optura-ai/intent", state: "OPEN",
	title:  "perf(agents): read portfolio tool use cases with lean pipeline queries",
	author: "darius-optura", commits: 11, files: 15, add: 2118, del: 605,
	head: "perf/lean-portfolio", base: "main", fetched: "14s ago", behind: 3,
	checks: append(repeat("passed", 27), check{"e2e / portfolio", "failed"}, check{"lint", "failed"},
		check{"integration", "pending"}, check{"preview", "skipped"}),
	reviews: []review{{"ana-m", "APPROVED", "2h"}, {"radu", "CHANGES_REQUESTED", "40m"}, {"ioana", "PENDING", ""}},
	threads: []thread{
		{"agents/portfolio/query.go", 88, "radu", "this N+1 comes back when the filter is empty", "40m", false},
		{"agents/portfolio/query.go", 141, "radu", "can we keep the old index hint?", "38m", false},
		{"agents/tools/usecases.go", 12, "ana-m", "nit: name", "2h", true},
		{"db/migrations/0142_lean.sql", 4, "ana-m", "needs a down migration", "2h", false},
	},
	events: []event{
		{"6m", "ci-fail", "github", "e2e / portfolio failed"},
		{"12m", "push", "darius-optura", "pushed 2 commits"},
		{"38m", "comment", "radu", "commented on query.go:141"},
		{"40m", "changes", "radu", "requested changes"},
		{"2h", "approve", "ana-m", "approved"},
		{"3h", "push", "darius-optura", "opened the PR"},
	},
	stack: []stackPR{
		{4301, "refactor(agents): split portfolio tools", "APPROVED", false},
		{4317, "perf(agents): lean pipeline queries", "REVIEW_REQUIRED", true},
		{4320, "feat(agents): portfolio use-case cache", "PENDING", false},
	},
}

var allGreen = data{
	number: 4317, repo: "optura-ai/intent", state: "OPEN",
	title:  "perf(agents): read portfolio tool use cases with lean pipeline queries",
	author: "darius-optura", commits: 11, files: 15, add: 2118, del: 605,
	head: "perf/lean-portfolio", base: "main", fetched: "14s ago",
	checks:  append(repeat("passed", 30), check{"a", "neutral"}, check{"b", "skipped"}),
	reviews: []review{{"ioana", "PENDING", ""}, {"radu", "PENDING", ""}},
	events: []event{
		{"12m", "push", "darius-optura", "pushed 2 commits"},
		{"3h", "push", "darius-optura", "opened the PR"},
	},
}

func repeat(state string, n int) (out []check) {
	for i := range n {
		out = append(out, check{fmt.Sprintf("job %d", i), state})
	}
	return
}

// pane pins header and footer; only the body scrolls.
type pane struct {
	d  data
	w  int
	vp viewport.Model
}

func (p *pane) Init() tea.Cmd { return nil }

func (p *pane) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.w = msg.Width - 1
		h := lipgloss.Height(header(p.d, p.w))
		p.vp = viewport.New(viewport.WithWidth(p.w), viewport.WithHeight(msg.Height-h-3))
		body := strings.TrimPrefix(combo(p.d, p.w), header(p.d, p.w)+"\n")
		p.vp.SetContent(body)
		return p, nil
	case tea.KeyPressMsg:
		if msg.String() == "q" {
			return p, tea.Quit
		}
	}
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return p, cmd
}

func (p *pane) View() tea.View {
	if p.w == 0 {
		return tea.NewView("")
	}
	more := ""
	if !p.vp.AtBottom() {
		more = faint.Render(fmt.Sprintf("  ↓ %d%%", int(p.vp.ScrollPercent()*100)))
	}
	v := tea.NewView(header(p.d, p.w) + "\n" + p.vp.View() + "\n\n" + footer(p.w) + more)
	v.AltScreen = true
	return v
}
