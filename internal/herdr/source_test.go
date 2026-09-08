package herdr

import (
	"context"
	"encoding/json"
	"github.com/cupsadarius/herdr-pr-glance/internal/command"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T) (Invocation, Snapshot) {
	t.Helper()
	var c Invocation
	var e snapshotEnvelope
	b, _ := os.ReadFile("testdata/context.json")
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile("testdata/snapshot.json")
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return c, *e.Result.Snapshot
}
func TestPaneTracking(t *testing.T) {
	c, s := fixture(t)
	r := NewResolver(nil, "/herdr", c, "w1:p3", []string{"w1:p5"})
	check := func(want string) {
		t.Helper()
		got := r.Select(s)
		if got.PaneID != want {
			t.Fatalf("pane=%q want %q", got.PaneID, want)
		}
	}
	check("w1:p1")
	s.FocusedPaneID = "w1:p2"
	check("w1:p2")
	s.FocusedPaneID = "w1:p3"
	check("w1:p2")
	s.Panes = append(s.Panes, Pane{PaneID: "w1:p5", WorkspaceID: "w1", TabID: "w1:t1"})
	s.FocusedPaneID = "w1:p5"
	check("w1:p2")
	s.FocusedTabID = "w1:t2"
	s.FocusedPaneID = "w1:p4"
	check("w1:p2")
	if r.Select(s).Visible {
		t.Fatal("hidden tab visible")
	}
	s.Panes = append(s.Panes[:1], s.Panes[2:]...)
	check("w1:p1")
	s.Panes = s.Panes[1:]
	check("")
	if r.Select(s).EmptyReason != model.NoWorkingPane {
		t.Fatal("missing empty state")
	}
}
func TestSourceClosureDeterministicFallback(t *testing.T) {
	c, s := fixture(t)
	c.FocusedPaneID = "closed"
	s.Panes[0], s.Panes[1] = s.Panes[1], s.Panes[0]
	r := NewResolver(nil, "herdr", c, "w1:p3", nil)
	if got := r.Select(s); got.PaneID != "w1:p1" {
		t.Fatal(got)
	}
	s.FocusedPaneID = "w1:p2"
	if got := r.Select(s); got.PaneID != "w1:p2" {
		t.Fatal(got)
	}
}
func TestDirectoryPriority(t *testing.T) {
	c, s := fixture(t)
	r := NewResolver(nil, "herdr", c, "w1:p3", nil)
	for _, want := range []string{"/work/project/subdir", "/work/project", "/work/project"} {
		got := r.Select(s)
		if got.CWD != want {
			t.Fatalf("%q != %q", got.CWD, want)
		}
		if s.Panes[0].ForegroundCWD != "" {
			s.Panes[0].ForegroundCWD = ""
		} else {
			s.Panes[0].CWD = ""
		}
	}
}
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	v, e := (command.ExecRunner{}).Run(context.Background(), dir, append([]string{"git"}, args...)...)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestRealCheckouts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo space; $(not-shell)")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-b", "main")
	git(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
	r := NewResolver(command.ExecRunner{}, "herdr", Invocation{}, "", nil)
	check := func(cwd, branch string, reason model.EmptyReason) {
		t.Helper()
		got, err := r.Checkout(context.Background(), model.Source{CWD: cwd})
		if err != nil || got.Branch != branch || got.EmptyReason != reason {
			t.Fatalf("%+v %v", got, err)
		}
	}
	nested := filepath.Join(dir, "nested")
	os.Mkdir(nested, 0700)
	check(nested, "main", "")
	git(t, dir, "checkout", "-b", "feature")
	check(nested, "feature", "")
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, dir, "worktree", "add", "-b", "linked", linked)
	check(linked, "linked", "")
	check(dir, "feature", "")
	git(t, linked, "checkout", "--detach")
	check(linked, "", model.DetachedHEAD)
	check(t.TempDir(), "", model.NotGit)
}

type snapshotRunner struct {
	body    string
	err     error
	program string
	args    []string
}

func (r *snapshotRunner) Run(_ context.Context, _ string, args ...string) (string, error) {
	r.program = args[0]
	r.args = args[1:]
	return r.body, r.err
}
func TestResolveUsesHerdrBinaryAndReportsMalformedResponses(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", "/herdr path/custom")
	runner := &snapshotRunner{body: `{"result":{"snapshot":{"panes":[]}}}`}
	r := NewResolver(runner, "", Invocation{WorkspaceID: "w1", TabID: "w1:t1"}, "", nil)
	s, e := r.Resolve(context.Background())
	if e != nil || s.EmptyReason != model.NoWorkingPane || runner.program != "/herdr path/custom" || len(runner.args) != 2 || runner.args[0] != "api" || runner.args[1] != "snapshot" {
		t.Fatalf("%+v %v %+v", s, e, runner)
	}
	for _, body := range []string{`{`, `{}`, `{"error":{"code":"unavailable","message":"retry"}}`} {
		runner.body = body
		if _, e = r.Resolve(context.Background()); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestDynamicExclusion(t *testing.T) {
	c, s := fixture(t)
	r := NewResolver(nil, "herdr", c, "w1:p3", nil)
	r.ExcludePane("w1:p1")
	if got := r.Select(s); got.PaneID != "w1:p2" {
		t.Fatal(got)
	}
}
func TestUnbornAndMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	r := NewResolver(nil, "herdr", Invocation{}, "", nil)
	s, e := r.Checkout(context.Background(), model.Source{CWD: dir})
	if e != nil || s.EmptyReason != model.UnbornHEAD {
		t.Fatalf("%+v %v", s, e)
	}
	if _, e = r.Checkout(context.Background(), model.Source{CWD: filepath.Join(dir, "missing")}); e == nil {
		t.Fatal("missing directory treated as non-Git")
	}
}

func TestBranchNameWithMatchingTag(t *testing.T) {
	for _, branch := range []string{"main", "feature/topic"} {
		t.Run(branch, func(t *testing.T) {
			dir := t.TempDir()
			git(t, dir, "init", "-b", branch)
			git(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
			git(t, dir, "tag", branch)
			r := NewResolver(command.ExecRunner{}, "herdr", Invocation{}, "", nil)
			got, err := r.Checkout(context.Background(), model.Source{CWD: dir})
			if err != nil || got.Branch != branch {
				t.Fatalf("branch = %q, want %q; error = %v", got.Branch, branch, err)
			}
		})
	}
}
