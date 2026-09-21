type t = Compile_internal.t

val compile :
  ?commands:Zenbu_model_api.Command_registry.t ->
  source_name:string ->
  source:string ->
  unit ->
  (t * Diagnostic.t list, Diagnostic.t list) result

val descriptor : t -> Zenbu_model_api.Editing_model.descriptor
val model_id : t -> string
val title : t -> string
val language_version : t -> int
val source_name : t -> string
val source_fingerprint : t -> string
val state_count : t -> int
val transition_count : t -> int
val prefix_count : t -> int
val action_count : t -> int
