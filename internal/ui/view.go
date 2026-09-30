package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	bkey "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	appName       = "GLANCE PR"
	defaultWidth  = 44
	defaultHigh   = 24
)

type footerHint struct {
	text string
	drop int
}

// footerLine fits as many control hints as the width allows, closing last.
func footerLine(w int, hints []footerHint) string {
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
// labels and the PR number also constrain the column span; body rows and the
// title accept any column (x1 < 0). A target with a url opens it on click.
type rowTarget struct {
	y, x0, x1 int
	tab       model.Section
	item      int
	url       string
}

// bodyItem is one selectable entity in the body: a check, the CI fold row, a
// stack entry, a comment, a review or a review thread. Items own the lines
// they occupy so the row map, the cursor and the scroll offset all agree on
// the layout.
type bodyItem struct {
	// head lines belong to no item: they introduce the group this item starts,
	// so the cursor and the row map still address the item itself.
	head     []string
	lines    []string
	url      string
	threadID string
	// pin is the stack entry this row shows, if any; selecting it pins it.
	pin *model.PR
	// fold marks the CI summary row; activating it expands the folded checks.
	fold bool
}

// View renders the current state without mutating the model.
func (m *Model) View() tea.View {
	content, _ := m.layoutView()
	v := tea.NewView(content)
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

// layoutView derives both display text and click targets from current state.
func (m *Model) layoutView() (string, []rowTarget) {
	var rows []rowTarget
	w, h := m.size()
	if w < minWidth {
		return styled(pal.faint, narrowText), nil
	}
	foot := styleFooter(footerLine(w, m.bindings().footerHints(m.Snapshot.Stack != nil)))
	if msg := m.emptyMessage(); msg != "" {
		out := []string{}
		for _, l := range m.titleLines(w) {
			out = append(out, l.text)
		}
		out = append(out, "")
		if m.ShowHelp {
			for _, l := range m.helpLines() {
				out = append(out, ansi.Truncate(l, w, "…"))
			}
		} else {
			out = append(out, m.errorLines(w)...)
			out = append(out, styleWrapped(pal.faint, msg, w)...)
		}
		return strings.Join(append(pad(out, w, h), foot), "\n"), rows
	}
	var help []string
	budget := h - 2
	if m.ShowHelp {
		help = m.helpLines()
		budget = max(2, h-1-len(help))
	}
	head := fitHeader(m.headerLines(w), budget)
	out := make([]string, 0, h)
	for i, l := range head {
		for _, t := range l.targets {
			t.y = i
			rows = append(rows, t)
		}
		out = append(out, ansi.Truncate(l.text, w, "…"))
	}
	if m.ShowHelp {
		for _, l := range help {
			out = append(out, ansi.Truncate(l, w, "…"))
		}
		return strings.Join(append(pad(out, w, h), foot), "\n"), rows
	}
	lines, spans := renderBody(m.body(m.contentWidth()))
	markCursor(lines, spans, m.Cursor)
	bodyHigh := h - len(head) - 1
	off := clampOffset(m.Offset, len(lines), bodyHigh)
	for i := off; i < len(lines) && i-off < bodyHigh; i++ {
		rows = append(rows, rowTarget{y: len(out), x1: -1, item: itemAt(spans, i)})
		out = append(out, ansi.Truncate(lines[i], w, "…"))
	}
	return strings.Join(append(pad(out, w, h), foot), "\n"), rows
}

// pad keeps the footer on the last row and the whole view within the height.
func pad(out []string, w, h int) []string {
	for len(out) < h-1 {
		out = append(out, "")
	}
	return out[:h-1]
}

type headerLine struct {
	text    string
	prio    int
	targets []rowTarget
}

// Header priorities: 0 never drops, higher numbers are dropped first when the
// terminal is too short to show the whole header plus one body row.
const (
	prioError = iota + 1
	prioTitle
	prioCounts
	prioBlank
)

func (m *Model) headerLines(w int) []headerLine {
	s := m.Snapshot
	out := m.identityLines(w)
	add := func(prio int, text string) { out = append(out, headerLine{text: text, prio: prio}) }
	// Every title line opens the pull request, like the number on line 1.
	var open []rowTarget
	if s.PR != nil {
		open = []rowTarget{{x1: -1, item: -1, url: s.PR.URL}}
	}
	for _, l := range styleWrapped(pal.bold, clean(s.Title), w) {
		out = append(out, headerLine{text: l, prio: prioTitle, targets: open})
	}
	for _, l := range wrapSpans(m.metaSpans(), w) {
		add(prioCounts, l)
	}
	add(prioBlank, "")
	text, spans := tabLine(m.Section)
	out = append(out, headerLine{text: text, targets: spans})
	for _, l := range m.errorLines(w) {
		add(prioError, l)
	}
	add(prioBlank, "")
	return out
}

// identityLines is line 1: badge, number and repository on the left, the
// status on the right. The repository gives way first; a status that still
// does not fit wraps onto its own lines below, as the empty-state title does.
// Without a pull request it is the empty-state title itself.
func (m *Model) identityLines(w int) []headerLine {
	s := m.Snapshot
	if s.PR == nil {
		return m.titleLines(w)
	}
	b, number := badge(prState(s)), fmt.Sprintf("#%d", s.PR.Number)
	left := b + " " + styled(pal.bold, number)
	leftW := ansi.StringWidth(left)
	// A click on the number opens the pull request.
	x0 := ansi.StringWidth(b) + 1
	open := []rowTarget{{x0: x0, x1: min(x0+len(number), w), item: -1, url: s.PR.URL}}
	status := m.statusSpans()
	statusW := ansi.StringWidth(spanText(status))
	if statusW > 0 && leftW+1+statusW > w {
		out := []headerLine{{text: ansi.Truncate(left, w, "…"), targets: open}}
		for _, l := range wrapSpans(status, w) {
			out = append(out, headerLine{text: l, prio: prioError})
		}
		return out
	}
	room := w - leftW - 2
	if statusW > 0 {
		room -= statusW + 1
	}
	if repo := clean(s.PR.Repository); room >= 2 && repo != "" {
		shown := ansi.Truncate(repo, room, "…")
		left += "  " + styled(pal.faint, shown)
		leftW += 2 + ansi.StringWidth(shown)
	}
	if statusW == 0 {
		return []headerLine{{text: left, targets: open}}
	}
	return []headerLine{{text: left + strings.Repeat(" ", w-leftW-statusW) + renderSpans(status), targets: open}}
}

// metaSpans is the author and the size of the change.
func (m *Model) metaSpans() []span {
	s := m.Snapshot
	return []span{
		{text: "@" + clean(s.Author), style: pal.cyan},
		{text: " · " + plural(s.Commits, "commit") + " · " + plural(s.ChangedFiles, "file") + " · ", style: pal.faint},
		{text: "+" + compact(s.Additions), style: pal.green},
		{text: " ", style: pal.none},
		{text: "-" + compact(s.Deletions), style: pal.red},
	}
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
	x := 0
	for i, t := range tabs {
		if i > 0 {
			b.WriteString("  ")
			x += 2
		}
		label, style := t.label, pal.faint
		if t.section == active {
			style = pal.activeTab
		}
		b.WriteString(styled(style, label))
		spans = append(spans, rowTarget{x0: x, x1: x + ansi.StringWidth(label), tab: t.section, item: -1})
		x += ansi.StringWidth(label)
	}
	return b.String(), spans
}

// statusSpans keeps the age of the data even while a cooldown is running: that
// is exactly when knowing how old the summary is matters most.
func (m *Model) statusSpans() []span {
	now := m.now()
	var parts []span
	add := func(style lipgloss.Style, text string) {
		if len(parts) > 0 {
			parts = append(parts, span{text: " · ", style: pal.faint})
		}
		parts = append(parts, span{text: text, style: style})
	}
	switch {
	case !m.Snapshot.FetchedAt.IsZero():
		add(pal.faint, ago(now.Sub(m.Snapshot.FetchedAt))+" ago")
		if m.SummaryStale() {
			add(pal.yellow, "stale")
		}
		if m.Pinned != nil {
			add(pal.yellow, "pinned")
		}
	case m.SummaryLoading:
		add(pal.faint, "Loading…")
	}
	if now.Before(m.CooldownUntil) {
		d := m.CooldownUntil.Sub(now)
		add(pal.yellow, fmt.Sprintf("rate limited, retry in %ds", int((d+time.Second-1)/time.Second)))
	}
	return parts
}

// titleLines renders the name and status banner, moving the status onto its own
// wrapped line when it cannot share the first row.
func (m *Model) titleLines(w int) []headerLine {
	spans := m.statusSpans()
	status := spanText(spans)
	if status == "" || w-ansi.StringWidth(appName)-ansi.StringWidth(status) >= 1 {
		return []headerLine{{text: fitStatus(w, appName, spans)}}
	}
	out := []headerLine{{text: styled(pal.bold, ansi.Truncate(appName, w, "…"))}}
	for _, l := range wrapSpans(spans, w) {
		out = append(out, headerLine{text: l, prio: prioError})
	}
	return out
}

func fitStatus(w int, left string, right []span) string {
	text := spanText(right)
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(text)
	if text == "" || gap < 1 {
		return styled(pal.bold, ansi.Truncate(left, w, "…"))
	}
	return styled(pal.bold, left) + strings.Repeat(" ", gap) + renderSpans(right)
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
		out = append(out, styleWrapped(pal.yellow, "cache: "+clean(d.Warning.Error()), w)...)
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
	out := styleWrapped(pal.red, clean(err.Error()), w)
	var fe *model.FetchError
	if errors.As(err, &fe) && fe.Kind == model.AuthenticationError {
		out = append(out, styleWrapped(pal.yellow, "run: gh auth login", w)...)
	}
	return out
}

func prState(s model.Snapshot) string {
	if s.Draft && strings.EqualFold(s.State, "OPEN") {
		return "DRAFT"
	}
	if s.State == "" {
		return "UNKNOWN"
	}
	return strings.ToUpper(clean(s.State))
}

// body returns the lead lines, the selectable items and the trailing lines of
// the active section, laid out for the given content width.
func (m *Model) body(w int) ([]string, []bodyItem, []string) {
	switch m.Section {
	case model.Comments:
		lead, items := m.commentsBody(w)
		return lead, items, nil
	case model.Reviews:
		lead, items := m.reviewsBody(w)
		return lead, items, nil
	default:
		return m.overviewBody(w)
	}
}

// overviewBody lays out CI, then the review decision, then the stack. Lines
// that introduce a group ride on the next item as its head, so the cursor and
// the row map skip them; whatever is left over trails the last item.
func (m *Model) overviewBody(w int) ([]string, []bodyItem, []string) {
	paneW, _ := m.size()
	var items []bodyItem
	var pending []string
	add := func(it bodyItem) {
		it.head = append(pending, it.head...)
		pending = nil
		items = append(items, it)
	}
	head, ci := m.ciBlock(w, paneW)
	pending = append(pending, head...)
	for _, it := range ci {
		add(it)
	}
	pending = append(pending, "", m.reviewLine(w))
	if s := m.Snapshot.Stack; s != nil {
		pending = append(pending, "", m.stackHeading(w))
		for i := len(s.Entries) - 1; i >= 0; i-- {
			e := s.Entries[i]
			pin := e.PR
			// The cursor gutter already points at the row it selects, so the
			// entry marker stands down there. The index is the item's place in
			// the whole body, which now starts with the CI rows.
			add(bodyItem{lines: []string{m.stackRow(e, w, m.Cursor == len(items))}, url: e.PR.URL, pin: &pin})
		}
		if hidden := s.Size - len(s.Entries); hidden > 0 {
			pending = append(pending, styled(pal.faint, fmt.Sprintf("+%d more", hidden)))
		}
	}
	return nil, items, pending
}

// reviewLine states the review decision in words.
func (m *Model) reviewLine(w int) string {
	text := "no decision yet"
	switch strings.ToUpper(strings.ReplaceAll(clean(m.Snapshot.ReviewDecision), " ", "_")) {
	case "APPROVED":
		text = "approved"
	case "CHANGES_REQUESTED":
		text = "changes requested"
	case "REVIEW_REQUIRED":
		text = "review required"
	}
	return truncateSpans([]span{{text: "Review", style: pal.bold}, {text: "  ", style: pal.none},
		{text: text, style: decisionStyle(m.Snapshot.ReviewDecision)}}, w)
}

// stackHeading names the stack, the shown entry's position and the trunk.
func (m *Model) stackHeading(w int) string {
	s := m.Snapshot
	return truncateSpans([]span{{text: "Stack", style: pal.bold}, {text: fmt.Sprintf("  #%d · %s/%d · onto %s",
		s.Stack.Number, position(s.StackPosition), s.Stack.Size, clean(s.Stack.BaseBranch)), style: pal.faint}}, w)
}

// stackRow shows one entry: its number, a dot coloured by its lifecycle, its
// title and its review decision. The entry currently displayed is marked.
func (m *Model) stackRow(e model.StackEntry, w int, selected bool) string {
	marker, own := "  ", pal.none
	if m.showsEntry(e) {
		own = pal.bold
		if !selected {
			marker = "› "
		}
	}
	row := []span{{text: marker, style: pal.bold},
		{text: "#" + strconv.Itoa(e.PR.Number), style: own},
		{text: "  ", style: pal.none},
		{text: "●", style: prStateStyle(entryState(e))},
		{text: "  ", style: pal.none},
		{text: strings.ReplaceAll(clean(e.Title), "\n", " "), style: own}}
	if d := entryDecision(e.ReviewDecision); d != "" {
		row = append(row, span{text: "  " + d, style: pal.faint})
	}
	return truncateSpans(row, w)
}

// showsEntry reports whether an entry is the pull request on screen.
func (m *Model) showsEntry(e model.StackEntry) bool {
	if m.Snapshot.PR != nil && *m.Snapshot.PR == e.PR {
		return true
	}
	return m.Snapshot.StackPosition == e.Position
}

// position reads an unknown stack position as unknown rather than as zero.
func position(p int) string {
	if p < 1 {
		return "?"
	}
	return strconv.Itoa(p)
}

func entryState(e model.StackEntry) string {
	if e.Draft && strings.EqualFold(e.State, "OPEN") {
		return "DRAFT"
	}
	if e.State == "" {
		return "UNKNOWN"
	}
	return strings.ToUpper(clean(e.State))
}

// entryDecision reads a review decision as the quiet tail of a stack row.
func entryDecision(s string) string {
	return strings.TrimSpace(strings.ToLower(strings.ReplaceAll(clean(s), "_", " ")))
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

func checkRow(c model.Check, w int) string { return checkRowGlyph(c, w, checkGlyph(c.State)) }

func checkRowGlyph(c model.Check, w int, glyph string) string {
	left := glyph + " " + strings.ReplaceAll(clean(c.Name), "\n", " ")
	right := strings.ReplaceAll(string(c.State), "_", " ")
	// Keep a readable name even in the narrowest supported pane.
	if c.State == model.CheckActionRequired && w < 30 {
		right = "action req"
	}
	left = ansi.Truncate(left, max(0, w-ansi.StringWidth(right)-1), "…")
	gap := max(1, w-ansi.StringWidth(left)-ansi.StringWidth(right))
	// The glyph and the outcome carry the state; the name stays readable.
	style := stateStyle(c.State)
	if name, ok := strings.CutPrefix(left, glyph); ok {
		left = styled(style, glyph) + name
	}
	return left + strings.Repeat(" ", gap) + styled(style, right)
}

// sectionLead reports loading progress and the age of the cached discussion.
func (m *Model) sectionLead(s model.Section) []string {
	var out []string
	d := m.Discussions[s]
	if d == nil || d.Loading {
		out = append(out, styled(pal.yellow, m.pendingGlyph())+styled(pal.faint, " Loading…"))
	}
	if d != nil && d.Data != nil {
		age := styled(pal.faint, "fetched "+ago(m.now().Sub(d.Data.FetchedAt))+" ago")
		if m.DiscussionStale(s) {
			age += " " + styled(pal.yellow, "(stale)")
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
		return append(lead, styled(pal.faint, "No comments")), nil
	}
	items := make([]bodyItem, 0, len(d.Comments))
	for _, c := range d.Comments {
		items = append(items, bodyItem{lines: m.commentLines(c, w, ""), url: c.URL})
	}
	return lead, items
}

func (m *Model) commentLines(c model.Comment, w int, indent string) []string {
	lines := []string{indent + styled(pal.cyan, "@"+clean(c.Author)) +
		styled(pal.faint, " · "+ago(m.now().Sub(c.CreatedAt))+" ago")}
	for _, l := range m.md.body(clean(c.Body), w-len(indent)) {
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
		row := []span{{text: "@" + clean(r.Author), style: pal.cyan}, {text: "  ", style: pal.none},
			{text: clean(r.State), style: decisionStyle(r.State)}}
		items = append(items, bodyItem{lines: []string{truncateSpans(row, w)}, url: r.URL})
	}
	for _, t := range d.Threads {
		items = append(items, m.threadItem(t, w))
	}
	if len(items) == 0 {
		return append(lead, styled(pal.faint, "No reviews")), nil
	}
	return lead, items
}

func (m *Model) threadItem(t model.ReviewThread, w int) bodyItem {
	arrow := "▸"
	if m.Expanded[t.ID] {
		arrow = "▾"
	}
	head := arrow + " "
	// An unresolved path is where the reader must look, so it is the loudest.
	pathStyle := pal.boldCyan
	if t.Resolved {
		pathStyle = pal.cyan
	}
	where := []span{{text: clean(t.Path), style: pathStyle}}
	if t.Line != nil {
		where = append(where, span{text: ":" + strconv.Itoa(*t.Line), style: pal.faint})
	}
	path := spanText(where)
	avail := w - ansi.StringWidth(head)
	tailSpans := threadTailSpans(t, w < compactThread)
	if avail-ansi.StringWidth(spanText(tailSpans)) < minPathWidth {
		tailSpans = threadTailSpans(t, true)
	}
	tail := spanText(tailSpans)
	room := avail - ansi.StringWidth(tail)
	tailWidth := ansi.StringWidth(tail)
	if room < minPathWidth {
		room = min(minPathWidth, avail)
		tailWidth = avail - room
	}
	if room < 1 {
		room = 1
	}
	shown := truncatePath(path, room)
	lines := []string{styled(pal.bold, arrow) + " " +
		styleLeftTruncated(path, shown, where) + truncateSpans(tailSpans, tailWidth)}
	if m.Expanded[t.ID] {
		for _, c := range t.Comments {
			lines = append(lines, m.commentLines(c, w, "  ")...)
		}
		return bodyItem{lines: lines, url: t.URL, threadID: t.ID}
	}
	return bodyItem{lines: append(lines, ""), url: t.URL, threadID: t.ID}
}

// threadTailSpans is the state and reply count shown after a thread's path. The
// compact form keeps a narrow row readable: ✓ resolved, ~ outdated, Nc replies.
func threadTailSpans(t model.ReviewThread, compact bool) []span {
	out := []span{{text: "  ", style: pal.none}}
	if compact {
		if t.Resolved {
			out = append(out, span{text: "✓", style: pal.faintGreen})
		}
		if t.Outdated {
			out = append(out, span{text: "~", style: pal.faint})
		}
		if t.Resolved || t.Outdated {
			out = append(out, span{text: " ", style: pal.none})
		}
		return append(out, span{text: strconv.Itoa(len(t.Comments)) + "c", style: pal.none})
	}
	if t.Resolved || t.Outdated {
		out = append(out, span{text: "(", style: pal.faint})
		if t.Resolved {
			out = append(out, span{text: "resolved", style: pal.faintGreen})
		}
		if t.Resolved && t.Outdated {
			out = append(out, span{text: ", ", style: pal.faint})
		}
		if t.Outdated {
			out = append(out, span{text: "outdated", style: pal.faint})
		}
		out = append(out, span{text: ")", style: pal.faint}, span{text: "  ", style: pal.none})
	} else {
		out = out[:0]
		out = append(out, span{text: "  ", style: pal.none})
	}
	return append(out, span{text: plural(len(t.Comments), "comment"), style: pal.none})
}

// renderBody flattens lead lines, items and trailing lines into screen lines,
// and reports each item's [start,end) line span. Head and trailing lines
// belong to no item, so a click on them selects nothing.
func renderBody(lead []string, items []bodyItem, tail []string) ([]string, [][2]int) {
	lines := make([]string, 0, len(lead)+len(items)+len(tail))
	for _, l := range lead {
		lines = append(lines, " "+l)
	}
	spans := make([][2]int, len(items))
	for i, it := range items {
		for _, l := range it.head {
			lines = append(lines, " "+l)
		}
		spans[i][0] = len(lines)
		for _, l := range it.lines {
			lines = append(lines, " "+l)
		}
		spans[i][1] = len(lines)
	}
	for _, l := range tail {
		lines = append(lines, " "+l)
	}
	return lines, spans
}

func markCursor(lines []string, spans [][2]int, cursor int) {
	if cursor < 0 || cursor >= len(spans) {
		return
	}
	if at := spans[cursor][0]; at < len(lines) {
		lines[at] = styled(pal.bold, "›") + strings.TrimPrefix(lines[at], " ")
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

// clampOffset keeps a scroll offset inside the body. It never moves the offset
// toward the cursor: an explicit scroll must survive the next render, so the
// rest of an item taller than the body stays reachable.
func clampOffset(off, total, high int) int {
	if high <= 0 || total <= high {
		return 0
	}
	if off > total-high {
		off = total - high
	}
	if off < 0 {
		off = 0
	}
	return off
}

// revealCursor scrolls the least it can to bring the selected item's first line
// into view. An item taller than the body shows its head, never its tail.
func revealCursor(off, total, high int, spans [][2]int, cursor int) int {
	if high <= 0 || cursor < 0 || cursor >= len(spans) {
		return clampOffset(off, total, high)
	}
	start, end := spans[cursor][0], spans[cursor][1]
	switch {
	case start < off, start >= off+high:
		off = start
	case end-start <= high && end > off+high:
		off = end - high
	}
	return clampOffset(off, total, high)
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
		// U+200C and U+200D are kept: emoji and script joiners are content.
		case r == 0x200b, r == 0x200e, r == 0x200f, r == 0x2028, r == 0x2029:
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

// plural formats a count with its unit, singular at one.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
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

// helpLines renders each help group as its own block: the pane is too narrow
// for Bubbles' side-by-side columns.
func (m *Model) helpLines() []string {
	var out []string
	for _, g := range m.bindings().FullHelp() {
		block := m.help.FullHelpView([][]bkey.Binding{g})
		if block == "" {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		for _, l := range strings.Split(block, "\n") {
			out = append(out, " "+l)
		}
	}
	return out
}
