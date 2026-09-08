package command

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunnerLiteralArguments(t *testing.T) {
	dir := t.TempDir()
	arg := "path with spaces; $(touch forbidden) `echo bad`"
	got, err := (ExecRunner{}).Run(context.Background(), dir, "/usr/bin/printf", "%s", arg)
	if err != nil || got != arg {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := os.Stat(dir + "/forbidden"); !os.IsNotExist(err) {
		t.Fatal("shell syntax executed")
	}
}
func TestRunnerFailures(t *testing.T) {
	_, err := (ExecRunner{}).Run(context.Background(), "", "glance-missing-executable")
	var e *Error
	if !errors.As(err, &e) || !strings.Contains(err.Error(), "glance-missing-executable") {
		t.Fatalf("missing executable: %v", err)
	}
	_, err = (ExecRunner{Timeout: 20 * time.Millisecond}).Run(context.Background(), "", "/bin/sleep", "5")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
}
func TestRunnerNoninteractiveAndStderr(t *testing.T) {
	out, err := (ExecRunner{}).Run(context.Background(), "", "/bin/sh", "-c", "read value; printf '%s' \"$GIT_TERMINAL_PROMPT:$GH_PROMPT_DISABLED\"; printf failure >&2; exit 7")
	var e *Error
	if out != "0:1" || !errors.As(err, &e) || e.Stderr != "failure" || e.ExitCode != 7 {
		t.Fatalf("%q %#v", out, err)
	}
}
func TestRunnerUsesStableLocale(t *testing.T) {
	t.Setenv("LC_ALL", "some-other-locale")
	got, err := (ExecRunner{}).Run(context.Background(), "", "/bin/sh", "-c", "printf '%s' \"$LC_ALL\"")
	if err != nil || got != "C" {
		t.Fatalf("locale %q %v", got, err)
	}
}
