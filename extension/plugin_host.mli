(** Immutable active-plugin snapshots. A failed candidate never mutates the
    current snapshot; valid providers remain independently usable. *)

type config = Default | Directories of string list | Disabled
type state = Active | Failed
type view
type t

val default_dirs : unit -> string list

val load :
  config:config ->
  base_commands:Zenbu_model_api.Command_registry.t ->
  base_semantics:Zenbu_kernel.Semantic_descriptor.t list ->
  ?base_bindings:Zenbu_scripting.Scripting.binding list ->
  unit ->
  t

val reload :
  t ->
  base_commands:Zenbu_model_api.Command_registry.t ->
  base_semantics:Zenbu_kernel.Semantic_descriptor.t list ->
  ?base_bindings:Zenbu_scripting.Scripting.binding list ->
  unit ->
  t

val deactivate : t -> Plugin_id.t -> t
val dispose : t -> unit
val commands : t -> Zenbu_model_api.Command.t list
val semantic_behaviors : t -> Zenbu_model_api.Semantic_behavior_registry.t
val bindings : t -> Zenbu_scripting.Scripting.binding list
val hooks : t -> Zenbu_scripting.Scripting.hook list
val providers : t -> Zenbu_kernel.Provider.t list
val views : t -> view list
val find : t -> Plugin_id.t -> view option
val view_id : view -> Plugin_id.t option
val view_name : view -> string option
val view_version : view -> Plugin_version.t option
val view_api : view -> int option
val view_runtime : view -> string option
val view_manifest_path : view -> string
val view_state : view -> state
val view_requested_capabilities : view -> Capability.t list
val view_granted_capabilities : view -> Capability.t list
val view_contributions : view -> Contribution.t list
val view_registered_ids : view -> string list
val view_error : view -> Zenbu_kernel.Error.t option
val state_name : state -> string
