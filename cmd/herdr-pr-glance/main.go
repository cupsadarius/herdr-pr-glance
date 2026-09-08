// Command herdr-pr-glance shows the current branch's pull request inside Herdr.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cupsadarius/herdr-pr-glance/internal/cache"
	"github.com/cupsadarius/herdr-pr-glance/internal/command"
	"github.com/cupsadarius/herdr-pr-glance/internal/github"
	"github.com/cupsadarius/herdr-pr-glance/internal/herdr"
	"github.com/cupsadarius/herdr-pr-glance/internal/model"
	"github.com/cupsadarius/herdr-pr-glance/internal/ui"
)

// version is replaced at release build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: herdr-pr-glance view|open|overlay|snapshot --cwd PATH|version"

// actionTimeout bounds the browser and zoom commands the pane triggers.
const actionTimeout = 10 * time.Second

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "herdr-pr-glance: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	switch args[0] {
	case "view":
		return view(ctx)
	case "open":
		l, err := launcher()
		if err != nil {
			return err
		}
		return l.Open(ctx)
	case "overlay":
		l, err := launcher()
		if err != nil {
			return err
		}
		return l.Overlay(ctx)
	case "snapshot":
		return snapshot(ctx, args[1:], stdout)
	case "version":
		_, err := fmt.Fprintln(stdout, version)
		return err
	}
	return fmt.Errorf("unknown subcommand %q\n%s", args[0], usage)
}

// invocation decodes the context Herdr passes to every plugin command.
func invocation() (herdr.Invocation, error) {
	var i herdr.Invocation
	raw := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")
	if raw == "" {
		return i, nil
	}
	if err := json.Unmarshal([]byte(raw), &i); err != nil {
		return i, fmt.Errorf("decode HERDR_PLUGIN_CONTEXT_JSON: %w", err)
	}
	return i, nil
}
func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func launcher() (*herdr.Launcher, error) {
	i, err := invocation()
	if err != nil {
		return nil, err
	}
	return &herdr.Launcher{
		Runner:       command.ExecRunner{},
		Bin:          os.Getenv("HERDR_BIN_PATH"),
		StateDir:     os.Getenv("HERDR_PLUGIN_STATE_DIR"),
		WorkspaceID:  env("HERDR_WORKSPACE_ID", i.WorkspaceID),
		TabID:        env("HERDR_TAB_ID", i.TabID),
		TargetPaneID: env("HERDR_PANE_ID", i.FocusedPaneID),
	}, nil
}

// browserArgv opens a link without a shell; the URL is a single argument.
func browserArgv(url string) []string {
	if runtime.GOOS == "darwin" {
		return []string{"open", url}
	}
	return []string{"xdg-open", url}
}

func view(ctx context.Context) error {
	i, err := invocation()
	if err != nil {
		return err
	}
	runner := command.ExecRunner{}
	bin := env("HERDR_BIN_PATH", "herdr")
	self := os.Getenv("HERDR_PANE_ID")
	action := func(argv ...string) error {
		ctx, cancel := context.WithTimeout(ctx, actionTimeout)
		defer cancel()
		_, err := runner.Run(ctx, "", argv...)
		return err
	}
	resolver := herdr.NewResolver(runner, bin, i, self, nil)
	m := ui.New(resolver, github.Client{Runner: runner}, cache.New(os.Getenv("HERDR_PLUGIN_STATE_DIR")), time.Now)
	m.Open = func(url string) error { return action(browserArgv(url)...) }
	m.Zoom = func() error {
		if self == "" {
			return errors.New("zoom needs HERDR_PANE_ID")
		}
		return action(bin, "pane", "zoom", self, "--toggle")
	}
	_, err = tea.NewProgram(m).Run()
	return err
}

// snapshot prints one summary for an explicit checkout, for diagnosis outside
// Herdr. It resolves the directory directly and fetches no discussion.
func snapshot(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	cwd := flags.String("cwd", "", "checkout directory to inspect")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *cwd == "" {
		return errors.New("snapshot requires --cwd PATH")
	}
	runner := command.ExecRunner{}
	resolver := herdr.NewResolver(runner, "", herdr.Invocation{}, "", nil)
	source, err := resolver.Checkout(ctx, model.Source{CWD: *cwd})
	if err != nil {
		return err
	}
	out := struct {
		Source   model.Source
		Snapshot *model.Snapshot `json:",omitempty"`
	}{Source: source}
	if source.EmptyReason == "" {
		s, err := (github.Client{Runner: runner}).Snapshot(ctx, source)
		if err != nil {
			return err
		}
		out.Snapshot = &s
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}
