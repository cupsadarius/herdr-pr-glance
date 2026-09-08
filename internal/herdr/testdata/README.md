# Herdr 0.8.2 fixtures

Synthetic, sanitized fixtures based on the tagged sources below and the installed
0.8.2 `api schema` / `api snapshot` response structure. No real pane IDs, paths,
repository names, agents, or terminal output are retained.

- `context.json`: `PluginInvocationContext` fields from
  https://github.com/herdrdev/herdr/blob/v0.8.2/src/api/schema/plugins.rs#L364
- `snapshot.json`: `session.snapshot` envelope (`result.snapshot`), focused IDs,
  and `PaneInfo` cwd/foreground_cwd/tab/workspace fields from
  https://github.com/herdrdev/herdr/blob/v0.8.2/src/api/schema/panes.rs#L447
  and https://herdr.dev/docs/0.8.2/socket-api/
- Live reads use argv `[HERDR_BIN_PATH, "api", "snapshot"]`.

Ownership is **not** part of PaneInfo or session.snapshot. Herdr privately stores
`state.plugin_panes`; plugin.pane.open/focus responses expose
`result.plugin_pane.pane.pane_id` alongside plugin_id/entrypoint. There is no
plugin.pane.list API in 0.8.2. The resolver excludes its own pane ID plus explicit
known IDs. It does not infer ownership from labels, titles, cwd or process names.
Unknown third-party panes cannot reliably be classified on this API version.
See https://github.com/herdrdev/herdr/blob/v0.8.2/src/app/api/plugins/panes.rs#L265

Verified launch contract (for subsequent launcher implementation):
`plugin.pane.open` accepts plugin_id, entrypoint, placement, workspace_id,
target_pane_id, direction, cwd, focus and env. There is no tab_id field; target
pane determines tab. `width`/`height` are popup dimensions, not split widths.
`pane.resize` accepts pane_id, direction and optional fractional amount, not
absolute columns. Direction is left/right/up/down. The split launcher must
calculate any approximate 44-column adjustment from current layout geometry.
Sources: plugins.rs `PluginPaneOpenParams`; panes.rs `PaneResizeParams`; and
https://github.com/herdrdev/herdr/blob/v0.8.2/src/app/api/panes.rs#L397
