let api_version = 1
let manifest_version = 1
let runtime_ids = [ "lua-trusted" ]

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
      purpose = "read a data-only syntax summary for the current or requested range";
      capability = Some Capability.Syntax_read;
      arguments = "zero arguments or start: integer, stop: integer";
      result = "syntax summary or nil";
      errors = [ "capability-denied"; "invalid-range" ];
      since = 1;
    };
    {
      id = "action.document-edit";
      purpose = "return insert, delete, or replace actions for normal transaction validation";
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
    "duplicate-id";
    "unknown-semantic-id";
    "invalid-range";
    "invalid-selection";
    "invalid-action";
    "transaction-rejected";
  ]

let supports_runtime runtime = List.mem runtime runtime_ids

let markdown () =
  let buffer = Buffer.create 4096 in
  Buffer.add_string buffer "# Zenbu Extension API v1\n\n";
  Buffer.add_string buffer
    "This file is generated from `zenbu.extension.Contract`; do not edit it manually.\n\n";
  Buffer.add_string buffer "## Compatibility\n\n";
  Buffer.add_string buffer
    "A manifest declaring `api = 1` is compatible with this contract. Zenbu may add optional fields and services within v1, but it will not remove or change required v1 behavior. Breaking changes require a new API version.\n\n";
  Buffer.add_string buffer "## Runtimes\n\n";
  List.iter (fun runtime -> Buffer.add_string buffer ("- `" ^ runtime ^ "`\n")) runtime_ids;
  Buffer.add_string buffer "\n## Contributions\n\n";
  List.iter
    (fun contribution ->
      Buffer.add_string buffer
        ("- `" ^ Contribution.id contribution ^ "`: "
       ^ Contribution.description contribution ^ "\n"))
    Contribution.all;
  Buffer.add_string buffer "\n## Capabilities\n\n";
  List.iter
    (fun capability ->
      Buffer.add_string buffer
        ("- `" ^ Capability.id capability ^ "`: "
       ^ Capability.description capability ^ "\n"))
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
       ^ Option.value ~default:"none" (Option.map Capability.id service.capability)
       ^ "\n");
      Buffer.add_string buffer ("- Errors: " ^ String.concat ", " service.errors ^ "\n\n"))
    services;
  Buffer.add_string buffer "## Stable extension errors\n\n";
  List.iter (fun code -> Buffer.add_string buffer ("- `" ^ code ^ "`\n")) stable_error_codes;
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
---@param registration table
function zenbu.bind(registration) end
---@param registration table
function zenbu.on(registration) end

return zenbu
|}
