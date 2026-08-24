(** Immutable active-plugin snapshots. A failed candidate never mutates the
    current snapshot; valid providers remain independently usable. *)

type config = Default | Directories of string list | Disabled
type state = Active | Failed
type health = Healthy | Unavailable

type runtime_event = {
  provider : Zenbu_kernel.Provider.t;
  runtime : string;
  stage : string;
  operation : string option;
  outcome : string;
  duration_seconds : float;
  fuel_consumed : int option;
  reason : string option;
}

type view
type t

val default_dirs : unit -> string list

val load :
  config:config ->
  base_commands:Zenbu_model_api.Command_registry.t ->
  base_semantics:Zenbu_kernel.Semantic_descriptor.t list ->
  ?base_bindings:Zenbu_model_api.Extension_registration.binding list ->
  ?base_binding_layers:Zenbu_model_api.Extension_registration.binding_layer list ->
  unit ->
  t

val reload :
  t ->
  base_commands:Zenbu_model_api.Command_registry.t ->
  base_semantics:Zenbu_kernel.Semantic_descriptor.t list ->
  ?base_bindings:Zenbu_model_api.Extension_registration.binding list ->
  ?base_binding_layers:Zenbu_model_api.Extension_registration.binding_layer list ->
  unit ->
  t

val deactivate : t -> Plugin_id.t -> t
val dispose : t -> unit
val commands : t -> Zenbu_model_api.Command.t list
val semantic_behaviors : t -> Zenbu_model_api.Semantic_behavior_registry.t
val bindings : t -> Zenbu_model_api.Extension_registration.binding list

val binding_layers :
  t -> Zenbu_model_api.Extension_registration.binding_layer list

val hooks : t -> Zenbu_model_api.Extension_registration.hook list
val drain_runtime_events : t -> runtime_event list
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
val view_health : view -> health
val view_requested_capabilities : view -> Capability.t list
val view_granted_capabilities : view -> Capability.t list
val view_contributions : view -> Contribution.t list
val view_registered_ids : view -> string list
val view_error : view -> Zenbu_kernel.Error.t option
val view_runtime_limits : view -> (int * int) option
val view_runtime_deadline_ms : view -> int option
val state_name : state -> string
val health_name : health -> string
