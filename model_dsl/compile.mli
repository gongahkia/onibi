type node

type edge = {
  pattern : Zenbu_model_api.Input_event.binding_pattern;
  token : string;
  next : node;
}

type node_view = { edges : edge list; complete : Ir.transition option }

val view_node : node -> node_view

type compiled_state = { ir : Ir.state; root : node }

type t = {
  ir : Ir.t;
  source : string;
  source_fingerprint : string;
  descriptor : Zenbu_model_api.Editing_model.descriptor;
  states : compiled_state list;
}

val compile :
  source_name:string ->
  source:string ->
  (t * Diagnostic.t list, Diagnostic.t list) result

val state : t -> int -> compiled_state
val prefixes : compiled_state -> string list
val effect_description : Ir.effect -> string
