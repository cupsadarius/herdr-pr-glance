# Split pr-glance core from the Herdr wrapper — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the TUI, GitHub client, cache and git checkout logic into a new
module `github.com/cupsadarius/pr-glance`, and make this repo a thin Herdr
wrapper over it, with no change to what a Herdr user sees.

**Architecture:** `pr-glance` starts from this repo's full history, renames its
module, makes `model` and `command` public, and adds a public `glance` package
(`Run`, `Checkout`, `Options`) as the only entry point a wrapper calls. This
repo then deletes everything that moved and imports `pr-glance` at a tag.

**Tech Stack:** Go 1.25, Bubble Tea v2, lipgloss v2, GoReleaser v2, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-05-split-and-readiness-overview-design.md`
(Part 1, steps 1–2 of "Order of work"). The Rex wrapper (step 3) and the
Overview redesign (step 4) get their own plans.

## Global Constraints

- Structure only. No behaviour change in the TUI, the GitHub queries or the cache format.
- Herdr plugin id `glance.pr`, binary name `herdr-pr-glance`, and `herdr plugin install cupsadarius/herdr-pr-glance` stay unchanged.
- Herdr subcommands kept: `view`, `open`, `overlay`, `version`. Removed: `view --cwd`, `snapshot` (now `pr-glance --cwd` and `pr-glance snapshot --cwd`).
- `pr-glance` cache directory: `$XDG_STATE_HOME/pr-glance`, default `~/.local/state/pr-glance`.
- `pr-glance` releases start at `v0.2.0`. The Herdr wrapper also goes to `0.2.0` (it drops two subcommands).
- Colours: 16 ANSI colours only (unchanged code; do not touch styles).
- Every repo keeps CI: gofmt, `go vet ./...`, `go test ./...`, `go test -race ./...`, `go mod tidy` clean.
- **Deviation from spec, recorded here:** `command` is also public in `pr-glance`. The Herdr wrapper needs `command.ExecRunner` and inspects `*command.Error`; a private copy would duplicate it (spec anti-goal).
- **Kept as today:** a nil `Zoom` is a no-op (the spec's "nil hides `z`" is behaviour; it goes to the redesign plan).
- Pushing to GitHub, tagging and releasing need the owner's explicit go-ahead at that step. Commits use `/ledger`, no Claude footers.

## Review Focus

1. Herdr pane opened from a path with spaces or shell metacharacters → checkout still resolves (pinned by moved `TestRealCheckouts`, dir `repo space; $(not-shell)`).
2. `pr-glance` run outside any git repo, or in an unborn/detached checkout → pane shows the existing empty states, no crash (pinned by moved checkout tests + Task 3 smoke step).
3. `pr-glance` started with no `--cwd` → uses the current directory, not `/` or the binary's dir (Task 3 test `TestDefaultCWDIsWorkingDirectory`).
4. Herdr build hook on a fresh install after the split → still downloads `herdr-pr-glance` for `VERSION` (pinned by `internal/installtest`, which stays in this repo unchanged).
5. Existing Herdr cache files in `HERDR_PLUGIN_STATE_DIR` → still read after upgrade (cache package moves verbatim; Task 1 requires `internal/cache` tests to pass unchanged).

---

## File Structure

`pr-glance` (sibling repo `~/Personal/pr-glance`) after Task 3:

```
pr-glance/
├── go.mod                      module github.com/cupsadarius/pr-glance
├── model/types.go              moved from internal/model (public)
├── command/runner.go           moved from internal/command (public)
├── glance/
│   ├── glance.go               Options, Resolver, Run, browserArgv
│   ├── checkout.go             Checkout (moved from herdr.Resolver.Checkout)
│   └── checkout_test.go        moved checkout tests
├── internal/{cache,github,ui}/ moved verbatim (import paths only)
├── cmd/pr-glance/{main.go,main_test.go}
├── spike/variants/main.go      kept until the redesign plan
├── .goreleaser.yaml, .github/workflows/{ci,release}.yml, VERSION, README.md, docs/releasing.md
```

This repo after Task 5: `cmd/herdr-pr-glance/`, `internal/herdr/`,
`internal/installtest/`, `scripts/install.sh`, `herdr-plugin.toml`, `docs/`,
CI and release config. Nothing else.

---

### Task 1: Bootstrap pr-glance with history and rename the module

**Files:**
- Modify (in `~/Personal/pr-glance`): `go.mod`, every `*.go` import
- Move: `internal/model/` → `model/`, `internal/command/` → `command/`

**Interfaces:**
- Produces: import paths `github.com/cupsadarius/pr-glance/model`,
  `.../command`, `.../internal/{cache,github,ui,herdr}` (herdr is deleted in Task 3).

- [ ] **Step 1: Pull this repo's history into the empty clone**

```bash
cd ~/Personal/pr-glance
git pull ../herdr-glance-pr docs/split-design
git log --oneline -1   # expect 070fe2e or later: docs: design the core split…
```

- [ ] **Step 2: Rename the module and move the two packages**

```bash
git mv internal/model model && git mv internal/command command
go mod edit -module github.com/cupsadarius/pr-glance
grep -rl 'herdr-pr-glance/internal/model\|herdr-pr-glance/internal/command\|cupsadarius/herdr-pr-glance' --include='*.go' . \
  | xargs sed -i '' -e 's#cupsadarius/herdr-pr-glance/internal/model#cupsadarius/pr-glance/model#g' \
                    -e 's#cupsadarius/herdr-pr-glance/internal/command#cupsadarius/pr-glance/command#g' \
                    -e 's#cupsadarius/herdr-pr-glance/#cupsadarius/pr-glance/#g'
