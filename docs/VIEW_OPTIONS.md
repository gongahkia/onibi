# Pane-local display options

`zenbu.view.View_options` is a small, data-only renderer policy owned by each
pane. Its default is `scroll_margin = 0` with no presentation override. A
missing override inherits the Session presentation dynamically, so a later
session-level presentation switch updates every inheriting pane without
touching panes that selected an override.

`Session.set_pane_display` accepts a pane id, a vertical scroll margin, and an
optional built-in presentation name. It validates atomically: margins are
integers from 0 through 32; an override must be a built-in profile with no
buffer line. `default`, `numbered`, `relative`, `minimal`, and `bare` are
available. `buffered` is rejected because its buffer line is workspace-wide;
custom presentation TOML files remain a session-level policy and are not
stored as host-dependent pane paths. `Session.reset_pane_display` returns a
pane to the explicit inherited default.

The margin is vertical only. During cursor follow, the renderer reserves as
many rows above and below the source cursor as the current pane height permits;
small panes reduce that reservation safely. Explicit mouse, line, page, and
center scrolling continue to set a direct viewport position. Gutter width,
status-row exclusion, cursor placement, and pointer/source conversion use the
effective presentation of the addressed pane.

Splitting copies the source pane's option snapshot. Focusing another pane,
switching that pane's buffer, resizing layouts, and changing themes do not
change its options. Closing or keeping only panes discards their associated
options. They do not alter document contents, selections, semantic history,
editing-model state, or binding state.

Workspace layouts serialize every pane option in schema version 3. Decoding
validates a complete, unique pane mapping and the same bounds as the host API.
Schema versions 1 and 2 migrate to `scroll_margin = 0` and presentation
inheritance. Decorations remain host-supplied snapshots and are intentionally
not part of this persisted policy.

This is not a general editor option registry: it provides no horizontal margin,
wrapping policy, arbitrary per-pane style values, Lua/Lisp state, GUI widgets,
or host callback execution.
