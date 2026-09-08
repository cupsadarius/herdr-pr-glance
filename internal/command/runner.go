// Package command runs bounded, noninteractive argv commands without a shell.
package command

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type ExecRunner struct{ Timeout time.Duration }
type Error struct {
	Program  string
	Stderr   string
	ExitCode int
	Err      error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v: %s", e.Program, e.Err, e.Stderr) }
func (e *Error) Unwrap() error { return e.Err }
func (r ExecRunner) Run(ctx context.Context, cwd string, argv ...string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("empty command")
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = cwd
	// Do not inherit repository overrides: cwd must determine the Git checkout.
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if (key == "GIT_DIR" || key == "GIT_WORK_TREE" || key == "GIT_INDEX_FILE" || key == "GIT_COMMON_DIR" || key == "GIT_OBJECT_DIRECTORY" || key == "GIT_ALTERNATE_OBJECT_DIRECTORIES" || key == "GIT_TERMINAL_PROMPT") || key == "GH_PROMPT_DISABLED" || key == "GH_PAGER" || key == "PAGER" || key == "LC_ALL" {
			continue
		}
		cmd.Env = append(cmd.Env, v)
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "PAGER=cat", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 100 * time.Millisecond
	err := cmd.Run()
	if err != nil {
		code := -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return stdout.String(), &Error{argv[0], strings.TrimSpace(stderr.String()), code, err}
	}
	return stdout.String(), nil
}