```

- [ ] **Step 3: Verify nothing else changed**

Run: `gofmt -l . ; go vet ./... && go test ./...`
Expected: no gofmt output; all packages `ok`. `git diff --stat` shows only renames and import lines.

- [ ] **Step 4: Commit** with `/ledger` (type `refactor`).

---

### Task 2: Add the public `glance` package

**Files:**
- Create: `glance/glance.go`, `glance/checkout.go`, `glance/checkout_test.go`, `glance/glance_test.go`
- Modify: `internal/herdr/source.go` (Checkout delegates), `internal/herdr/source_test.go` (checkout tests move out)

**Interfaces:**
- Consumes: `ui.New(r ui.SourceResolver, g ui.GitHub, c ui.Cache, now func() time.Time) *ui.Model`; `github.Client{Runner}`; `cache.New(dir)`; `command.ExecRunner`, `*command.Error`.
- Produces:
  - `type Resolver = ui.SourceResolver` (alias, so wrappers can name it)
  - `type Options struct { Resolver Resolver; Zoom func() error; StateDir string }`
  - `func Run(ctx context.Context, o Options) error`
  - `func Checkout(ctx context.Context, s model.Source) (model.Source, error)`
  - `const ActionTimeout = 10 * time.Second` (wrappers bound their Zoom command with it)

- [ ] **Step 1: Move the checkout tests and write the browser test**

Move `TestRealCheckouts`, `TestUnbornAndMissingDirectory`,
`TestBranchNameWithMatchingTag` and the `git(t, dir, args...)` helper from
`internal/herdr/source_test.go` into `glance/checkout_test.go` (package
`glance`). Replace each `r.Checkout(ctx, src)` with `Checkout(ctx, src)`; drop
the `NewResolver` lines. Assertions stay exactly as they are.

Move `TestBrowserArgvPassesTheURLAsOneArgument` from
`cmd/herdr-pr-glance/main_test.go` into `glance/glance_test.go`, calling
`browserArgv`.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./glance/`
Expected: FAIL, `undefined: Checkout`, `undefined: browserArgv`.

- [ ] **Step 3: Implement**

- `glance/checkout.go`: `Checkout` is today's `(*herdr.Resolver).Checkout`
  body verbatim, with `command.ExecRunner{}` as the runner.
- `glance/glance.go`: `Run` does what `view()` in
  `cmd/herdr-pr-glance/main.go` does after building the resolver:
  `ExecRunner`, `ui.New(o.Resolver, github.Client{Runner: runner}, cache.New(o.StateDir), time.Now)`,
  `m.Open` runs `browserArgv(url)` bounded by `ActionTimeout`, `m.Zoom = o.Zoom`,
  `tea.NewProgram(m).Run()`. Move `browserArgv` here. Return an error if
  `o.Resolver` is nil or `o.StateDir` is empty.
