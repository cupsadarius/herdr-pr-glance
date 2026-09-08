package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

const (
	minWidth = 20
	// A thread row never gives its path less than minPathWidth columns, and
	// below compactThread columns the state tail switches to its short form.
	minPathWidth  = 12
	compactThread = 40
	narrowText    = "too narrow"
	defaultWidth  = 44
	defaultHigh   = 24
)

type footerHint struct {
	text string
	drop int
}

// footerHints appear in this order; the higher the drop rank, the sooner a hint
// is dropped when the pane is too narrow to list them all.
var footerHints = []footerHint{{"r refresh", 2}, {"o browser", 3}, {"z zoom", 1}, {"q close", 0}}

// footerLine fits as many control hints as the width allows, closing last.
func footerLine(w int) string {
	hints := footerHints
	for {
		texts := make([]string, len(hints))
		for i, h := range hints {
			texts[i] = h.text
		}
		for _, sep := range []string{"  ", " "} {
			if s := strings.Join(texts, sep); ansi.StringWidth(s) <= w {
				return s
			}
		}
		if len(hints) == 1 {
			return ansi.Truncate(hints[0].text, w, "…")
		}
		worst, idx := -1, 0
		for i, h := range hints {
			if h.drop > worst {
				worst, idx = h.drop, i
			}
		}
		hints = append(append([]footerHint{}, hints[:idx]...), hints[idx+1:]...)
	}
}

// rowTarget maps a rendered screen row to what a click on it selects. Tab
// labels also constrain the column span; body rows accept any column (x1 < 0).
type rowTarget struct {
	y, x0, x1 int
	tab       model.Section
	item      int
}

// bodyItem is one selectable entity in the body: a check, a comment, a review
// or a review thread. Items own the lines they occupy so the row map, the
// cursor and the scroll offset all agree on the layout.
type bodyItem struct {
	lines    []string
	url      string
	threadID string
}

// View renders the current state. Rendering is pure apart from recording the
// row-to-target map that handleMouse consults for hit testing; no other state
// is mutated here, and every derived value is recomputed from the model.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.MouseMode = tea.MouseModeCellMotion
	v.AltScreen = true
	return v
}

func (m *Model) size() (int, int) {
	w, h := m.Width, m.Height
	if w <= 0 {
		w = defaultWidth
	}
	if h <= 0 {
		h = defaultHigh
	}
	if h < 3 {
		h = 3
	}
	return w, h
}

// contentWidth reserves one column for the cursor gutter.
func (m *Model) contentWidth() int {
	w, _ := m.size()
	if w < minWidth {
		return minWidth - 1
	}
	return w - 1
}

func (m *Model) render() string {
	m.rows = nil
	w, h := m.size()
	if w < minWidth {
		return narrowText
	}
	foot := footerLine(w)
	if msg := m.emptyMessage(); msg != "" {
		out := []string{}
		for _, l := range m.titleLines(w) {
			out = append(out, l.text)
		}
		out = append(out, "")
		out = append(out, m.errorLines(w)...)
		out = append(out, wrapLines(msg, w)...)
		return strings.Join(append(pad(out, w, h), foot), "\n")
	}
	head := fitHeader(m.headerLines(w), h-2)
	lines, spans := renderBody(m.body(m.contentWidth()))
	markCursor(lines, spans, m.Cursor)
	bodyHigh := h - len(head) - 1
	off := clampOffset(m.Offset, len(lines), bodyHigh, spans, m.Cursor)
	out := make([]string, 0, h)
	for i, l := range head {
		for _, t := range l.tabs {
			t.y = i
			m.rows = append(m.rows, t)
		}
		out = append(out, ansi.Truncate(l.text, w, "…"))
	}
	for i := off; i < len(lines) && i-off < bodyHigh; i++ {
		m.rows = append(m.rows, rowTarget{y: len(out), x1: -1, item: itemAt(spans, i)})
		out = append(out, ansi.Truncate(lines[i], w, "…"))
	}
	return strings.Join(append(pad(out, w, h), foot), "\n")
}

