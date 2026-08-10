(** Private adapter from the M9 Component ABI to the stable extension surface. *)

type t

type limits = { fuel : int; memory_bytes : int }

val default_limits : limits

val load :
  limits:limits ->
  provider:Zenbu_kernel.Provider.t ->
  capabilities:string list ->
  contributions:string list ->
  base_commands:Zenbu_model_api.Command_registry.t ->
  base_semantics:Zenbu_kernel.Semantic_descriptor.t list ->
  entrypoint:string ->
  (t, Zenbu_kernel.Error.t) result

val provider : t -> Zenbu_kernel.Provider.t
val commands : t -> Zenbu_model_api.Command.t list
val semantic_behaviors : t -> Zenbu_model_api.Semantic_behavior_registry.t
val descriptors : t -> Zenbu_kernel.Semantic_descriptor.t list
val bindings : t -> Zenbu_model_api.Extension_registration.binding list
val hooks : t -> Zenbu_model_api.Extension_registration.hook list
val limits : t -> limits
val dispose : t -> unit
