# Snapshot command fixtures

Command-boundary fixtures are built inline in `snapshot_test.go` so pagination,
identity, and check-state variations remain visible beside their assertions.
They contain no discussion bodies.

The absence contract was verified with the installed `gh` against a temporary
checkout: exit status 1 and stderr
`no pull requests found for branch "glance-absent-contract-fixture"` followed by a
newline. `command.ExecRunner` trims stderr. Only exit code 1 with the complete message prefix and a nonempty, correctly
double-quoted Go string is classified as no associated PR. The resolved head may
be fork-qualified or differ from the local branch; malformed or appended error
text remains an error. See gh v2.100.0 `pkg/cmd/pr/shared/finder.go` and
`find_refs_resolution.go` for the resolved-head and quoting contract.

The opt-in `TestLiveSnapshot` was verified against the authorized public fixture
https://github.com/herdrdev/herdr/pull/3721 on 2026-09-08. It returned 2 commits,
1 changed file, +26/-3, and 7 passed checks. Run it only with an authorized local
checkout via `GLANCE_LIVE_PR_CWD`; ordinary tests perform no network requests.