// pad keeps the footer on the last row and the whole view within the height.
func pad(out []string, w, h int) []string {
	for len(out) < h-1 {
		out = append(out, "")
	}
	return out[:h-1]
}

type headerLine struct {
	text string
	prio int
	tabs []rowTarget
}

// Header priorities: 0 never drops, higher numbers are dropped first when the
// terminal is too short to show the whole header plus one body row.
const (
	prioError = iota + 1
	prioState
	prioTitle
	prioRepo
	prioAuthor
	prioReview
	prioCounts
	prioDiff
	prioBlank
)

func (m *Model) headerLines(w int) []headerLine {
	s := m.Snapshot
	out := m.titleLines(w)
	add := func(prio int, text string) { out = append(out, headerLine{text: text, prio: prio}) }
	addWrapped := func(prio int, text string) {
		for _, l := range wrapLines(text, w) {
			add(prio, l)
		}
	}
	repo := ""
	if s.PR != nil {
		repo = clean(s.PR.Repository)
	}
	add(prioRepo, repo+" · "+clean(m.Source.Branch))
	add(prioBlank, "")
	if s.PR != nil {
		add(prioState, fmt.Sprintf("#%d  %s", s.PR.Number, prState(s)))
	}
	addWrapped(prioTitle, clean(s.Title))
	add(prioAuthor, "@"+clean(s.Author)+"  "+headRef(s)+" → "+clean(s.BaseBranch))
	add(prioBlank, "")
	add(prioCounts, fmt.Sprintf("%d commits · %d files", s.Commits, s.ChangedFiles))
	add(prioDiff, fmt.Sprintf("+%d  -%d", s.Additions, s.Deletions))
	add(prioReview, "Review: "+reviewDecision(s.ReviewDecision))
	add(prioBlank, "")
	text, spans := tabLine(m.Section)
	out = append(out, headerLine{text: text, tabs: spans})
	for _, l := range m.errorLines(w) {
		add(prioError, l)
	}
	add(prioBlank, "")
	return out
}

func fitHeader(lines []headerLine, budget int) []headerLine {
	for len(lines) > budget {
		worst, idx := 0, -1
		for i, l := range lines {
			if l.prio > worst {
				worst, idx = l.prio, i
			}
		}
		if idx < 0 {
			break
		}
		lines = append(lines[:idx], lines[idx+1:]...)
	}
	return lines
}

var tabs = []struct {
	section model.Section
	label   string
}{{model.Overview, "Overview"}, {model.Comments, "Comments"}, {model.Reviews, "Reviews"}}

func tabLine(active model.Section) (string, []rowTarget) {
	var b strings.Builder
	var spans []rowTarget
	for i, t := range tabs {
		if i > 0 {
			b.WriteString("  ")
		}
		label := t.label
		if t.section == active {
			label = "[" + label + "]"
		}
		x0 := ansi.StringWidth(b.String())
		b.WriteString(label)
		spans = append(spans, rowTarget{x0: x0, x1: ansi.StringWidth(b.String()), tab: t.section, item: -1})
	}
	return b.String(), spans
}

// statusText keeps the age of the data even while a cooldown is running: that
// is exactly when knowing how old the summary is matters most.
func (m *Model) statusText() string {
	now := m.now()
	var parts []string
	switch {
	case !m.Snapshot.FetchedAt.IsZero():
		parts = append(parts, "refreshed "+ago(now.Sub(m.Snapshot.FetchedAt))+" ago")
		if m.SummaryStale() {
			parts = append(parts, "stale")
		}
	case m.SummaryLoading:
		parts = append(parts, "Loading…")
	}
	if now.Before(m.CooldownUntil) {
		d := m.CooldownUntil.Sub(now)
		parts = append(parts, fmt.Sprintf("rate limited, retry in %ds", int((d+time.Second-1)/time.Second)))
	}
	return strings.Join(parts, " · ")
}

