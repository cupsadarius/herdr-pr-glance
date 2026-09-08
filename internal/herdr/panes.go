package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/cupsadarius/herdr-pr-glance/internal/command"
)

const (
	pluginID       = "glance.pr"
	viewEntrypoint = "view"
	// The split aims at roughly 44 columns while leaving the working pane usable.
	targetColumns     = 44
	minWorkingColumns = 40
	// Herdr accepts at most half a split per resize call; two calls suffice.
	maxResizeAmount = 0.5
	maxResizeCalls  = 2
	resizeTolerance = 0.02
	callTimeout     = 2 * time.Second
	// State files beneath HERDR_PLUGIN_STATE_DIR.
	splitsFile        = "panes.json"
	overlaysFile      = "overlays.json"
	maxOverlayRecords = 32
)

// Launcher opens, focuses and sizes the Glance pane of the invoking tab. Herdr
// 0.8.2 cannot list plugin panes, so the pane opened for a tab is recorded in
// the plugin state directory and checked against a snapshot and focus ownership.
type Launcher struct {
	Runner       Runner
	Bin          string
	StateDir     string
	WorkspaceID  string
	TabID        string
	TargetPaneID string
	// Warn receives non-fatal failures; nil writes one line to stderr.
	Warn func(error)
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type paneOpenEnvelope struct {
	Result struct {
		PluginPane struct {
			PluginID   string `json:"plugin_id"`
			Entrypoint string `json:"entrypoint"`
			Pane       struct {
				PaneID string `json:"pane_id"`
			} `json:"pane"`
		} `json:"plugin_pane"`
	} `json:"result"`
	Error *envelopeError `json:"error"`
}
type rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}
type layoutPane struct {
	PaneID string `json:"pane_id"`
	Rect   rect   `json:"rect"`
}
type layoutSplit struct {
	ID        string  `json:"id"`
	Direction string  `json:"direction"`
	Ratio     float64 `json:"ratio"`
	Rect      rect    `json:"rect"`
}
type layout struct {
	Area   rect          `json:"area"`
	Panes  []layoutPane  `json:"panes"`
	Splits []layoutSplit `json:"splits"`
	Zoomed bool          `json:"zoomed"`
}
type layoutEnvelope struct {
	Result struct {
		Layout *layout `json:"layout"`
	} `json:"result"`
	Error *envelopeError `json:"error"`
}

func (l *Launcher) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	runner := l.Runner
	if runner == nil {
		runner = command.ExecRunner{}
	}
	bin := l.Bin
	if bin == "" {
		bin = os.Getenv("HERDR_BIN_PATH")
		if bin == "" {
			bin = "herdr"
		}
	}
	return runner.Run(ctx, "", append([]string{bin}, args...)...)
}

// Open focuses the tab's recorded Glance pane when it still exists, and
// otherwise opens a right-hand split, records it and sizes it. Records are read
// before the snapshot so that dead IDs can be pruned in the same pass.
func (l *Launcher) Open(ctx context.Context) error {
	records, overlays := l.records(), l.overlays()
	live, err := l.sessionPanes(ctx)
	if err != nil {
		// Without a snapshot a recorded pane cannot be verified; with nothing
		// recorded the launch can proceed regardless.
		if records[l.TabID] != "" {
			return err
		}
		// Unpruned records may name panes Herdr has since reused, so a stale ID
		// can cost Glance the right-hand column this once.
		l.warn(fmt.Errorf("pane records left unverified, placement may be off: %w", err))
	} else {
		var pruneErr error
		overlays, pruneErr = l.prune(records, overlays, live)
		l.warn(pruneErr)
	}
	if existing := liveRecord(records[l.TabID], l.TabID, live); existing != "" {
		owned, err := l.focusRecorded(ctx, existing)
		if err != nil || owned {
			return err
		}
		delete(records, l.TabID)
		l.warn(l.writeRecords(records))
	}
	args := []string{"--placement", "split", "--direction", "right"}
	switch target := l.splitTarget(ctx, glancePanes(records, overlays)); {
	case target != "":
		args = append(args, "--target-pane", target)
	case l.WorkspaceID != "":
		args = append(args, "--workspace", l.WorkspaceID)
	}
	pane, err := l.openPane(ctx, args)
	if err != nil {
		return err
	}
	// The pane is on screen, so the action has succeeded: a failed record only
	// costs the next invocation a new pane instead of a focus.
	l.warn(l.record(pane))
	l.resize(ctx, pane)
	return nil
}

