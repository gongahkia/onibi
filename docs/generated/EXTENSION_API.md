# Zenbu Extension API v1

This file is generated from `zenbu.extension.Contract`; do not edit it manually.

## Compatibility

A manifest declaring `api = 1` is compatible with this contract. Zenbu may add optional fields and services within v1, but it will not remove or change required v1 behavior. Breaking changes require a new API version.

## Runtimes

- `lua-trusted`
- `wasm-component`

## Contributions

- `commands`: semantic command registrations
- `selectors`: dynamic selector registrations
- `transformations`: dynamic transformation registrations
- `bindings`: scoped logical input sequences
- `events`: document-changed and after-save event handlers

## Capabilities

- `document.read`: inspect copied document metadata and UTF-8 text
- `document.edit`: return declarative text edits for Zenbu validation
- `selection.read`: inspect copied current selections
- `selection.write`: return declarative selection changes
- `syntax.read`: inspect Zenbu-owned data-only syntax summaries
- `command.invoke`: invoke a registered semantic command by stable ID
- `ui.message`: emit a user-facing informational message
- `event.subscribe`: register document-changed or after-save handlers

## Services

### `document.text`

read an in-bounds UTF-8 byte slice of the current document

- Arguments: start: integer, stop: integer
- Result: UTF-8 string
- Capability: document.read
- Errors: capability-denied, invalid-range

### `syntax.current`

read a data-only syntax summary for the current or requested range

- Arguments: zero arguments or start: integer, stop: integer
- Result: syntax summary or nil
- Capability: syntax.read
- Errors: capability-denied, invalid-range

### `action.document-edit`

return insert, delete, or replace actions for normal transaction validation

- Arguments: declarative action record
- Result: normal semantic effect
- Capability: document.edit
- Errors: capability-denied, invalid-action, transaction-rejected

### `action.selection-write`

return a selection-set action

- Arguments: selection descriptors and primary index
- Result: normal semantic effect
- Capability: selection.write
- Errors: capability-denied, invalid-selection

### `action.command-invoke`

invoke a registered semantic command by stable ID

- Arguments: command ID
- Result: normal semantic effect
- Capability: command.invoke
- Errors: capability-denied, unknown-semantic-id

### `action.message`

emit an informational host message

- Arguments: UTF-8 text
- Result: normal semantic effect
- Capability: ui.message
- Errors: capability-denied, invalid-action

### `event.subscribe`

register a document-changed or after-save handler

- Arguments: stable event ID and callback
- Result: ordered event registration
- Capability: event.subscribe
- Errors: capability-denied, contribution-not-declared

## Stable extension errors

- `invalid-manifest`
- `incompatible-api`
- `unknown-runtime`
- `unknown-capability`
- `unknown-contribution`
- `capability-denied`
- `contribution-not-declared`
- `namespace-violation`
- `invalid-plugin-package`
- `plugin-not-active`
- `extension-runtime-error`
- `extension-abi-mismatch`
- `extension-fuel-exhausted`
- `extension-memory-exhausted`
- `extension-trap`
- `extension-deadline-exhausted`
- `extension-cancelled`
- `extension-response-limit`
- `extension-runtime-unavailable`
- `duplicate-id`
- `unknown-semantic-id`
- `invalid-range`
- `invalid-selection`
- `invalid-action`
- `transaction-rejected`