- `internal/herdr/source.go`: `(*Resolver).Checkout` becomes a one-line call to
  `glance.Checkout` (kept until Task 3 deletes the package).

- [ ] **Step 4: Run all tests**

Run: `go vet ./... && go test ./...`
Expected: all `ok`.

- [ ] **Step 5: Commit** with `/ledger` (type `feat`, scope `glance`).

---

### Task 3: Make pr-glance a standalone module and binary

**Files:**
- Delete: `internal/herdr/`, `internal/installtest/`, `cmd/herdr-pr-glance/`, `herdr-plugin.toml`, `scripts/install.sh` (`docs/superpowers/` stays as history)
- Create: `cmd/pr-glance/main.go`, `cmd/pr-glance/main_test.go`
- Modify: `.goreleaser.yaml`, `.github/workflows/release.yml`, `VERSION` (`0.2.0`), `README.md`, `docs/releasing.md`

**Interfaces:**
- Consumes: `glance.Run`, `glance.Options`, `glance.Checkout`, `github.Client.Snapshot`.
- Produces: binary `pr-glance` with
  `pr-glance [--cwd PATH]`, `pr-glance snapshot --cwd PATH`, `pr-glance version`.
  `run(args []string, stdout io.Writer) error` and
  `stateDir() (string, error)` in `package main`.

- [ ] **Step 1: Write the failing tests** in `cmd/pr-glance/main_test.go`

