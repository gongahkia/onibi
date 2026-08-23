let api_version = 1
let manifest_version = 1
let runtime_ids = [ "lua-trusted"; "wasm-component" ]

type service = {
  id : string;
  purpose : string;
  capability : Capability.t option;
  arguments : string;
  result : string;
  errors : string list;
  since : int;
}

let services =
  [
    {
      id = "document.text";
      purpose = "read an in-bounds UTF-8 byte slice of the current document";
      capability = Some Capability.Document_read;
      arguments = "start: integer, stop: integer";
      result = "UTF-8 string";
      errors = [ "capability-denied"; "invalid-range" ];
      since = 1;
    };
    {
      id = "syntax.current";
      purpose =
        "read a data-only syntax summary for the current or requested range";
      capability = Some Capability.Syntax_read;
      arguments = "zero arguments or start: integer, stop: integer";
      result = "syntax summary or nil";
      errors = [ "capability-denied"; "invalid-range" ];
      since = 1;
    };
    {
      id = "action.document-edit";
      purpose =
        "return insert, delete, or replace actions for normal transaction \
         validation";
      capability = Some Capability.Document_edit;
      arguments = "declarative action record";
      result = "normal semantic effect";
      errors = [ "capability-denied"; "invalid-action"; "transaction-rejected" ];
      since = 1;
    };
    {
      id = "action.selection-write";
      purpose = "return a selection-set action";
      capability = Some Capability.Selection_write;
      arguments = "selection descriptors and primary index";
      result = "normal semantic effect";
      errors = [ "capability-denied"; "invalid-selection" ];
      since = 1;
    };
    {
      id = "action.command-invoke";
      purpose = "invoke a registered semantic command by stable ID";
      capability = Some Capability.Command_invoke;
      arguments = "command ID";
      result = "normal semantic effect";
      errors = [ "capability-denied"; "unknown-semantic-id" ];
      since = 1;
    };
    {
      id = "action.message";
      purpose = "emit an informational host message";
      capability = Some Capability.Ui_message;
      arguments = "UTF-8 text";
      result = "normal semantic effect";
      errors = [ "capability-denied"; "invalid-action" ];
      since = 1;
    };
    {
      id = "event.subscribe";
      purpose = "register a document-changed or after-save handler";
      capability = Some Capability.Event_subscribe;
      arguments = "stable event ID and callback";
      result = "ordered event registration";
      errors = [ "capability-denied"; "contribution-not-declared" ];
      since = 1;
    };
  ]

let stable_error_codes =
  [
    "invalid-manifest";
    "incompatible-api";
    "unknown-runtime";
    "unknown-capability";
    "unknown-contribution";
    "capability-denied";
    "contribution-not-declared";
    "namespace-violation";
    "invalid-plugin-package";
    "plugin-not-active";
    "extension-runtime-error";
    "extension-abi-mismatch";
    "extension-fuel-exhausted";
    "extension-memory-exhausted";
    "extension-trap";
    "extension-response-limit";
    "extension-runtime-unavailable";
    "duplicate-id";
    "unknown-semantic-id";
    "invalid-range";
    "invalid-selection";
    "invalid-action";
    "transaction-rejected";
  ]

let wit () =
  String.trim
    {|
package zenbu:plugin@1.0.0;

/// A recursive, language-neutral representation of the data exchanged at the
/// existing Extension_host boundary. It intentionally has no document,
/// terminal, history, parser, callback, file, process, or host-object case.
interface types {
  enum path-segment-kind {
    field,
    item,
  }

  record path-segment {
    kind: path-segment-kind,
    name: string,
    index: u32,
  }

  enum value-kind {
    nil,
    boolean,
    integer,
    floating,
    text,
    items,
    fields,
  }

  /// WIT does not permit recursive type declarations. A [value] is therefore
  /// a pre-order list of typed nodes. Every node has a typed path from the
  /// root, so this is still structured Component data rather than JSON or a
  /// raw linear-memory protocol.
  record value-node {
    path: list<path-segment>,
    kind: value-kind,
    boolean: bool,
    integer: s64,
    floating: f64,
    text: string,
  }

  type value = list<value-node>;

  /// Empty input, scope, and event strings mean that the corresponding
  /// registration property is absent for this contribution class. Binding
  /// input is one to sixteen tokens separated by one ASCII space; [Space]
  /// names a literal space key.
  record registration {
    contribution: string,
    id: string,
    callback: string,
    title: string,
    description: string,
    requires-syntax: bool,
    input: string,
    scope: string,
    event: string,
  }

  record invocation {
    callback: string,
    request: value,
  }
}

interface control {
  use types.{value, registration, invocation};

  /// Runs once while the host stages an isolated plugin generation.
  register: func() -> result<list<registration>, string>;

  /// Runs a previously registered callback. The host supplies only the
  /// capability-projected, copied request data.
  invoke: func(invocation: invocation) -> result<value, string>;
}

world extension {
  export control;
}
|}
  ^ "\n"