// focusRecorded verifies ownership because short pane handles can be reused
// after a session restart. Only a known missing/wrong owner permits a new pane.
func (l *Launcher) focusRecorded(ctx context.Context, pane string) (bool, error) {
	out, runErr := l.run(ctx, "plugin", "pane", "focus", pane)
	var envelope paneOpenEnvelope
	decodeErr := json.Unmarshal([]byte(out), &envelope)
	if decodeErr == nil && envelope.Error != nil {
		if envelope.Error.Code == "plugin_pane_not_found" {
			return false, nil
		}
		return false, fmt.Errorf("Herdr %s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if runErr != nil {
		return false, runErr
	}
	if decodeErr != nil {
		return false, fmt.Errorf("decode Herdr pane focus: %w", decodeErr)
	}
	info := envelope.Result.PluginPane
	if info.PluginID == "" || info.Entrypoint == "" || info.Pane.PaneID != pane {
		return false, fmt.Errorf("Herdr pane focus response missing or mismatched ownership")
	}
	return info.PluginID == pluginID && info.Entrypoint == viewEntrypoint, nil
}

// glancePanes is the set of pane IDs the launcher has recorded as its own.
func glancePanes(records map[string]string, overlays []string) map[string]bool {
	set := make(map[string]bool, len(records)+len(overlays))
	for _, id := range records {
		set[id] = true
	}
	for _, id := range overlays {
		set[id] = true
	}
	return set
}

// liveRecord returns the recorded pane only while the session still shows it in
// the same tab. Ownership is never guessed from titles or paths.
func liveRecord(pane, tab string, live []Pane) string {
	if pane == "" {
		return ""
	}
	for _, p := range live {
		if p.PaneID == pane && p.TabID == tab {
			return pane
		}
	}
	return ""
}

// Overlay opens a transient overlay. Herdr places overlays on the active pane
// and rejects --target-pane for them, so only the placement is sent. Overlays
// are recorded (so a split Glance never treats one as a working pane) but never
// resized and never tied to a tab.
func (l *Launcher) Overlay(ctx context.Context) error {
	records, overlays := l.records(), l.overlays()
	// Pruning is maintenance: a snapshot failure only leaves the files as they
	// were, so it neither warns nor blocks the overlay.
	if live, err := l.sessionPanes(ctx); err == nil {
		_, pruneErr := l.prune(records, overlays, live)
		l.warn(pruneErr)
	}
	pane, err := l.openPane(ctx, []string{"--placement", "overlay"})
	if err != nil {
		return err
	}
	// The overlay is on screen: a failed record only costs a split Glance in the
	// same tab its exclusion, so it warns rather than failing the action.
	l.warn(l.recordOverlay(pane))
	return nil
}

// warn reports a non-fatal failure on one line without failing the action.
func (l *Launcher) warn(err error) {
	if err == nil {
		return
	}
	if l.Warn != nil {
		l.Warn(err)
		return
	}
	fmt.Fprintf(os.Stderr, "herdr-pr-glance: %v\n", err)
}

func (l *Launcher) openPane(ctx context.Context, extra []string) (string, error) {
	args := []string{"plugin", "pane", "open", "--plugin", pluginID, "--entrypoint", viewEntrypoint}
	args = append(args, extra...)
	out, err := l.run(ctx, append(args, "--focus")...)
	if err != nil {
		return "", err
	}
	var envelope paneOpenEnvelope
	if err = json.Unmarshal([]byte(out), &envelope); err != nil {
		return "", fmt.Errorf("decode Herdr pane open: %w", err)
	}
	if envelope.Error != nil {
		return "", fmt.Errorf("Herdr %s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if envelope.Result.PluginPane.Pane.PaneID == "" {
		return "", fmt.Errorf("Herdr pane open response missing pane_id")
	}
	return envelope.Result.PluginPane.Pane.PaneID, nil
}

func (l *Launcher) recordsPath() string { return filepath.Join(l.StateDir, splitsFile) }

// records maps tab ID to the split Glance pane opened for it. An unreadable or
// corrupt file is treated as empty: a new pane is cheaper than a stuck record.
func (l *Launcher) records() map[string]string {
	m := map[string]string{}
	readJSON(l.recordsPath(), &m)
	return m
}

// overlays lists the overlay panes recorded for this state directory.
func (l *Launcher) overlays() []string {
	var ids []string
	readJSON(filepath.Join(l.StateDir, overlaysFile), &ids)
	return ids
}

// readJSON zeroes the target when the file is missing or corrupt, so a partial
// decode never leaves half a record behind.
func readJSON(path string, target any) {
	raw, readErr := os.ReadFile(path)
	if readErr == nil && json.Unmarshal(raw, target) == nil {
		return
	}
	switch t := target.(type) {
	case *map[string]string:
		*t = map[string]string{}
	case *[]string:
		*t = nil
	}
}
func (l *Launcher) writeRecords(m map[string]string) error {
	return l.writeJSON(l.recordsPath(), m)
}

// writeJSON replaces the file atomically at 0600, like the discussion cache.
func (l *Launcher) writeJSON(path string, value any) error {
	if err := os.MkdirAll(l.StateDir, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(l.StateDir, ".panes-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func (l *Launcher) record(pane string) error {
	if l.TabID == "" {
		return nil
	}
	m := l.records()
	m[l.TabID] = pane
	return l.writeRecords(m)
}

// recordOverlay remembers an overlay pane so that a split Glance in the same
// tab never mistakes it for a working pane. Overlays are closed by the user
// rather than tracked per tab, so the newest few IDs are kept as a plain list.
func (l *Launcher) recordOverlay(pane string) error {
	ids := l.overlays()
	kept := []string{pane}
	for _, id := range ids {
		if id != pane && id != "" && len(kept) < maxOverlayRecords {
			kept = append(kept, id)
		}
	}
	return l.writeJSON(filepath.Join(l.StateDir, overlaysFile), kept)
}

// RecordedPanes lists every Glance pane this plugin is known to have opened:
// the per-tab splits and the recent overlays. Stale IDs are harmless because
// they never match a live pane; the view passes them to the resolver so a
// second Glance pane is never taken for the working pane.
func RecordedPanes(stateDir string) []string {
	if stateDir == "" {
		return nil
	}
	splits := map[string]string{}
	readJSON(filepath.Join(stateDir, splitsFile), &splits)
	var overlays []string
	readJSON(filepath.Join(stateDir, overlaysFile), &overlays)
	seen, ids := map[string]bool{}, []string{}
	for _, id := range splits {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range overlays {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// sessionPanes returns the panes Herdr currently shows.
func (l *Launcher) sessionPanes(ctx context.Context) ([]Pane, error) {
	out, err := l.run(ctx, "api", "snapshot")
	if err != nil {
		return nil, err
	}
	var envelope snapshotEnvelope
	if err = json.Unmarshal([]byte(out), &envelope); err != nil {
		return nil, fmt.Errorf("decode Herdr snapshot: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("Herdr %s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if envelope.Result.Snapshot == nil {
		return nil, fmt.Errorf("Herdr snapshot response missing snapshot")
	}
	return envelope.Result.Snapshot.Panes, nil
}

// prune drops recorded panes the session no longer shows. Herdr pane IDs are
// short handles, so presence alone does not prove ownership; Open also verifies
// the current tab's record using the focus response.
func (l *Launcher) prune(records map[string]string, overlays []string, live []Pane) ([]string, error) {
	alive := make(map[string]bool, len(live))
	for _, p := range live {
		alive[p.PaneID] = true
	}
	var err error
	dropped := false
	for tab, pane := range records {
		if !alive[pane] {
			delete(records, tab)
			dropped = true
		}
	}
	if dropped {
		err = l.writeRecords(records)
	}
	kept := make([]string, 0, len(overlays))
	for _, id := range overlays {
		if alive[id] {
			kept = append(kept, id)
		}
	}
	if len(kept) != len(overlays) {
		err = errors.Join(err, l.writeJSON(filepath.Join(l.StateDir, overlaysFile), kept))
	}
	return kept, err
}

// paneLayout reads the tab geometry around one pane.
func (l *Launcher) paneLayout(ctx context.Context, pane string) (*layout, error) {
	out, err := l.run(ctx, "pane", "layout", "--pane", pane)
	if err != nil {
		return nil, err
	}
	var envelope layoutEnvelope
	if err = json.Unmarshal([]byte(out), &envelope); err != nil {
		return nil, fmt.Errorf("decode Herdr pane layout: %w", err)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("Herdr %s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if envelope.Result.Layout == nil {
		return nil, fmt.Errorf("Herdr pane layout response missing layout")
	}
	return envelope.Result.Layout, nil
}

// splitTarget picks the pane to split so that Glance becomes the tab's
// rightmost column. Herdr 0.8.2 cannot insert a column beside an existing split
// without rebuilding the tab, so the best available placement is the right edge
// of the layout: the full-height pane there when the column holds one pane,
// otherwise its tallest (topmost on a tie) pane. Recorded Glance panes are never
// split, and any geometry failure falls back to the invoking pane.
func (l *Launcher) splitTarget(ctx context.Context, glance map[string]bool) string {
	if l.TargetPaneID == "" {
		return ""
	}
	geometry, err := l.paneLayout(ctx, l.TargetPaneID)
	if err != nil {
		l.warn(err)
		return l.TargetPaneID
	}
	if geometry.Zoomed {
		// A zoomed tab reports one pane covering the area; splitting it would
		// place Glance inside the zoom rather than beside the column.
		return l.TargetPaneID
	}
	edge := geometry.Area.X + geometry.Area.Width
	best, found := layoutPane{}, false
	for _, p := range geometry.Panes {
		if glance[p.PaneID] || p.Rect.X+p.Rect.Width != edge {
			continue
		}
		if p.Rect.Height == geometry.Area.Height {
			return p.PaneID
		}
		if !found || p.Rect.Height > best.Rect.Height || (p.Rect.Height == best.Rect.Height && p.Rect.Y < best.Rect.Y) {
			best, found = p, true
		}
	}
	if !found {
		return l.TargetPaneID
	}
	return best.PaneID
}

// resize nudges the new split toward targetColumns. The pane is already open,
// so every geometry failure is skipped silently rather than reported.
func (l *Launcher) resize(ctx context.Context, pane string) {
	geometry, err := l.paneLayout(ctx, pane)
	if err != nil {
		return
	}
	current, ok := geometry.pane(pane)
	if !ok {
		return
	}
	split, ok := geometry.containing(current)
	if !ok || split.Rect.Width-targetColumns < minWorkingColumns {
		return
	}
	width := float64(split.Rect.Width)
	target := math.Min(targetColumns/width, maxResizeAmount)
	delta := float64(current.Width)/width - target
	// Live Herdr 0.8.2: on the right-hand pane of a `right` split (224-column
	// area, both panes 112), `--direction left --amount 0.30` moved the divider
	// left and grew Glance to 179 columns. Shrinking Glance therefore needs
	// `--direction right`, growing it `--direction left`.
	direction := "right"
	if delta < 0 {
		direction, delta = "left", -delta
	}
	for i := 0; i < maxResizeCalls && delta > resizeTolerance; i++ {
		amount := math.Min(delta, maxResizeAmount)
		args := []string{"pane", "resize", "--pane", pane, "--direction", direction,
			"--amount", strconv.FormatFloat(amount, 'f', 2, 64)}
		if _, err = l.run(ctx, args...); err != nil {
			return
		}
		delta -= amount
	}
}

func (l *layout) pane(id string) (rect, bool) {
	for _, p := range l.Panes {
		if p.PaneID == id {
			return p.Rect, true
		}
	}
	return rect{}, false
}

// containing returns the innermost horizontal split holding the pane.
func (l *layout) containing(r rect) (layoutSplit, bool) {
	var found layoutSplit
	ok := false
	for _, s := range l.Splits {
		if s.Direction != "right" || s.Rect.Width <= 0 {
			continue
		}
		if r.X < s.Rect.X || r.X+r.Width > s.Rect.X+s.Rect.Width {
			continue
		}
		if r.Y < s.Rect.Y || r.Y+r.Height > s.Rect.Y+s.Rect.Height {
			continue
		}
		if !ok || s.Rect.Width < found.Rect.Width {
			found, ok = s, true
		}
	}
	return found, ok
}
