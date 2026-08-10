type kind = Builtin | Editing_model | Syntax | Application | Plugin
type t = { id : string; kind : kind }

let create ~id ~kind =
  if String.length id = 0 then
    Error (Error.Invalid_provenance "provider id is empty")
  else Ok { id; kind }

let id value = value.id
let kind value = value.kind

let kind_name = function
  | Builtin -> "builtin"
  | Editing_model -> "editing-model"
  | Syntax -> "syntax"
  | Application -> "application"
  | Plugin -> "plugin"
