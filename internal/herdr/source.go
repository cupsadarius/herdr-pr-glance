// Package herdr resolves a tab's working checkout through Herdr's public API.
package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/cupsadarius/pr-glance/command"
	"github.com/cupsadarius/pr-glance/glance"
	"github.com/cupsadarius/pr-glance/model"
)

type Runner interface {
	Run(context.Context, string, ...string) (string, error)
}
type Invocation struct {
	WorkspaceID    string `json:"workspace_id"`
	TabID          string `json:"tab_id"`
	FocusedPaneID  string `json:"focused_pane_id"`
	FocusedPaneCWD string `json:"focused_pane_cwd"`
	WorkspaceCWD   string `json:"workspace_cwd"`
}
type Pane struct {
	PaneID        string `json:"pane_id"`
	WorkspaceID   string `json:"workspace_id"`
	TabID         string `json:"tab_id"`
	CWD           string `json:"cwd"`
	ForegroundCWD string `json:"foreground_cwd"`
	Focused       bool   `json:"focused"`
}
type Snapshot struct {
	FocusedWorkspaceID string `json:"focused_workspace_id"`
	FocusedTabID       string `json:"focused_tab_id"`
	FocusedPaneID      string `json:"focused_pane_id"`
	Panes              []Pane `json:"panes"`
}
type snapshotEnvelope struct {
	Result struct {
		Snapshot *Snapshot `json:"snapshot"`
	} `json:"result"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Resolver retains the last observed working pane. Herdr 0.8.2 does not expose
// arbitrary plugin ownership; callers must supply known plugin pane IDs.
// Resolve calls should be serialized by the consumer to avoid obsolete results.
type Resolver struct {
	runner     Runner
	bin        string
	invocation Invocation
	mu         sync.Mutex
	last       string
	excluded   map[string]bool
	// recorded holds pane IDs the launcher wrote to its state files. They are
	// skipped only while the live pane runs in pluginRoot, because Herdr pane
	// IDs are short handles a later session can reuse for a working pane.
	recorded   map[string]bool
	pluginRoot string
}

func NewResolver(runner Runner, bin string, invocation Invocation, self string, excluded []string) *Resolver {
	if runner == nil {
		runner = command.ExecRunner{}
	}
	if bin == "" {
		bin = os.Getenv("HERDR_BIN_PATH")
		if bin == "" {
			bin = "herdr"
		}
	}
	r := &Resolver{runner: runner, bin: bin, invocation: invocation, last: invocation.FocusedPaneID,
		excluded: map[string]bool{}, recorded: map[string]bool{}, pluginRoot: os.Getenv("HERDR_PLUGIN_ROOT")}
	r.excluded[self] = true
	for _, id := range excluded {
		r.excluded[id] = true
	}
	return r
}
func (r *Resolver) ExcludePane(id string) { r.mu.Lock(); defer r.mu.Unlock(); r.excluded[id] = true }

// SetRecorded replaces the launcher's recorded Glance pane IDs. Unlike
// ExcludePane it forgets IDs that disappear from the records, and it only
// takes effect for panes actually running in the plugin root.
func (r *Resolver) SetRecorded(ids []string) {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = m
}

// isGlance reports a recorded pane that still runs where Herdr starts plugin
// commands. A reused ID pointing at a real checkout stays selectable.
func (r *Resolver) isGlance(p Pane) bool {
	if r.pluginRoot == "" || !r.recorded[p.PaneID] {
		return false
	}
	return p.CWD == r.pluginRoot || p.ForegroundCWD == r.pluginRoot
}
func (r *Resolver) Select(snapshot Snapshot) model.Source {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := model.Source{WorkspaceID: r.invocation.WorkspaceID, TabID: r.invocation.TabID, Visible: snapshot.FocusedWorkspaceID == r.invocation.WorkspaceID && snapshot.FocusedTabID == r.invocation.TabID}
	var candidates []Pane
	for _, p := range snapshot.Panes {
		if p.WorkspaceID == s.WorkspaceID && p.TabID == s.TabID && !r.excluded[p.PaneID] && !r.isGlance(p) {
			candidates = append(candidates, p)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].PaneID < candidates[j].PaneID })
	if len(candidates) == 0 {
		s.EmptyReason = model.NoWorkingPane
		r.last = ""
		return s
	}
	chosen := candidates[0]
	for _, p := range candidates {
		if p.PaneID == r.last {
			chosen = p
		}
	}
	for _, p := range candidates {
		if p.PaneID == snapshot.FocusedPaneID {
			chosen = p
			break
		}
	}
	r.last = chosen.PaneID
	s.PaneID = chosen.PaneID
	s.CWD = chosen.ForegroundCWD
	if s.CWD == "" {
		s.CWD = chosen.CWD
	}
	if s.CWD == "" {
		s.CWD = r.invocation.WorkspaceCWD
	}
	if s.CWD == "" {
		s.EmptyReason = model.NoDirectory
	}
	return s
}
func (r *Resolver) Resolve(ctx context.Context) (model.Source, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := r.runner.Run(ctx, "", r.bin, "api", "snapshot")
	if err != nil {
		return model.Source{}, err
	}
	var envelope snapshotEnvelope
	if err = json.Unmarshal([]byte(out), &envelope); err != nil {
		return model.Source{}, fmt.Errorf("decode Herdr snapshot: %w", err)
	}
	if envelope.Error != nil {
		return model.Source{}, fmt.Errorf("Herdr %s: %s", envelope.Error.Code, envelope.Error.Message)
	}
	if envelope.Result.Snapshot == nil {
		return model.Source{}, fmt.Errorf("Herdr snapshot response missing snapshot")
	}
	source := r.Select(*envelope.Result.Snapshot)
	if source.EmptyReason != "" {
		return source, nil
	}
	return glance.Checkout(ctx, source)
}
