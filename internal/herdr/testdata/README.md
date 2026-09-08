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

Launcher fixtures (synthetic, no real IDs):

- `pane-open.json`: `plugin.pane.open` envelope, `result.plugin_pane.pane.pane_id`
  per https://github.com/herdrdev/herdr/blob/v0.8.2/src/api/schema/plugins.rs#L364
- `layout.json`: `pane.layout` envelope with one `right` split 319 columns wide and
  a 159-column Glance pane, per
  https://github.com/herdrdev/herdr/blob/v0.8.2/src/api/schema/panes.rs#L447
- `layout-narrow.json`: an 80-column split, too narrow to keep a 40-column working
  pane beside 44 columns of Glance; the launcher skips the resize.
- `layout-zoomed.json`: a zoomed layout with no splits; the launcher skips silently.
- `layout-thin-pane.json`: a 200-column split with a 20-column Glance pane, which
  must grow (`--direction left`) toward 44 columns.
- `layout-wide-pane.json`: a 250-column Glance pane in the 319-column split, a
  shrink larger than the 0.5 Herdr accepts per call, so it takes two calls.

Resize direction was verified against live Herdr 0.8.2: on the right-hand pane of a
`right` split, `--direction left` grows the pane and `--direction right` shrinks it.
