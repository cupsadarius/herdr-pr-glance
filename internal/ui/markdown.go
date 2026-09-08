package ui

import (
	"regexp"
	"strconv"
	"strings"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	// maxMarkdownRunes is where a body stops being prose and starts being a
	// paste: past it the render cost is not worth the formatting.
	maxMarkdownRunes = 20000
	// markdownCacheLimit bounds the rendered-line cache. Entries are keyed by
	// the body text itself, so a refreshed comment simply stops being looked
	// up and the whole cache is dropped once it grows past this.
	markdownCacheLimit = 256
	// glamourMargin is the two-column left margin the standard styles add.
	// The pane indents bodies itself, so the margin is taken back off.
	glamourMargin = "  "
)

// hyperlink matches an OSC 8 sequence. Glamour wraps links in them; the pane
// opens the comment with o instead and never offers a clickable link, and
// dropping them keeps every escape the view writes an SGR sequence.
var hyperlink = regexp.MustCompile("\x1b]8;[^\x07\x1b]*(?:\x07|\x1b\\\\)")

// markdown renders comment bodies through Glamour and remembers the result.
// Rendering is far too expensive to repeat on every frame, so both the
// renderers and their output are cached; the output cache is keyed by the body
// text and the width, which makes it self-invalidating when either changes.
type markdown struct {
	style     string
	render    func(text string, w int) (string, error)
	renderers map[int]*glamour.TermRenderer
	lines     map[string][]string
}

func newMarkdown() *markdown {
	// Bubble Tea v2 can report the terminal background (RequestBackgroundColor
	// plus BackgroundColorMsg), but only with Init and Update plumbing this
	// task does not touch, so the dark style is the standing choice. "auto" is
	// never right here: it reads the environment, not the terminal.
	md := &markdown{style: "dark", renderers: map[int]*glamour.TermRenderer{}, lines: map[string][]string{}}
	md.render = md.glamour
	return md
}

// body renders one sanitized body into lines that fit the width. Anything that
// cannot be rendered as Markdown falls back to the plain wrapping the pane used
// before, so a body is never lost to a renderer error.
func (md *markdown) body(text string, w int) []string {
	if md == nil || w < 1 || strings.TrimSpace(text) == "" || len([]rune(text)) > maxMarkdownRunes {
		return wrapLines(text, w)
	}
	key := strconv.Itoa(w) + "\x00" + text
	if lines, ok := md.lines[key]; ok {
		return lines
	}
	out, err := md.render(text, w)
	if err != nil {
		return wrapLines(text, w)
	}
	lines := markdownLines(out, w)
	if len(lines) == 0 {
		return wrapLines(text, w)
	}
	if len(md.lines) >= markdownCacheLimit {
		clear(md.lines)
	}
	md.lines[key] = lines
	return lines
}

func (md *markdown) glamour(text string, w int) (string, error) {
	r, ok := md.renderers[w]
	if !ok {
		var err error
		r, err = glamour.NewTermRenderer(
			glamour.WithStandardStyle(md.style),
			glamour.WithWordWrap(w),
			glamour.WithPreservedNewLines(),
		)
		if err != nil {
			return "", err
		}
		md.renderers[w] = r
	}
	return r.Render(text)
}

// markdownLines turns a Glamour document into pane rows: no hyperlink escapes,
// no left margin, no leading or trailing blank lines, and never wider than the
// space the body was given.
func markdownLines(out string, w int) []string {
	lines := strings.Split(hyperlink.ReplaceAllString(out, ""), "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(strings.TrimPrefix(l, glamourMargin), w, "…")
	}
	blank := func(l string) bool { return strings.TrimSpace(ansi.Strip(l)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines
}