// titleLines renders the name and status banner, moving the status onto its own
// wrapped line when it cannot share the first row.
func (m *Model) titleLines(w int) []headerLine {
	status := m.statusText()
	if status == "" || w-ansi.StringWidth("GLANCE PR")-ansi.StringWidth(status) >= 1 {
		return []headerLine{{text: fitStatus(w, "GLANCE PR", status)}}
	}
	out := []headerLine{{text: ansi.Truncate("GLANCE PR", w, "…")}}
	for _, l := range wrapLines(status, w) {
		out = append(out, headerLine{text: l, prio: prioError})
	}
	return out
}

func fitStatus(w int, left, right string) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if right == "" || gap < 1 {
		return ansi.Truncate(left, w, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

var emptyTexts = map[model.EmptyReason]string{
	model.NoWorkingPane: "No working pane",
	model.NoDirectory:   "No directory",
	model.NotGit:        "Not a Git repository",
	model.DetachedHEAD:  "Detached HEAD",
	model.UnbornHEAD:    "Unborn HEAD",
	model.NoPR:          "No PR for this branch",
}

// emptyMessage returns the state to show instead of a PR, or "" when a PR is known.
func (m *Model) emptyMessage() string {
	if t, ok := emptyTexts[m.Source.EmptyReason]; ok {
		return t
	}
	if m.Snapshot.PR != nil {
		return ""
	}
	if t, ok := emptyTexts[m.Snapshot.EmptyReason]; ok {
		return t
	}
	return "Loading…"
}

func (m *Model) errorLines(w int) []string {
	var out []string
	for _, err := range []error{m.SourceError, m.SummaryError, m.sectionError(), m.ActionError} {
		out = append(out, describeError(err, w)...)
	}
	if d := m.Discussions[m.Section]; d != nil && d.Warning != nil {
		out = append(out, wrapLines("cache: "+clean(d.Warning.Error()), w)...)
	}
	return out
}

func (m *Model) sectionError() error {
	if d := m.Discussions[m.Section]; d != nil {
		return d.Error
	}
	return nil
}

func describeError(err error, w int) []string {
	if err == nil {
		return nil
	}
	out := wrapLines(clean(err.Error()), w)
	var fe *model.FetchError
	if errors.As(err, &fe) && fe.Kind == model.AuthenticationError {
		out = append(out, wrapLines("run: gh auth login", w)...)
	}
	return out
}

func prState(s model.Snapshot) string {
	if s.Draft {
		return "DRAFT"
	}
	if s.State == "" {
		return "UNKNOWN"
	}
	return strings.ToUpper(clean(s.State))
}

func headRef(s model.Snapshot) string {
	head := clean(s.HeadBranch)
	if repo := clean(s.HeadRepository); repo != "" && s.PR != nil && repo != s.PR.Repository {
		return repo + ":" + head
	}
	return head
}

func reviewDecision(s string) string {
	r := []rune(strings.ToLower(strings.ReplaceAll(clean(s), "_", " ")))
	if len(r) == 0 {
		return "unavailable"
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// body returns the non-selectable lead lines and the selectable items of the
// active section, laid out for the given content width.
func (m *Model) body(w int) ([]string, []bodyItem) {
	switch m.Section {
	case model.Comments:
		return m.commentsBody(w)
	case model.Reviews:
		return m.reviewsBody(w)
	default:
		return m.overviewBody(w)
	}
}

func (m *Model) overviewBody(w int) ([]string, []bodyItem) {
	s := m.Snapshot
	if len(s.Checks) == 0 {
		return []string{"CHECKS", "No checks"}, nil
	}
	lead := wrapLines("CHECKS  "+countsText(s.CheckCounts), w)
	items := make([]bodyItem, 0, len(s.Checks))
	for _, c := range s.Checks {
		items = append(items, bodyItem{lines: []string{checkRow(c, w)}, url: c.URL})
	}
	return lead, items
}

func countsText(c model.CheckCounts) string {
	var parts []string
	for _, p := range []struct {
		n     int
		label string
	}{{c.Failed, "failed"}, {c.Pending, "pending"}, {c.Passed, "passed"},
		{c.Neutral, "neutral"}, {c.Skipped, "skipped"}, {c.Unknown, "unknown"}} {
		if p.n > 0 {
			parts = append(parts, strconv.Itoa(p.n)+" "+p.label)
		}
	}
	if len(parts) == 0 {
		return "No checks"
	}
	return strings.Join(parts, " · ")
}

func checkGlyph(s model.CheckState) string {
	switch s {
	case model.CheckFailed, model.CheckTimedOut, model.CheckCancelled, model.CheckActionRequired:
		return "×"
	case model.CheckPending:
		return "◷"
	case model.CheckPassed:
		return "✓"
	case model.CheckNeutral, model.CheckSkipped:
		return "·"
	default:
		return "?"
	}
}

func checkRow(c model.Check, w int) string {
	left := checkGlyph(c.State) + " " + clean(c.Name)
	right := strings.ReplaceAll(string(c.State), "_", " ")
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return ansi.Truncate(left, w, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

// sectionLead reports loading progress and the age of the cached discussion.
func (m *Model) sectionLead(s model.Section) []string {
	var out []string
	d := m.Discussions[s]
	if d == nil || d.Loading {
		out = append(out, "Loading…")
	}
	if d != nil && d.Data != nil {
		age := "fetched " + ago(m.now().Sub(d.Data.FetchedAt)) + " ago"
		if m.DiscussionStale(s) {
			age += " (stale)"
		}
		out = append(out, age)
	}
	return out
}

func (m *Model) discussionData(s model.Section) *model.Discussion {
	if d := m.Discussions[s]; d != nil {
		return d.Data
	}
	return nil
}

func (m *Model) commentsBody(w int) ([]string, []bodyItem) {
	lead := m.sectionLead(model.Comments)
	d := m.discussionData(model.Comments)
	if d == nil {
		return lead, nil
	}
	if len(d.Comments) == 0 {
		return append(lead, "No comments"), nil
	}
	items := make([]bodyItem, 0, len(d.Comments))
	for _, c := range d.Comments {
		items = append(items, bodyItem{lines: m.commentLines(c, w, ""), url: c.URL})
	}
	return lead, items
}

func (m *Model) commentLines(c model.Comment, w int, indent string) []string {
	lines := []string{indent + "@" + clean(c.Author) + " · " + ago(m.now().Sub(c.CreatedAt)) + " ago"}
	for _, l := range wrapLines(clean(c.Body), w-len(indent)) {
		lines = append(lines, indent+l)
	}
	return append(lines, "")
}

func (m *Model) reviewsBody(w int) ([]string, []bodyItem) {
	lead := m.sectionLead(model.Reviews)
	d := m.discussionData(model.Reviews)
	if d == nil {
		return lead, nil
	}
	items := make([]bodyItem, 0, len(d.Reviews)+len(d.Threads))
	for _, r := range d.Reviews {
		row := "@" + clean(r.Author) + "  " + clean(r.State)
		items = append(items, bodyItem{lines: []string{ansi.Truncate(row, w, "…")}, url: r.URL})
	}
	for _, t := range d.Threads {
		items = append(items, m.threadItem(t, w))
	}
	if len(items) == 0 {
		return append(lead, "No reviews"), nil
	}
	return lead, items
}

func (m *Model) threadItem(t model.ReviewThread, w int) bodyItem {
	head := "▸ "
	if m.Expanded[t.ID] {
		head = "▾ "
	}
	where := clean(t.Path)
	if t.Line != nil {
		where += ":" + strconv.Itoa(*t.Line)
	}
	avail := w - ansi.StringWidth(head)
	tail := threadTail(t, w < compactThread)
	if avail-ansi.StringWidth(tail) < minPathWidth {
		tail = threadTail(t, true)
	}
	room := avail - ansi.StringWidth(tail)
	if room < minPathWidth {
		room = min(minPathWidth, avail)
		tail = ansi.Truncate(tail, avail-room, "…")
	}
	if room < 1 {
		room = 1
	}
	lines := []string{head + truncatePath(where, room) + tail}
	if m.Expanded[t.ID] {
		for _, c := range t.Comments {
			lines = append(lines, m.commentLines(c, w, "  ")...)
		}
		return bodyItem{lines: lines, url: t.URL, threadID: t.ID}
	}
	return bodyItem{lines: append(lines, ""), url: t.URL, threadID: t.ID}
}

// threadTail is the state and reply count shown after a thread's path. The
// compact form keeps a narrow row readable: ✓ resolved, ~ outdated, Nc replies.
func threadTail(t model.ReviewThread, compact bool) string {
	if compact {
		flags := ""
		if t.Resolved {
			flags += "✓"
		}
		if t.Outdated {
			flags += "~"
		}
		if flags != "" {
			flags += " "
		}
		return "  " + flags + strconv.Itoa(len(t.Comments)) + "c"
	}
	var tags []string
	if t.Resolved {
		tags = append(tags, "resolved")
	}
	if t.Outdated {
		tags = append(tags, "outdated")
	}
	tail := ""
	if len(tags) > 0 {
		tail = "  (" + strings.Join(tags, ", ") + ")"
	}
	return tail + fmt.Sprintf("  %d comments", len(t.Comments))
}

// renderBody flattens lead lines and items into screen lines, marking the
// cursor in the gutter, and reports each item's [start,end) line span.
func renderBody(lead []string, items []bodyItem) ([]string, [][2]int) {
	lines := make([]string, 0, len(lead)+len(items))
	for _, l := range lead {
		lines = append(lines, " "+l)
	}
	spans := make([][2]int, len(items))
	for i, it := range items {
		spans[i][0] = len(lines)
		for _, l := range it.lines {
			lines = append(lines, " "+l)
		}
		spans[i][1] = len(lines)
	}
	return lines, spans
}

func markCursor(lines []string, spans [][2]int, cursor int) {
	if cursor < 0 || cursor >= len(spans) {
		return
	}
	if at := spans[cursor][0]; at < len(lines) {
		lines[at] = "›" + strings.TrimPrefix(lines[at], " ")
	}
}

func itemAt(spans [][2]int, line int) int {
	for i, s := range spans {
		if line >= s[0] && line < s[1] {
			return i
		}
	}
	return -1
}

func clampOffset(off, total, high int, spans [][2]int, cursor int) int {
	if high <= 0 || total <= high {
		return 0
	}
	if cursor >= 0 && cursor < len(spans) {
		// Selecting the first item reveals the lead lines above it.
		start := spans[cursor][0]
		if cursor == 0 {
			start = 0
		}
		if start < off {
			off = start
		}
		if spans[cursor][1] > off+high {
			off = spans[cursor][1] - high
		}
	}
	if off > total-high {
		off = total - high
	}
	if off < 0 {
		off = 0
	}
	return off
}

// clean neutralises remote text: no escape sequences, no control characters,
// no invisible or bidirectional-override runes that could disguise a path or a
// handle, tabs expanded, line breaks preserved.
func clean(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range ansi.Strip(s) {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		case r >= 0x200b && r <= 0x200f, r == 0x2028, r == 0x2029:
		case r >= 0x202a && r <= 0x202e, r >= 0x2060 && r <= 0x2064:
		case r >= 0x2066 && r <= 0x2069, r == 0xfeff:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// wrapLines wraps to width while leaving short lines — code blocks and their
// indentation included — exactly as written.
func wrapLines(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if ansi.StringWidth(line) <= w {
			out = append(out, line)
			continue
		}
		out = append(out, strings.Split(ansi.Hardwrap(ansi.Wordwrap(line, w, "-"), w, true), "\n")...)
	}
	return out
}

// truncatePath drops leading path segments so the file name stays visible.
func truncatePath(s string, w int) string {
	if width := ansi.StringWidth(s); width > w {
		return ansi.TruncateLeft(s, width-w+1, "…")
	}
	return s
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		if d < 0 {
			d = 0
		}
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	default:
		return strconv.Itoa(int(d/(24*time.Hour))) + "d"
	}
}
