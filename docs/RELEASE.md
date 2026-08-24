# Release gate

The checked-in development identifier is `0.11.0-dev` (`VERSION` and
`Zenbu_kernel.Version.current`). A release changes both values deliberately,
updates the package metadata, and records the validation evidence below.

## Required gate

1. Start from a clean worktree and record `git status --short --branch` plus
   the release commit.
2. On a fresh Linux x86_64 clone, run `make bootstrap`, then `make check` and
   `make demo`.
3. Run `make release-check`; it verifies that WIT/Lua/API output matches the
   checked-in contract artifacts. Run `make extension-docs` and commit the
   resulting files deliberately when the contract changes.
4. Exercise the TUI in a real terminal: open OCaml and JSON files, wait for
   `language.status` on OCaml, request hover/completion, search Unicode text,
   invoke a script/plugin command from the palette, save-as, switch models,
   and paste multiline text in a text-entry state.
5. For a Component plugin, verify normal execution, a fatal callback that
   leaves the Component unavailable, normal host editing afterwards, and
   recovery after reload.
6. Re-check the worktree. The release artifact must not contain `_opam/`,
   `.zenbu/`, build output, or unreviewed generated changes.

GitHub Actions covers the source-build gate on Ubuntu 24.04 and Apple Silicon
macOS. The tag-triggered release workflow also packages Linux x86_64 and Apple
Silicon macOS archives. Each bundle includes Wasmtime, Lua 5.4, and libffi,
and its launchers set `ZENBU_LUA_LIBRARY` to the bundled copy. These are
unsigned command-line archives; code signing and notarization require separate
Apple distribution credentials and are not part of this workflow.
