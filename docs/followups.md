# Follow-ups

## Stacked pull requests — done (2026-09-08)

Requested during implementation on 2026-09-08; deferred from the initial release.

- Indicate when the current PR belongs to a stack. — done
- Allow navigation between PRs in that stack. — done

Shipped as GitHub's own stacks: the `stack` and `stackEntry` fields ride the
existing checks query, Overview lists the stack top first, and Enter, a click,
`]`, `[` and `\` move between its pull requests. See the "Stacks" section of
docs/superpowers/specs/2026-09-08-glance-pr-design.md.

## Per-directory gh account

Noted on 2026-09-08. Glance runs `gh` with Herdr's environment, so shell hooks that switch
`GH_CONFIG_DIR` by directory do not apply. Repos that need a different gh account than
Herdr inherited fail PR discovery. Options: honor a `.glance.toml`/`gh.config_dir` per
checkout, or read `GH_CONFIG_DIR` from the working pane's foreground process environment.
