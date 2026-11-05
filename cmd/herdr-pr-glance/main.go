// Command herdr-pr-glance shows the current branch's pull request inside Herdr.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cupsadarius/herdr-pr-glance/internal/herdr"
	"github.com/cupsadarius/pr-glance/command"
	"github.com/cupsadarius/pr-glance/glance"
	"github.com/cupsadarius/pr-glance/model"
)

// version is replaced at release build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = "usage: herdr-pr-glance view|open|overlay|version"

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
		return view(ctx, args[1:])
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

// stateDir refuses to fall back to a relative path: Herdr runs plugin commands
// with the plugin checkout as cwd, and cached discussions and pane records must
// never land there.
func stateDir() (string, error) {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		return "", errors.New("HERDR_PLUGIN_STATE_DIR is not set; run inside Herdr")
	}
	return dir, nil
}
func launcher() (*herdr.Launcher, error) {
	i, err := invocation()
	if err != nil {
		return nil, err
	}
	state, err := stateDir()
	if err != nil {
		return nil, err
	}
	return &herdr.Launcher{
		Runner:       command.ExecRunner{},
		Bin:          os.Getenv("HERDR_BIN_PATH"),
		StateDir:     state,
		WorkspaceID:  env("HERDR_WORKSPACE_ID", i.WorkspaceID),
		TabID:        env("HERDR_TAB_ID", i.TabID),
		TargetPaneID: env("HERDR_PANE_ID", i.FocusedPaneID),
	}, nil
}

// excludingResolver keeps the resolver's recorded-pane set in step with the
// launcher's state files, so a second Glance pane in the tab (an overlay beside
// a split) is never mistaken for the working pane. The set is replaced rather
// than accumulated, and the resolver only honours it for panes running in the
// plugin root, so a reused pane ID cannot hide a real working pane.
type excludingResolver struct {
	*herdr.Resolver
	stateDir string
	self     string
}

func (r excludingResolver) Resolve(ctx context.Context) (model.Source, error) {
	r.exclude()
	return r.Resolver.Resolve(ctx)
}
func (r excludingResolver) exclude() {
	recorded := herdr.RecordedPanes(r.stateDir)
	kept := make([]string, 0, len(recorded))
	for _, id := range recorded {
		if id != r.self {
			kept = append(kept, id)
		}
	}
	r.Resolver.SetRecorded(kept)
}

func view(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("view takes no arguments\n%s", usage)
	}
	i, err := invocation()
	if err != nil {
		return err
	}
	state, err := stateDir()
	if err != nil {
		return err
	}
	runner := command.ExecRunner{}
	bin := env("HERDR_BIN_PATH", "herdr")
	self := os.Getenv("HERDR_PANE_ID")
	resolver := excludingResolver{herdr.NewResolver(runner, bin, i, self, nil), state, self}
	resolver.exclude()
	zoom := func() error {
		if self == "" {
			return errors.New("zoom needs HERDR_PANE_ID")
		}
		ctx, cancel := context.WithTimeout(ctx, glance.ActionTimeout)
		defer cancel()
		_, err := runner.Run(ctx, "", bin, "pane", "zoom", self, "--toggle")
		return err
	}
	return glance.Run(ctx, glance.Options{Resolver: resolver, StateDir: state, Zoom: zoom})
}