let supports_runtime runtime = List.mem runtime runtime_ids

let markdown () =
  let buffer = Buffer.create 4096 in
  Buffer.add_string buffer "# Zenbu Extension API v1\n\n";
  Buffer.add_string buffer
    "This file is generated from `zenbu.extension.Contract`; do not edit it \
     manually.\n\n";
  Buffer.add_string buffer "## Compatibility\n\n";
  Buffer.add_string buffer
    "A manifest declaring `api = 1` is compatible with this contract. Zenbu \
     may add optional fields and services within v1, but it will not remove or \
     change required v1 behavior. Breaking changes require a new API version.\n\n";
  Buffer.add_string buffer "## Runtimes\n\n";
  List.iter
    (fun runtime -> Buffer.add_string buffer ("- `" ^ runtime ^ "`\n"))
    runtime_ids;
  Buffer.add_string buffer "\n## Contributions\n\n";
  List.iter
    (fun contribution ->
      Buffer.add_string buffer
        ("- `"
        ^ Contribution.id contribution
        ^ "`: "
        ^ Contribution.description contribution
        ^ "\n"))
    Contribution.all;
  Buffer.add_string buffer "\n## Capabilities\n\n";
  List.iter
    (fun capability ->
      Buffer.add_string buffer
        ("- `" ^ Capability.id capability ^ "`: "
        ^ Capability.description capability
        ^ "\n"))
    Capability.all;
  Buffer.add_string buffer "\n## Services\n\n";
  List.iter
    (fun service ->
      Buffer.add_string buffer ("### `" ^ service.id ^ "`\n\n");
      Buffer.add_string buffer (service.purpose ^ "\n\n");
      Buffer.add_string buffer ("- Arguments: " ^ service.arguments ^ "\n");
      Buffer.add_string buffer ("- Result: " ^ service.result ^ "\n");
      Buffer.add_string buffer
        ("- Capability: "
        ^ Option.value ~default:"none"
            (Option.map Capability.id service.capability)
        ^ "\n");
      Buffer.add_string buffer
        ("- Errors: " ^ String.concat ", " service.errors ^ "\n\n"))
    services;
  Buffer.add_string buffer "## Stable extension errors\n\n";
  List.iter
    (fun code -> Buffer.add_string buffer ("- `" ^ code ^ "`\n"))
    stable_error_codes;
  Buffer.contents buffer

let lua_stub () =
  {|
---@meta
--- Zenbu Extension API v1. Generated from zenbu.extension.Contract.
---@class ZenbuExtension
---@field api_version integer
local zenbu = { api_version = 1 }

---@class ZenbuDocument
---@field id string
---@field version integer
---@field length integer

---@class ZenbuSelection
---@field anchor integer
---@field head integer
---@field start integer
---@field stop integer
---@field text string

---@class ZenbuCall
---@field context table
---@field arguments any

---@param start integer
---@param stop integer
---@return string
function zenbu.text(start, stop) end

---@param start? integer
---@param stop? integer
---@return table|nil
function zenbu.syntax(start, stop) end

---@param registration table
function zenbu.command(registration) end
---@param registration table
function zenbu.selector(registration) end
---@param registration table
function zenbu.transform(registration) end
---@class ZenbuBindingRegistration
---@field input string One to sixteen input tokens separated by one ASCII space.
---@field command string
---@field scope? string global, model:<id>, or model:<id>:<status>
---@param registration ZenbuBindingRegistration
function zenbu.bind(registration) end
---@param registration table
function zenbu.on(registration) end

return zenbu
|}
