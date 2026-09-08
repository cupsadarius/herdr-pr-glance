package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

func body(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// scriptedRunner answers by command prefix ("plugin pane open") and records argv.
type scriptedRunner struct {
	responses map[string]string
	failures  map[string]error
	calls     [][]string
	// before runs just ahead of the answer, so a test can change the filesystem
	// between two steps of one launcher call.
	before func(line string)
}

func (r *scriptedRunner) Run(_ context.Context, cwd string, args ...string) (string, error) {
	r.calls = append(r.calls, args)
	if cwd != "" {
		return "", errors.New("launcher must not choose a working directory")
	}
	line := strings.Join(args[1:], " ")
	if r.before != nil {
		r.before(line)
	}
	for prefix, err := range r.failures {
		if strings.HasPrefix(line, prefix) {
			return r.responses[prefix], err
		}
	}
	for prefix, out := range r.responses {
		if strings.HasPrefix(line, prefix) {
			return out, nil
		}
	}
	return "", errors.New("unexpected command: " + line)
}
func (r *scriptedRunner) find(prefix string) []string {
	for _, c := range r.calls {
		if strings.HasPrefix(strings.Join(c[1:], " "), prefix) {
			return c
		}
	}
	return nil
}
func (r *scriptedRunner) count(prefix string) int {
	n := 0
	for _, c := range r.calls {
		if strings.HasPrefix(strings.Join(c[1:], " "), prefix) {
			n++
		}
	}
	return n
}

func launcher(t *testing.T, r Runner) *Launcher {
	t.Helper()
	return &Launcher{Runner: r, Bin: "/herdr path/custom", StateDir: t.TempDir(), WorkspaceID: "w1", TabID: "w1:t1", TargetPaneID: "w1:p1"}
}
func records(t *testing.T, l *Launcher) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(l.StateDir, "panes.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(l.StateDir, "panes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("panes.json mode = %v", info.Mode().Perm())
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
func splitRunner(t *testing.T, layout string) *scriptedRunner {
	t.Helper()
	return &scriptedRunner{responses: map[string]string{
		"api snapshot":      body(t, "snapshot.json"),
		"plugin pane open":  body(t, "pane-open.json"),
		"plugin pane focus": `{"id":"fixture","result":{"type":"plugin_pane_focused","plugin_pane":{"plugin_id":"glance.pr","entrypoint":"view","pane":{"pane_id":"w1:p2"}}}}`,
		"pane layout":       body(t, layout),
		"pane resize":       `{"id":"fixture","result":{}}`,
	}}
}

func TestOpenLaunchesSplitRecordsPaneAndResizes(t *testing.T) {
	r := splitRunner(t, "layout.json")
	l := launcher(t, r)
	if err := l.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	open := r.find("plugin pane open")
	want := []string{"/herdr path/custom", "plugin", "pane", "open", "--plugin", "glance.pr", "--entrypoint", "view",
		"--placement", "split", "--direction", "right", "--target-pane", "w1:p1", "--focus"}
	if strings.Join(open, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("open argv = %q, want %q", open, want)
	}
	if got := records(t, l); got["w1:t1"] != "w1:p9" {
		t.Fatalf("records = %v", got)
	}
	resize := r.find("pane resize")
	// 44 of 319 columns is ratio 0.138; the pane holds 0.5, so it shrinks by 0.36
	// and lands near 44 columns. Shrinking the right-hand pane moves the divider
	// right (verified against live Herdr 0.8.2).
	wantResize := []string{"/herdr path/custom", "pane", "resize", "--pane", "w1:p9", "--direction", "right", "--amount", "0.36"}
	if strings.Join(resize, "\x00") != strings.Join(wantResize, "\x00") {
		t.Fatalf("resize argv = %q, want %q", resize, wantResize)
	}
	if n := r.count("pane resize"); n != 1 {
		t.Fatalf("resize calls = %d, want 1", n)
	}
	if n := r.count("plugin pane focus"); n != 0 {
		t.Fatalf("focus calls = %d, want 0", n)
	}
}

func TestOpenFocusesRecordedPaneInSameTab(t *testing.T) {
	r := splitRunner(t, "layout.json")
	l := launcher(t, r)
	if err := os.WriteFile(filepath.Join(l.StateDir, "panes.json"), []byte(`{"w1:t1":"w1:p2","w1:t9":"w1:p8"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := l.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	focus := r.find("plugin pane focus")
	want := []string{"/herdr path/custom", "plugin", "pane", "focus", "w1:p2"}
	if strings.Join(focus, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("focus argv = %q, want %q", focus, want)
	}
	if n := r.count("plugin pane open") + r.count("pane resize"); n != 0 {
		t.Fatalf("focus path opened or resized a pane: %q", r.calls)
	}
	// The other tab's pane is gone from the session, so the record is pruned.
	if got := records(t, l); got["w1:t1"] != "w1:p2" || len(got) != 1 {
		t.Fatalf("records = %v", got)
	}
}

func TestOpenDropsStaleRecord(t *testing.T) {
	for _, stale := range []string{`{"w1:t1":"w1:gone"}`, `{"w1:t1":"w1:p4"}`} {
		r := splitRunner(t, "layout.json")
		l := launcher(t, r)
		if err := os.WriteFile(filepath.Join(l.StateDir, "panes.json"), []byte(stale), 0600); err != nil {
			t.Fatal(err)
		}
		if err := l.Open(context.Background()); err != nil {
			t.Fatal(err)
		}
		if n := r.count("plugin pane focus"); n != 0 {
			t.Fatalf("%s: focused a stale pane", stale)
		}
		if r.find("plugin pane open") == nil {
			t.Fatalf("%s: no new pane opened", stale)
		}
		if got := records(t, l); got["w1:t1"] != "w1:p9" {
			t.Fatalf("%s: records = %v", stale, got)
		}
	}
}

func TestResizeSkipsNarrowAndMissingSplits(t *testing.T) {
	for _, layout := range []string{"layout-narrow.json", "layout-zoomed.json"} {
		r := splitRunner(t, layout)
		l := launcher(t, r)
		if err := l.Open(context.Background()); err != nil {
			t.Fatalf("%s: %v", layout, err)
		}
		if n := r.count("pane resize"); n != 0 {
			t.Fatalf("%s: resized %d times", layout, n)
		}
	}
}

func TestResizeGrowsThePaneAndCapsEachCall(t *testing.T) {
	r := splitRunner(t, "layout-thin-pane.json")
	if err := launcher(t, r).Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"/herdr path/custom", "pane", "resize", "--pane", "w1:p9", "--direction", "left", "--amount", "0.12"}
	if got := r.find("pane resize"); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("resize argv = %q, want %q", got, want)
	}
	if n := r.count("pane resize"); n != 1 {
		t.Fatalf("resize calls = %d, want 1", n)
	}

	r = splitRunner(t, "layout-wide-pane.json")
	if err := launcher(t, r).Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 0.78 - 0.14 exceeds the 0.5 Herdr accepts per call, so it takes two.
	var amounts []string
	for _, c := range r.calls {
		if strings.HasPrefix(strings.Join(c[1:], " "), "pane resize") {
			if c[6] != "right" {
				t.Fatalf("direction = %q", c[6])
			}
			amounts = append(amounts, c[8])
		}
	}
	if strings.Join(amounts, ",") != "0.50,0.15" {
		t.Fatalf("amounts = %q, want [0.50 0.15]", amounts)
	}
}

func TestRecordFailuresDoNotFailTheAction(t *testing.T) {
	// Both cases must fail for any uid, root included, so they rely on structure
	// rather than permissions: a state directory whose parent is a regular file
	// can never be created or written.
	blocked := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "blocker"), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(dir, "blocker", "state")
	}

	// A fresh open whose record cannot be written: one warning, pane still sized.
	r := splitRunner(t, "layout.json")
	l := launcher(t, r)
	l.StateDir = blocked(t)
	var warnings []error
	l.Warn = func(err error) { warnings = append(warnings, err) }
	if err := l.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.find("plugin pane open") == nil || r.find("pane resize") == nil {
		t.Fatalf("record failure changed the launch: %q", r.calls)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one", warnings)
	}

	// A stale record read successfully but impossible to drop: the launcher warns
	// for the drop and for the new record, and still opens and sizes a pane. The
	// state directory turns into a regular file while the snapshot is fetched.
	r = splitRunner(t, "layout.json")
	l = launcher(t, r)
	warnings = nil
	l.Warn = func(err error) { warnings = append(warnings, err) }
	if err := os.WriteFile(filepath.Join(l.StateDir, "panes.json"), []byte(`{"w1:t1":"w1:gone"}`), 0600); err != nil {
		t.Fatal(err)
	}
	r.before = func(line string) {
		if !strings.HasPrefix(line, "api snapshot") {
			return
		}
		if err := os.RemoveAll(l.StateDir); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(l.StateDir, []byte("x"), 0600); err != nil {
			t.Error(err)
		}
	}
	if err := l.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.find("plugin pane focus") != nil {
		t.Fatalf("focused a stale pane: %q", r.calls)
	}
	if r.find("plugin pane open") == nil || r.find("pane resize") == nil {
		t.Fatalf("stale record failure changed the launch: %q", r.calls)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want two", warnings)
	}
}

func TestResizeFailuresLeaveThePaneOpen(t *testing.T) {
	for _, prefix := range []string{"pane layout", "pane resize"} {
		r := splitRunner(t, "layout.json")
		r.failures = map[string]error{prefix: errors.New("boom")}
		l := launcher(t, r)
		if err := l.Open(context.Background()); err != nil {
			t.Fatalf("%s failure surfaced: %v", prefix, err)
		}
		if got := records(t, l); got["w1:t1"] != "w1:p9" {
			t.Fatalf("%s: records = %v", prefix, got)
		}
	}
	r := splitRunner(t, "layout.json")
	r.responses["pane layout"] = `{"result":{"layout":`
	if err := launcher(t, r).Open(context.Background()); err != nil {
		t.Fatalf("malformed layout surfaced: %v", err)
	}
}

// Herdr rejects a targeted overlay: "overlay and popup plugin panes target the
// active pane". The overlay argv therefore carries placement and focus only.
func TestOverlayRecordsItselfWithoutTabRecordOrResize(t *testing.T) {
	r := splitRunner(t, "layout.json")
	l := launcher(t, r)
	if err := l.Overlay(context.Background()); err != nil {
		t.Fatal(err)
	}
	open := r.find("plugin pane open")
	want := []string{"/herdr path/custom", "plugin", "pane", "open", "--plugin", "glance.pr", "--entrypoint", "view",
		"--placement", "overlay", "--focus"}
	if strings.Join(open, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("overlay argv = %q, want %q", open, want)
	}
	if n := r.count("pane layout") + r.count("pane resize"); n != 0 {
		t.Fatalf("overlay measured or resized a pane: %q", r.calls)
	}
	if _, err := os.Stat(filepath.Join(l.StateDir, "panes.json")); !os.IsNotExist(err) {
		t.Fatalf("overlay wrote a tab record: %v", err)
	}
	// The overlay pane is still remembered, so a split Glance in the same tab
	// never treats it as the working pane.
	if got := RecordedPanes(l.StateDir); len(got) != 1 || got[0] != "w1:p9" {
		t.Fatalf("recorded panes = %v", got)
	}
}

func TestRecordedPanesCoverSplitsAndOverlaysAndFeedTheResolver(t *testing.T) {
	if got := RecordedPanes(""); got != nil {
		t.Fatalf("no state directory returned %v", got)
	}
	dir := t.TempDir()
	if got := RecordedPanes(dir); len(got) != 0 {
		t.Fatalf("empty state directory returned %v", got)
	}
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("panes.json", `{"w1:t1":"w1:p3","w1:t2":"w1:p6","w1:t3":""}`)
	write("overlays.json", `["w1:p7","w1:p3",""]`)
	got := RecordedPanes(dir)
	if strings.Join(got, ",") != "w1:p3,w1:p6,w1:p7" {
		t.Fatalf("recorded panes = %v", got)
	}
	write("panes.json", "not json")
	write("overlays.json", "{}")
	if got := RecordedPanes(dir); len(got) != 0 {
		t.Fatalf("corrupt records returned %v", got)
	}

	// A split Glance excluding the recorded overlay picks the working pane.
	c, snapshot := fixture(t)
	snapshot.Panes = append(snapshot.Panes, Pane{PaneID: "w1:p7", WorkspaceID: "w1", TabID: "w1:t1", CWD: "/plugins/glance"})
	snapshot.FocusedPaneID = "w1:p7"
	write("panes.json", `{"w1:t1":"w1:p3"}`)
	write("overlays.json", `["w1:p7"]`)
	r := NewResolver(nil, "herdr", c, "w1:p3", RecordedPanes(dir))
	if got := r.Select(snapshot); got.PaneID != "w1:p1" || got.CWD != "/work/project/subdir" {
		t.Fatalf("selected %+v, want the working pane", got)
	}
}

func TestOpenReportsMalformedResponses(t *testing.T) {
	for _, response := range []string{`{`, `{}`, `{"result":{"plugin_pane":{"pane":{"pane_id":""}}}}`,
		`{"error":{"code":"unavailable","message":"retry"}}`} {
		r := splitRunner(t, "layout.json")
		r.responses["plugin pane open"] = response
		if err := launcher(t, r).Open(context.Background()); err == nil {
			t.Fatalf("accepted open response %s", response)
		}
	}
	for _, response := range []string{`{`, `{}`, `{"error":{"code":"unavailable","message":"retry"}}`} {
		r := splitRunner(t, "layout.json")
		r.responses["api snapshot"] = response
		l := launcher(t, r)
		if err := os.WriteFile(filepath.Join(l.StateDir, "panes.json"), []byte(`{"w1:t1":"w1:p2"}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := l.Open(context.Background()); err == nil {
			t.Fatalf("accepted snapshot response %s", response)
		}
	}
	r := splitRunner(t, "layout.json")
	r.failures = map[string]error{"plugin pane focus": errors.New("boom")}
	l := launcher(t, r)
	if err := os.WriteFile(filepath.Join(l.StateDir, "panes.json"), []byte(`{"w1:t1":"w1:p2"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := l.Open(context.Background()); err == nil {
		t.Fatal("accepted a failed focus")
	}
}

func TestUnreadableRecordsAreTreatedAsAbsent(t *testing.T) {
	r := splitRunner(t, "layout.json")
	l := launcher(t, r)
	if err := os.WriteFile(filepath.Join(l.StateDir, "panes.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := l.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := records(t, l); got["w1:t1"] != "w1:p9" {
		t.Fatalf("records = %v", got)
	}
}

func TestPruningDropsDeadRecordsAndKeepsLiveOnes(t *testing.T) {
	for _, action := range []string{"open", "overlay"} {
		r := splitRunner(t, "layout.json")
		l := launcher(t, r)
		seed := func(name, contents string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(l.StateDir, name), []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
		}
		// w1:p2 and w1:p4 are in the session fixture; w1:pX and w1:pY are not.
		seed("panes.json", `{"w1:t2":"w1:p4","w1:t8":"w1:pX"}`)
		seed("overlays.json", `["w1:p2","w1:pY"]`)
		var err error
		if action == "open" {
			err = l.Open(context.Background())
		} else {
			err = l.Overlay(context.Background())
		}
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		got := records(t, l)
		if got["w1:t2"] != "w1:p4" || got["w1:t8"] != "" {
			t.Fatalf("%s: records = %v", action, got)
		}
		var overlays []string
		readJSON(filepath.Join(l.StateDir, "overlays.json"), &overlays)
		if action == "open" {
			if strings.Join(overlays, ",") != "w1:p2" {
				t.Fatalf("open: overlays = %v", overlays)
			}
		} else if strings.Join(overlays, ",") != "w1:p9,w1:p2" {
			t.Fatalf("overlay: overlays = %v", overlays)
		}
	}
}

func TestRecordedPanesAreExcludedOnlyInThePluginRoot(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_ROOT", "/plugins/glance")
	c, snapshot := fixture(t)
	// w1:p2 is a working pane in /work/other; a stale record naming it must not
	// hide it, because Herdr reuses short pane IDs across sessions.
	r := NewResolver(nil, "herdr", c, "w1:p3", nil)
	r.SetRecorded([]string{"w1:p2", "w1:p3"})
	snapshot.Panes = snapshot.Panes[1:] // drop w1:p1, leaving w1:p2 and Glance
	if got := r.Select(snapshot); got.PaneID != "w1:p2" {
		t.Fatalf("selected %+v, want the reused working pane", got)
	}
	// The same ID running in the plugin root is a Glance pane and is skipped.
	snapshot.Panes[0].CWD, snapshot.Panes[0].ForegroundCWD = "/plugins/glance", ""
	if got := r.Select(snapshot); got.PaneID != "" || got.EmptyReason != model.NoWorkingPane {
		t.Fatalf("selected %+v, want no working pane", got)
	}
	// Without a plugin root there is nothing to compare against, so records are
	// ignored and only the pane's own ID is excluded.
	t.Setenv("HERDR_PLUGIN_ROOT", "")
	rootless := NewResolver(nil, "herdr", c, "w1:p3", nil)
	rootless.SetRecorded([]string{"w1:p2"})
	if got := rootless.Select(snapshot); got.PaneID != "w1:p2" {
		t.Fatalf("selected %+v, want w1:p2", got)
	}
}

func TestRecordedPaneOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, out string
		failure   error
		recover   bool
	}{
		{"ordinary pane", `{"error":{"code":"plugin_pane_not_found","message":"plugin pane not found"}}`, errors.New("exit 1"), true},
		{"structured missing", `{"error":{"code":"plugin_pane_not_found","message":"plugin pane not found"}}`, nil, true},
		{"other plugin", `{"result":{"type":"plugin_pane_focused","plugin_pane":{"plugin_id":"other.plugin","entrypoint":"view","pane":{"pane_id":"w1:p2"}}}}`, nil, true},
		{"other entrypoint", `{"result":{"plugin_pane":{"plugin_id":"glance.pr","entrypoint":"other","pane":{"pane_id":"w1:p2"}}}}`, nil, true},
		{"unexpected error", `{"error":{"code":"permission_denied","message":"plugin_pane_not_found is not the error code"}}`, nil, false},
		{"transport failure", "", errors.New("transport failed"), false},
		{"malformed success", `{}`, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := splitRunner(t, "layout.json")
			r.responses["plugin pane focus"] = tc.out
			if tc.failure != nil {
				r.failures = map[string]error{"plugin pane focus": tc.failure}
			}
			l := launcher(t, r)
			if err := l.record("w1:p2"); err != nil {
				t.Fatal(err)
			}
			err := l.Open(context.Background())
			if tc.recover {
				if err != nil {
					t.Fatalf("stale ownership blocks launch: %v", err)
				}
				if r.count("plugin pane open") != 1 || records(t, l)[l.TabID] != "w1:p9" {
					t.Fatalf("did not replace stale pane: %v", r.calls)
				}
			} else {
				if err == nil {
					t.Fatal("unexpected focus failure was swallowed")
				}
				if r.count("plugin pane open") != 0 {
					t.Fatal("opened despite unexpected focus failure")
				}
			}
		})
	}
}
