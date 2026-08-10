(** Experimental M7 trusted-local scripting API. This module deliberately
    exposes semantic registrations, never Lua values or mutable editor state. *)

type event = Zenbu_model_api.Extension_registration.event =
  | Document_changed
  | After_save

type scope = Zenbu_model_api.Extension_registration.scope =
  | Global
  | Model of string
  | Model_status of { model : string; status : string }

type binding = Zenbu_model_api.Extension_registration.binding
type hook = Zenbu_model_api.Extension_registration.hook
type t
type config = Default | Explicit of string | Disabled

val api_version : int
val default_path : unit -> string

val load :
  generation_id:int ->
  base_commands:Zenbu_model_api.Command_registry.t ->
  base_semantics:Zenbu_kernel.Semantic_descriptor.t list ->
  config ->
  (t option, Zenbu_kernel.Error.t) result

val load_plugin :
  provider:Zenbu_kernel.Provider.t ->
  capabilities:string list ->
  contributions:string list ->
  base_commands:Zenbu_model_api.Command_registry.t ->
  base_semantics:Zenbu_kernel.Semantic_descriptor.t list ->
  entrypoint:string ->
  (t, Zenbu_kernel.Error.t) result

val check_file :
  base_commands:Zenbu_model_api.Command_registry.t ->
  base_semantics:Zenbu_kernel.Semantic_descriptor.t list ->
  string ->
  (int * int * int * int * int, Zenbu_kernel.Error.t) result

val generation_id : t -> int
val source : t -> string
val provider : t -> Zenbu_kernel.Provider.t
val commands : t -> Zenbu_model_api.Command.t list
val semantic_behaviors : t -> Zenbu_model_api.Semantic_behavior_registry.t
val descriptors : t -> Zenbu_kernel.Semantic_descriptor.t list
val bindings : t -> binding list
val hooks : t -> hook list
val counts : t -> int * int * int * int * int
val dispose : t -> unit
val binding_input : binding -> Zenbu_model_api.Input_event.t
val binding_command : binding -> string
val binding_scope : binding -> scope
val binding_provider : binding -> Zenbu_kernel.Provider.t
val hook_event : hook -> event
val hook_provider : hook -> Zenbu_kernel.Provider.t

val run_hook :
  hook ->
  Zenbu_model_api.Editor_context.t ->
  (Zenbu_model_api.Model_effect.t list, Zenbu_kernel.Error.t) result
