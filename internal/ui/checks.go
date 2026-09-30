package ui

import (
	"strconv"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

// barRuns splits width into the failing, running, passed and other runs of the
// CI bar. Each run is proportional to its count and any non-empty group keeps
// at least one cell; the largest run absorbs rounding and the cells the
// minimums borrowed, so the runs always fill the width exactly. Callers only
// draw the bar at widths well above four cells.
func barRuns(c model.CheckCounts, width int) [4]int {
	counts := [4]int{c.Failed, c.Pending, c.Passed, c.Neutral + c.Skipped + c.Unknown}
	total := counts[0] + counts[1] + counts[2] + counts[3]
	var runs [4]int
	if total == 0 || width <= 0 {
		return runs
	}
	sum, largest := 0, -1
	for i, n := range counts {
		if n == 0 {
			continue
		}
		runs[i] = max(1, n*width/total)
		sum += runs[i]
		if largest < 0 || runs[i] > runs[largest] {
			largest = i
		}
	}
	runs[largest] += width - sum
	return runs
}

// foldKey marks the fold row in Model.Expanded. Review-thread keys are GitHub
// node IDs, which never equal it.
const foldKey = "checks"

// minBarWidth is the narrowest pane that still draws the CI bar.
const minBarWidth = 24

// failing reports the states the pane treats as needing attention, the same
// four the snapshot counts as failed.
func failing(s model.CheckState) bool {
	switch s {
	case model.CheckFailed, model.CheckTimedOut, model.CheckCancelled, model.CheckActionRequired:
		return true
	}
	return false
}

// ciBar draws barRuns as coloured glyph runs; "other" uses a thinner rule so it
// reads as background.
func ciBar(c model.CheckCounts, w int) string {
	runs := barRuns(c, w)
	glyphs := [4]string{"━", "━", "━", "─"}
	styles := [4]lipgloss.Style{pal.red, pal.yellow, pal.green, pal.faint}
	var b strings.Builder
	for i, n := range runs {
		b.WriteString(styled(styles[i], strings.Repeat(glyphs[i], n)))
	}
	return b.String()
}

// ciSummary is the heading's verdict by precedence: failing and running,
// failing, running, passed, then the tally.
func (m *Model) ciSummary() []span {
	c := m.Snapshot.CheckCounts
	running := span{text: m.pendingGlyph() + " " + strconv.Itoa(c.Pending) + " running", style: pal.yellow}
	failed := span{text: "✗ " + strconv.Itoa(c.Failed) + " failing", style: pal.red}
	switch {
	case len(m.Snapshot.Checks) == 0:
		return []span{{text: "No checks", style: pal.faint}}
	case c.Failed > 0 && c.Pending > 0:
		return []span{running, {text: " · ", style: pal.faint}, failed}
	case c.Failed > 0:
		return []span{failed}
	case c.Pending > 0:
		return []span{running}
	case c.Passed > 0:
		return []span{{text: "✓ " + strconv.Itoa(c.Passed) + " passed", style: pal.green}}
	default:
		return countsSpans(c)
	}
}

// pendingGlyph is the running-check glyph; Task 7 turns it into the spinner.
func (m *Model) pendingGlyph() string { return "◷" }

// ciBlock renders the CI heading, the bar and the check rows. Failing and
// running checks are listed; the rest fold into one selectable summary row
// that enter expands in place.
func (m *Model) ciBlock(w, paneW int) ([]string, []bodyItem) {
	s := m.Snapshot
	heading := append([]span{{text: "CI", style: pal.bold}, {text: "  ", style: pal.none}}, m.ciSummary()...)
	head := wrapSpans(heading, w)
	if len(s.Checks) == 0 {
		return head, nil
	}
	if paneW >= minBarWidth {
		head = append(head, ciBar(s.CheckCounts, w))
	}
	var items []bodyItem
	row := func(c model.Check) bodyItem {
		return bodyItem{lines: []string{m.checkLine(c, w)}, url: c.URL}
	}
	var folded []model.Check
	for _, c := range s.Checks {
		if failing(c.State) {
			items = append(items, row(c))
		}
	}
	for _, c := range s.Checks {
		switch {
		case c.State == model.CheckPending:
			items = append(items, row(c))
		case !failing(c.State):
			folded = append(folded, c)
		}
	}
	if len(folded) == 0 {
		return head, items
	}
	items = append(items, bodyItem{lines: []string{m.foldRow(w)}, fold: true})
	if m.Expanded[foldKey] {
		for _, c := range folded {
			items = append(items, row(c))
		}
	}
	return head, items
}

// checkLine is a check row; Task 7 swaps the running glyph for the spinner.
func (m *Model) checkLine(c model.Check, w int) string { return checkRow(c, w) }

// foldRow summarises the folded checks. A green tick appears only when
// something actually passed.
func (m *Model) foldRow(w int) string {
	c := m.Snapshot.CheckCounts
	arrow := "▸"
	if m.Expanded[foldKey] {
		arrow = "▾"
	}
	spans := []span{{text: arrow + " ", style: pal.bold}}
	sep := false
	add := func(n int, text string, style lipgloss.Style) {
		if n <= 0 {
			return
		}
		if sep {
			spans = append(spans, span{text: " · ", style: pal.faint})
		}
		spans = append(spans, span{text: text, style: style})
		sep = true
	}
	add(c.Passed, "✓ "+strconv.Itoa(c.Passed)+" passed", pal.green)
	add(c.Neutral, strconv.Itoa(c.Neutral)+" neutral", pal.faint)
	add(c.Skipped, strconv.Itoa(c.Skipped)+" skipped", pal.faint)
	add(c.Unknown, strconv.Itoa(c.Unknown)+" unknown", pal.faint)
	return truncateSpans(spans, w)
}
