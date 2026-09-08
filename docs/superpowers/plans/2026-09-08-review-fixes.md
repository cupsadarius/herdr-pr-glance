# Review fixes implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development with the already approved Astra low implementer, Astra medium spec/quality gates, and parent final pass.

**Goal:** Fix the four confirmed findings from review of commit `380f148`.

**Architecture:** Keep the existing launcher and Bubble Tea interfaces. Recover from stale pane ownership at the launch boundary; preserve lifecycle/check outcomes in rendering; derive mouse hit targets without mutating the model during View.

**Tech Stack:** Go 1.25+, Bubble Tea v2.0.9, Herdr 0.8.2 CLI.

**Spec:** [Glance PR design](../specs/2026-09-08-glance-pr-design.md), plus the user's instruction to fix the review findings.

## Global constraints

- Work on the current feature branch. Local implementation commits are authorized; do not push or publish.
- Preserve argv execution, bounded Herdr commands, source tracking, cache/refresh behavior and private state.
- Keep changes confined to these four findings. Submitted-review bodies and stacked PRs remain separate scope questions/follow-ups.
- Use regression tests before changes, focused verification, and separate spec/quality gates.

## Task 1: Resolve the confirmed review findings

**Files:** `internal/herdr/panes.go`, `internal/herdr/panes_test.go`; `internal/ui/view.go`, `internal/ui/model.go`, `internal/ui/input.go`, and corresponding UI tests. Adjust `cmd/herdr-pr-glance/main.go` and its tests only if needed to carry a session identity for pane records.

**Interfaces:** Preserve `Launcher.Open(context.Context) error`, `Model.View() tea.View` and `Model.Update(tea.Msg) (tea.Model, tea.Cmd)`.

- [x] Reproduce stale records: a saved tab/pane handle names an ordinary pane after session restart; opening must create/focus a real Glance pane instead of repeatedly returning `plugin_pane_not_found`. Preserve unexpected focus errors as errors. Validate successful focus response ownership as well, so a different plugin is not accepted as Glance. Separate pane records across sessions if needed; shared PR discussion caches remain canonical per PR. Existing correct-pane focus remains idempotent. Test with structured Herdr responses, not an arbitrary substring match. Existing reproduction: `/tmp/glance-current-review/pane_reuse_test.go`.
- [x] Reproduce long check names at widths 20 and 44. For `integration / ubuntu-latest / go-1.25 / database`, failed, cancelled, timed_out and action_required must remain distinguishable, with every row within display-cell width. Reserve outcome-label space and truncate the check name; retain readable names, Unicode/control sanitization and unknown/neutral/skipped outcomes. Existing reproduction: `/tmp/glance-current-review/spec-overlay.json` (only the check-state and closed-draft tests are in scope).
- [x] Reproduce closed draft state. `prState(model.Snapshot{State:"CLOSED", Draft:true})` must retain CLOSED; merged retains MERGED, open draft retains DRAFT, ordinary open retains OPEN. Restrict the draft presentation to active/open PRs.
- [x] Reproduce render mutation and mouse replay without View. Render and hit-test through a shared pure layout calculation, for example `layoutView() (string, []rowTarget)`, instead of persisting `m.rows` in View. Test identical mouse selection with and without intervening renders, including resize, scrolling and thread expansion; repeated View calls must leave model state unchanged. Keep visual layout and click targets aligned.
- [x] Implement the minimal fixes after their failing tests, run covering Herdr/UI/CLI tests, UI race checks, then all-package tests and vet. Format changed Go files and check the diff.
- [x] Self-review and commit source/tests with conventional messages; record RED/GREEN commands and outputs in the task report.
- [x] Run Astra medium spec and quality gates; resolve important findings and perform scoped re-review.
- [x] Parent reviews the integrated fix and records final verification and any remaining limits.

## Completion record

Implementation commit: `2af2023` (`fix: recover stale panes and preserve UI state and hit targets`). Astra low implemented the four fixes with failing regression tests first. Separate Astra medium spec and quality gates approved the final diff without findings; the parent completed the integrated review.

Final verification at `2af2023`:

- `GOCACHE=/tmp/glance-review-go-cache go test ./... -count=1` — passed all packages, including installer integration tests (parent run).
- `GOCACHE=/tmp/glance-review-go-cache go vet ./...` — passed without output (parent run).
- `GOCACHE=/tmp/glance-review-go-cache go test ./internal/ui -race -count=1` — passed (implementer run).
- `git diff 380f148..2af2023 --check` — passed.

Pane recovery uses the structured ownership/error contract in [Herdr 0.8.2](https://github.com/herdrdev/herdr/blob/v0.8.2/src/app/api/plugins/mod.rs#L430-L458). Focus may briefly select a different plugin before the response identifies it; recovery then explicitly targets and focuses the new Glance pane. No live Herdr session was changed during this fix pass. Session record namespaces are unchanged; this fix validates ownership when reusing a recorded pane.

Submitted-review bodies and stacked PR navigation remain follow-up scope. Work is committed locally; nothing was pushed or published. Detailed implementer and gate reports are retained locally under `.superpowers/sdd/2026-09-08-review-fixes/`.
