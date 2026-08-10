type kind = Builtin | Editing_model | Syntax | Application | Script | Plugin
type t = { id : string; kind : kind; source : string option }

let create ~id ~kind ?source =
  if String.length id = 0 then
    Error (Error.Invalid_provenance "provider id is empty")
  else Ok { id; kind; source }

let id value = value.id
let kind value = value.kind
let source value = value.source

let kind_name = function
  | Builtin -> "builtin"
  | Editing_model -> "editing-model"
  | Syntax -> "syntax"
  | Application -> "application"
  | Script -> "script"
  | Plugin -> "plugin"
