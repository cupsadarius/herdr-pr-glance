package github

import (
	"context"
	"os"
	"testing"

	"github.com/cupsadarius/herdr-pr-glance/internal/command"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

// Explicit opt-in: normal tests never access credentials or GitHub.
func TestLiveSnapshot(t *testing.T) {
	cwd := os.Getenv("GLANCE_LIVE_PR_CWD")
	if cwd == "" {
		t.Skip("set GLANCE_LIVE_PR_CWD to an authorized checkout")
	}
	s, err := (Client{Runner: command.ExecRunner{}}).Snapshot(context.Background(), model.Source{CWD: cwd, Branch: "fix/windows-endpoint-paste"})
	if err != nil {
		t.Fatal(err)
	}
	if s.PR == nil || s.PR.NodeID == "" || s.Commits < 1 {
		t.Fatalf("%+v", s)
	}
	t.Logf("%s commits=%d files=%d +%d -%d checks=%+v", s.PR.URL, s.Commits, s.ChangedFiles, s.Additions, s.Deletions, s.CheckCounts)
}
