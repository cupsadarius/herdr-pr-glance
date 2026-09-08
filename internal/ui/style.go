package ui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

// palette names only the sixteen ANSI colours and the attributes every
// terminal implements, so the view follows the user's own theme instead of
// imposing one. Bubble Tea downsamples them to the terminal's profile.
type palette struct {
	none, bold, faint                 lipgloss.Style
	red, green, yellow, magenta, cyan lipgloss.Style
	faintGreen, boldCyan, activeTab   lipgloss.Style
}

var pal = newPalette()

func newPalette() palette {
	base := lipgloss.NewStyle()
	bold := base.Bold(true)
	faint := base.Faint(true)
	color := func(c string) lipgloss.Style { return base.Foreground(lipgloss.Color(c)) }
	red, green, yellow := color("1"), color("2"), color("3")
	magenta, cyan := color("5"), color("6")
	return palette{
		none: base, bold: bold, faint: faint,
		red: red, green: green, yellow: yellow, magenta: magenta, cyan: cyan,
		faintGreen: green.Faint(true), boldCyan: cyan.Bold(true), activeTab: bold.Reverse(true),
	}
}

// stateStyle colours a check by its outcome: red for anything that needs
// attention, yellow while it runs, green when it passed, faint otherwise.
func stateStyle(s model.CheckState) lipgloss.Style {
	switch s {
	case model.CheckFailed, model.CheckTimedOut, model.CheckCancelled, model.CheckActionRequired:
		return pal.red
	case model.CheckPending:
		return pal.yellow
	case model.CheckPassed:
		return pal.green
	default:
		return pal.faint
	}
}

// decisionStyle colours a review decision or an individual review state.
func decisionStyle(s string) lipgloss.Style {
	switch strings.ToUpper(strings.ReplaceAll(clean(s), " ", "_")) {
	case "APPROVED":
		return pal.green
	case "CHANGES_REQUESTED":
		return pal.red
	case "REVIEW_REQUIRED":
		return pal.yellow
	default:
		return pal.faint
	}
}

// prStateStyle colours the lifecycle badge shown next to the PR number.
func prStateStyle(state string) lipgloss.Style {
	switch state {
	case "OPEN":
		return pal.green
	case "MERGED":
		return pal.magenta
	case "CLOSED":
		return pal.red
	default: // DRAFT and UNKNOWN
		return pal.faint
	}
}

// styled renders text in a style, leaving empty text untouched so blank cells
// carry no escape sequences at all.
func styled(s lipgloss.Style, text string) string {
	if text == "" {
		return text
	}
	return s.Render(text)
}

// styleWrapped wraps plain text to width and styles each finished line, so the
// wrapping still measures the text the terminal will show.
func styleWrapped(s lipgloss.Style, text string, w int) []string {
	lines := wrapLines(text, w)
	for i, l := range lines {
		lines[i] = styled(s, l)
	}
	return lines
}

// span is a run of plain text with the style it is rendered in. Layout is
// always computed on the plain text; the styles are applied last.
type span struct {
	text  string
	style lipgloss.Style
}

func spanText(spans []span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.text)
	}
	return b.String()
}

func renderSpans(spans []span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(styled(s.style, s.text))
	}
	return b.String()
}

// sliceSpans keeps the spans covering [start,end) of the concatenated text.
func sliceSpans(spans []span, start, end int) []span {
	var out []span
	at := 0
	for _, s := range spans {
		lo, hi := max(at, start), min(at+len(s.text), end)
		if lo < hi {
			out = append(out, span{text: s.text[lo-at : hi-at], style: s.style})
		}
		at += len(s.text)
	}
	return out
}

// truncateSpans fits spans into width by truncating their plain text first and
// styling only what survives, so the ellipsis never lands mid-sequence.
func truncateSpans(spans []span, w int) string {
	text := spanText(spans)
	if ansi.StringWidth(text) <= w {
		return renderSpans(spans)
	}
	cut := ansi.Truncate(text, w, "…")
	kept, ok := strings.CutSuffix(cut, "…")
	if !ok {
		return styled(pal.faint, cut)
	}
	return renderSpans(sliceSpans(spans, 0, len(kept))) + styled(pal.faint, "…")
}

// wrapSpans wraps the plain text of spans to width, then restyles each line
// from the spans it came from. Continuation lines keep their own sequences, so
// no style leaks across a line break.
func wrapSpans(spans []span, w int) []string {
	plain := spanText(spans)
	lines := wrapLines(plain, w)
	out := make([]string, 0, len(lines))
	at := 0
	for _, line := range lines {
		i := strings.Index(plain[at:], line)
		if i < 0 { // the wrap rewrote the text: show it unstyled rather than wrong
			out = append(out, line)
			continue
		}
		start := at + i
		out = append(out, renderSpans(sliceSpans(spans, start, start+len(line))))
		at = start + len(line)
	}
	return out
}

// styleLeftTruncated restyles a path that truncatePath cut from the left.
func styleLeftTruncated(full, shown string, spans []span) string {
	if shown == full {
		return renderSpans(spans)
	}
	rest := strings.TrimPrefix(shown, "…")
	start := len(full) - len(rest)
	if start < 0 || !strings.HasSuffix(full, rest) {
		return styled(pal.faint, shown)
	}
	return styled(pal.faint, "…") + renderSpans(sliceSpans(spans, start, len(full)))
}

// countsSpans styles the check tally: a failing count is red and a running one
// yellow because they are the reason to look; the rest stay faint.
func countsSpans(c model.CheckCounts) []span {
	var out []span
	for _, p := range []struct {
		n     int
		label string
		style lipgloss.Style
	}{{c.Failed, "failed", pal.red}, {c.Pending, "pending", pal.yellow}, {c.Passed, "passed", pal.faint},
		{c.Neutral, "neutral", pal.faint}, {c.Skipped, "skipped", pal.faint}, {c.Unknown, "unknown", pal.faint}} {
		if p.n <= 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, span{text: " · ", style: pal.faint})
		}
		out = append(out, span{text: strconv.Itoa(p.n) + " " + p.label, style: p.style})
	}
	if len(out) == 0 {
		return []span{{text: "No checks", style: pal.faint}}
	}
	return out
}

// styleFooter keeps the key letters legible and dims their descriptions.
func styleFooter(s string) string {
	parts := strings.Split(s, " ")
	for i, p := range parts {
		switch {
		case p == "":
		case len([]rune(p)) == 1:
			parts[i] = styled(pal.bold, p)
		default:
			parts[i] = styled(pal.faint, p)
		}
	}
	return strings.Join(parts, " ")
}
