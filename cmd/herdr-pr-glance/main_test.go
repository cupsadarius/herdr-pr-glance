package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionAndUnknownSubcommands(t *testing.T) {
	var out bytes.Buffer
	original := version
	version = "1.2.3-test"
	t.Cleanup(func() { version = original })
	if err := run([]string{"version"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "1.2.3-test\n" {
		t.Fatalf("version output = %q", out.String())
	}
	for _, args := range [][]string{{}, {"nope"}, {"snapshot"}, {"snapshot", "--nope"}} {
		out.Reset()
		if err := run(args, &out); err == nil {
			t.Fatalf("%v accepted", args)
		}
		if out.Len() != 0 {
			t.Fatalf("%v wrote to stdout: %q", args, out.String())
		}
	}
}

func TestBrowserArgvPassesTheURLAsOneArgument(t *testing.T) {
	argv := browserArgv("https://example.invalid/a b?x=1&y=2")
	if len(argv) != 2 || argv[1] != "https://example.invalid/a b?x=1&y=2" {
		t.Fatalf("argv = %q", argv)
	}
}

func TestInvocationRejectsMalformedContext(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", "{")
	if _, err := invocation(); err == nil {
		t.Fatal("accepted malformed context")
	}
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", `{"workspace_id":"w1","tab_id":"w1:t1"}`)
	i, err := invocation()
	if err != nil || i.TabID != "w1:t1" {
		t.Fatalf("%+v %v", i, err)
	}
}

func TestLauncherPrefersRuntimeEnvironment(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", `{"workspace_id":"w1","tab_id":"w1:t1","focused_pane_id":"w1:p1"}`)
	t.Setenv("HERDR_PANE_ID", "w1:p7")
	t.Setenv("HERDR_TAB_ID", "")
	t.Setenv("HERDR_WORKSPACE_ID", "")
	l, err := launcher()
	if err != nil {
		t.Fatal(err)
	}
	if l.TargetPaneID != "w1:p7" || l.TabID != "w1:t1" || l.WorkspaceID != "w1" {
		t.Fatalf("%+v", l)
	}
}

// TestSnapshotOnATemporaryCheckout exercises the diagnosis path end to end with
// a fake gh on PATH, so no network or credentials are involved.
func TestSnapshotOnATemporaryCheckout(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-b", "feature")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'no pull requests found for branch \"feature\"' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var out bytes.Buffer
	// gh reports no pull request: the summary is empty, not an error.
	if err := run([]string{"snapshot", "--cwd", dir}, &out); err != nil {
		t.Fatalf("%v %s", err, out.String())
	}
	var decoded struct {
		Source struct {
			Branch, Root, EmptyReason string
		}
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	if decoded.Source.Branch != "feature" || decoded.Source.Root == "" {
		t.Fatalf("source = %+v", decoded.Source)
	}
	if !strings.Contains(out.String(), `"EmptyReason": "no_pr"`) {
		t.Fatalf("summary missing the no-PR state: %s", out.String())
	}
}