Port from the deleted `cmd/herdr-pr-glance/main_test.go`, adapted:
- `TestVersionAndUnknownSubcommands`: `run([]string{"version"})` prints `dev\n`; `run([]string{"nope"})` errors with `unknown subcommand "nope"`.
- `TestSnapshotOnATemporaryCheckout`: same assertions, `run([]string{"snapshot", "--cwd", dir}, &out)`.
- `TestStateDirFallsBackToXDGState`: with `XDG_STATE_HOME=$tmp`, `stateDir()` returns `$tmp/pr-glance` and the dir exists; with it unset and `HOME=$tmp`, returns `$tmp/.local/state/pr-glance`.
- `TestDefaultCWDIsWorkingDirectory`: a `fixedResolver` built from `parseView([]string{})` has `cwd == os.Getwd()`; from `parseView([]string{"--cwd", "x"})` has `cwd == "x"`.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/pr-glance/`
Expected: FAIL, `undefined: run`.

- [ ] **Step 3: Implement `cmd/pr-glance/main.go`**

- `run`: no args or first arg starting with `-` → view; `snapshot`, `version`, else `unknown subcommand %q` plus usage
  `usage: pr-glance [--cwd PATH] | snapshot --cwd PATH | version`.
- `parseView(args []string) (fixedResolver, error)`: flag `--cwd`, default `os.Getwd()`.
- `fixedResolver{cwd string}.Resolve` → `glance.Checkout(ctx, model.Source{CWD: r.cwd})` with `Visible = true` (today's `dirResolver`).
- View calls `glance.Run(ctx, glance.Options{Resolver: r, StateDir: dir})`; no Zoom.
- `stateDir()`: today's `localStateDir` minus the `HERDR_PLUGIN_STATE_DIR` branch, dir name `pr-glance`.
- `snapshot`: today's `snapshot()` with `glance.Checkout` in place of the Herdr resolver.

- [ ] **Step 4: Delete the Herdr parts and fix release config**

Delete the files listed above. In `.goreleaser.yaml` replace every `herdr-pr-glance` with `pr-glance` (project, build id, main `./cmd/pr-glance`, binary, archive, `release.github.name`). In `release.yml` delete the `herdr-plugin.toml` manifest comparison; keep the tag-vs-`VERSION` and green-CI gates. Set `VERSION` to `0.2.0`. Rewrite `README.md` for standalone use (prerequisites `git` + authenticated `gh`; usage; cache dir; pointers to `herdr-pr-glance` and `rex-pr-glance`). Rewrite `docs/releasing.md` without manifest steps.

- [ ] **Step 5: Verify**

Run: `gofmt -l . ; go vet ./... && go test -race ./... && go mod tidy && git diff --exit-code go.mod go.sum && go build ./cmd/pr-glance`
Expected: clean, all `ok`, tidy unchanged.

Smoke: `./pr-glance --cwd ~/Personal/herdr-glance-pr` in a Rex split shows this repo's PR or the "no pull request" state; `q` quits. `./pr-glance --cwd /tmp` shows the not-a-git-repository state.

- [ ] **Step 6: Commit** with `/ledger` (type `refactor`).

---

### Task 4: Publish pr-glance v0.2.0 *(owner go-ahead required)*

- [ ] **Step 1:** Ask the owner to approve: push `main` to `cupsadarius/pr-glance`, tag `v0.2.0`, push the tag.
- [ ] **Step 2:** After approval: `git push -u origin HEAD:main`, wait for CI green (`gh run watch`), then `git tag v0.2.0 && git push origin v0.2.0`.
- [ ] **Step 3:** Verify: `gh release view v0.2.0 -R cupsadarius/pr-glance` lists 4 archives + `checksums.txt`; `GOPROXY=direct go list -m github.com/cupsadarius/pr-glance@v0.2.0` resolves.

---

### Task 5: Make this repo the Herdr wrapper over pr-glance v0.2.0

**Files (this repo, new branch off `docs/split-design`):**
- Delete: `internal/{model,command,cache,github,ui}/`, `spike/`
- Modify: `go.mod` (require `github.com/cupsadarius/pr-glance v0.2.0`), `internal/herdr/*.go` + tests (imports), `internal/herdr/source.go` (Checkout), `cmd/herdr-pr-glance/main.go`, `cmd/herdr-pr-glance/main_test.go`, `README.md`, `VERSION` + `herdr-plugin.toml` (`0.2.0`)

**Interfaces:**
- Consumes: `glance.Run`, `glance.Options`, `glance.Checkout`, `glance.ActionTimeout`, `model.*`, `command.ExecRunner`, `*command.Error`.
- Produces: unchanged Herdr subcommands `view`, `open`, `overlay`, `version`.

- [ ] **Step 1: Update tests first**

In `cmd/herdr-pr-glance/main_test.go` delete `TestSnapshotOnATemporaryCheckout`, `TestDirResolverShowsTheFixedCheckout`, `TestLocalStateDirFallsBackToXDGState`, `TestBrowserArgvPassesTheURLAsOneArgument`. Add to `TestVersionAndUnknownSubcommands`: `run([]string{"snapshot"})` errors with `unknown subcommand "snapshot"`. In `internal/herdr/source_test.go` delete the three checkout tests (they live in `pr-glance/glance`).

- [ ] **Step 2: Run to see the new assertion fail**

Run: `go test ./cmd/herdr-pr-glance/`
Expected: FAIL, `snapshot` still accepted.

- [ ] **Step 3: Implement**

- Delete the moved packages; `go get github.com/cupsadarius/pr-glance@v0.2.0`; rewrite imports `internal/model` → `pr-glance/model`, `internal/command` → `pr-glance/command`.
- `herdr.Resolver.Resolve` calls `glance.Checkout(ctx, source)`; delete `(*Resolver).Checkout`.
- `main.go`: delete `viewDir`, `dirResolver`, `localStateDir`, `browserArgv`, `snapshot`, the `--cwd` flag; `view` builds `excludingResolver` as today and calls
  `glance.Run(ctx, glance.Options{Resolver: resolver, StateDir: state, Zoom: zoom})`, where `zoom` runs `bin pane zoom $self --toggle` bounded by `glance.ActionTimeout`. Usage becomes `usage: herdr-pr-glance view|open|overlay|version`.
- README: drop the `view --cwd` / Rex paragraph and the `snapshot` mention; point to `pr-glance` for standalone use.
- `VERSION` and `herdr-plugin.toml` version → `0.2.0`.

- [ ] **Step 4: Verify**

Run: `gofmt -l . ; go vet ./... && go test -race ./... && go mod tidy && git diff --exit-code go.mod go.sum`
Expected: clean; `internal/herdr`, `internal/installtest`, `cmd/herdr-pr-glance` all `ok`.

Smoke in Herdr: open "Glance PR: open" in a tab whose working pane sits in this repo; pane shows the PR, follows a pane switch, `z` zooms, `o` opens the browser.

- [ ] **Step 5: Commit** with `/ledger` (type `refactor`), then `/ledger pr`. Release per `docs/releasing.md` only on the owner's go-ahead.
