package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
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
)

// Launcher opens, focuses and sizes the Glance pane of the invoking tab. Herdr
// 0.8.2 cannot list plugin panes, so the pane opened for a tab is recorded in
// the plugin state directory and verified against a session snapshot.
type Launcher struct {
	Runner       Runner
	Bin          string
	StateDir     string
	WorkspaceID  string
	TabID        string
	TargetPaneID string
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type paneOpenEnvelope struct {
	Result struct {
		PluginPane struct {
			Pane struct {
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
// otherwise opens a right-hand split, records it and sizes it.
func (l *Launcher) Open(ctx context.Context) error {
	existing, err := l.recorded(ctx)
	if err != nil {
		return err
	}
	if existing != "" {
		_, err = l.run(ctx, "plugin", "pane", "focus", existing)
		return err
	}
	pane, err := l.openPane(ctx, "split", "--direction", "right")
	if err != nil {
		return err
	}
	if err = l.record(pane); err != nil {
		return err
	}
	l.resize(ctx, pane)
	return nil
}

// Overlay opens a transient overlay. Overlays are neither recorded nor resized.
func (l *Launcher) Overlay(ctx context.Context) error {
	_, err := l.openPane(ctx, "overlay")
	return err
}

func (l *Launcher) openPane(ctx context.Context, placement string, extra ...string) (string, error) {
	args := []string{"plugin", "pane", "open", "--plugin", pluginID, "--entrypoint", viewEntrypoint, "--placement", placement}
	args = append(args, extra...)
	switch {
	case l.TargetPaneID != "":
		args = append(args, "--target-pane", l.TargetPaneID)
	case l.WorkspaceID != "":
		args = append(args, "--workspace", l.WorkspaceID)
	}
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

func (l *Launcher) recordsPath() string { return filepath.Join(l.StateDir, "panes.json") }

// records maps tab ID to the Glance pane opened for it. An unreadable or
// corrupt file is treated as empty: a new pane is cheaper than a stuck record.
func (l *Launcher) records() map[string]string {
	m := map[string]string{}
	raw, err := os.ReadFile(l.recordsPath())
	if err != nil {
		return m
	}
	if json.Unmarshal(raw, &m) != nil {
		return map[string]string{}
	}
	return m
}
func (l *Launcher) writeRecords(m map[string]string) error {
	if err := os.MkdirAll(l.StateDir, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(m)
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
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, l.recordsPath())
}
func (l *Launcher) record(pane string) error {
	if l.TabID == "" {
		return nil
	}
	m := l.records()
	m[l.TabID] = pane
	return l.writeRecords(m)
}

// recorded returns the tab's Glance pane when the session still shows it in
// that tab, dropping the record otherwise. Ownership is never guessed.
func (l *Launcher) recorded(ctx context.Context) (string, error) {
	if l.TabID == "" {
		return "", nil
	}
	m := l.records()
	pane := m[l.TabID]
	if pane == "" {
		return "", nil
	}
	out, err := l.run(ctx, "api", "snapshot")
	if err != nil {
		return "", err
	}
	var envelope snapshotEnvelope
	if err = json.Unmarshal([]byte(out), &envelope); err != nil {
		return "", fmt.Errorf("decode Herdr snapshot: %w", err)
	}
	if envelope.Error != nil {
		return "", fmt.Errorf("Herdr %s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if envelope.Result.Snapshot == nil {
		return "", fmt.Errorf("Herdr snapshot response missing snapshot")
	}
	for _, p := range envelope.Result.Snapshot.Panes {
		if p.PaneID == pane && p.TabID == l.TabID {
			return pane, nil
		}
	}
	delete(m, l.TabID)
	return "", l.writeRecords(m)
}

// resize nudges the new split toward targetColumns. The pane is already open,
// so every geometry failure is skipped silently rather than reported.
func (l *Launcher) resize(ctx context.Context, pane string) {
	out, err := l.run(ctx, "pane", "layout", "--pane", pane)
	if err != nil {
		return
	}
	var envelope layoutEnvelope
	if json.Unmarshal([]byte(out), &envelope) != nil || envelope.Error != nil || envelope.Result.Layout == nil {
		return
	}
	current, ok := envelope.Result.Layout.pane(pane)
	if !ok {
		return
	}
	split, ok := envelope.Result.Layout.containing(current)
	if !ok || split.Rect.Width-targetColumns < minWorkingColumns {
		return
	}
	width := float64(split.Rect.Width)
	target := math.Min(targetColumns/width, maxResizeAmount)
	delta := float64(current.Width)/width - target
	direction := "left"
	if delta < 0 {
		direction, delta = "right", -delta
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
