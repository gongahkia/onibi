type kind = Builtin | Editing_model | Syntax | Application | Script | Plugin

type t = {
  id : string;
  kind : kind;
  source : string option;
  plugin_id : string option;
  version : string option;
  runtime : string option;
}

let make ~id ~kind ~source ~plugin_id ~version ~runtime =
  if String.length id = 0 then
    Error (Error.Invalid_provenance "provider id is empty")
  else Ok { id; kind; source; plugin_id; version; runtime }

let create ~id ~kind =
  make ~id ~kind ~source:None ~plugin_id:None ~version:None ~runtime:None

let create_with_source ~id ~kind ~source =
  make ~id ~kind ~source:(Some source) ~plugin_id:None ~version:None
    ~runtime:None

let create_with_metadata ~id ~kind ?source ?plugin_id ?version ?runtime () =
  make ~id ~kind ~source ~plugin_id ~version ~runtime

let id value = value.id
let kind value = value.kind
let source value = value.source
let plugin_id value = value.plugin_id
let version value = value.version
let runtime value = value.runtime

let describe value =
  match (value.plugin_id, value.version, value.runtime) with
  | Some plugin_id, Some version, Some runtime ->
      Printf.sprintf "%s@%s (%s)" plugin_id version runtime
  | Some plugin_id, Some version, None -> plugin_id ^ "@" ^ version
  | Some plugin_id, None, _ -> plugin_id
  | None, _, _ -> value.id

let kind_name = function
  | Builtin -> "builtin"
  | Editing_model -> "editing-model"
  | Syntax -> "syntax"
  | Application -> "application"
  | Script -> "script"
  | Plugin -> "plugin"
