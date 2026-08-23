# Themes

Zenbu's renderer emits stable semantic frame styles such as `syntax_keyword`,
`diagnostic_error`, `primary_selection`, and `status`. A terminal theme maps
those styles to foreground/background colours and text decorations. It has no
access to documents, selections, layout, or input, so changing a theme cannot
change editing behavior or bypass the transaction boundary.

Select one of the built-ins at launch:

```sh
zenbu --theme default FILE
zenbu --theme dark FILE
zenbu --theme light FILE
```

Or point `--theme` at a TOML file. A bad file fails before Zenbu takes over the
terminal.

```toml
name = "my-theme"

[plain]
foreground = "#d8dee9"
background = "#2e3440"

[syntax_keyword]
foreground = "#81a1c1"
bold = true

[diagnostic_error]
foreground = "#bf616a"
underline = true

[status]
foreground = "#eceff4"
background = "#4c566a"
```

The allowed style tables are:

```text
plain
primary_selection      secondary_selection
status                 message                 overlay
dim                    search_match
diagnostic_error       diagnostic_warning
diagnostic_information diagnostic_hint
syntax_keyword         syntax_string           syntax_number
syntax_comment         syntax_type             syntax_constructor
```

Each table may contain `foreground`, `background`, `bold`, `italic`, and
`underline`. A colour is `default` or `#RRGGBB`; booleans control their named
decoration. Omitted tables and fields inherit the built-in `default` theme.
Unknown style names and fields are rejected so a spelling mistake cannot be
silently ignored.

Zenbu sends true-colour attributes through Notty when a theme uses `#RRGGBB`.
Whether those colours render exactly is controlled by the user's terminal;
Zenbu does not detect or remap terminal colour capabilities.

Switch to a built-in or validated TOML theme while Zenbu is running through
`Ctrl-P` → `view.theme.switch`. The argument uses the same
`default|dark|light|PATH` form as `--theme`. The Session retains the validated
selection and the terminal backend applies it immediately before its next
frame draw, without changing a document, selection, history, model state, or
replay result.

Pair a theme with a pure line-number/status-row policy using
`--presentation`; see [Terminal presentation profiles](PRESENTATION.md).
Lua/plugin-defined themes, fonts, minimaps, arbitrary widgets, mouse UI, and
GUI presentation remain separate host evaluations.
