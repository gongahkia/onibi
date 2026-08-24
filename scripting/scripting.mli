(** Experimental M7 trusted-local scripting API. This module deliberately
    exposes semantic registrations and checked copied model state, never Lua
    values or mutable editor state. *)

type event = Zenbu_model_api.Extension_registration.event =
  | Document_changed
  | After_save

type scope = Zenbu_model_api.Extension_registration.scope =
  | Global
  | Model of string
  | Model_status of { model : string; status : string }
  | Mode of string

type mode_transition = Zenbu_model_api.Extension_registration.mode_transition =
  | Replace_mode of string
  | Push_mode of string
  | Pop_mode
  | Clear_modes

type binding = Zenbu_model_api.Extension_registration.binding
type binding_layer = Zenbu_model_api.Extension_registration.binding_layer
type hook = Zenbu_model_api.Extension_registration.hook
type mode
type model
type model_state
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
val binding_layers : t -> binding_layer list
val hooks : t -> hook list
val modes : t -> mode list
val model : t -> model option
val initial_modes : t -> string list
val counts : t -> int * int * int * int * int
val dispose : t -> unit
val binding_input : binding -> Zenbu_model_api.Input_event.binding_pattern
val binding_inputs : binding -> Zenbu_model_api.Input_event.binding_pattern list
val binding_command : binding -> string
val binding_scope : binding -> scope
val binding_layer : binding -> string option
val binding_mode_transition : binding -> mode_transition option
val binding_text_argument : binding -> string option
val binding_provider : binding -> Zenbu_kernel.Provider.t
val binding_layer_id : binding_layer -> string
val binding_layer_title : binding_layer -> string
val binding_layer_description : binding_layer -> string
val binding_layer_priority : binding_layer -> int
val binding_layer_provider : binding_layer -> Zenbu_kernel.Provider.t
val mode_id : mode -> string
val mode_title : mode -> string
val mode_description : mode -> string
val mode_input_mode : mode -> Zenbu_model_api.Model_status.input_mode
val model_descriptor : model -> Zenbu_model_api.Editing_model.descriptor
val initial_model_state : model -> model_state
val reset_model_state : model_state -> model_state
val model_state_status : model_state -> Zenbu_model_api.Model_status.t

val model_state_descriptor :
  model_state -> Zenbu_model_api.Editing_model.descriptor

val model_state_model : model_state -> model

val run_model :
  model_state ->
  Zenbu_model_api.Input_event.t ->
  Zenbu_model_api.Editor_context.t ->
  ( model_state * Zenbu_model_api.Model_effect.t list,
    Zenbu_kernel.Error.t )
  result

val hook_event : hook -> event
val hook_provider : hook -> Zenbu_kernel.Provider.t

val run_hook :
  hook ->
  Zenbu_model_api.Editor_context.t ->
  (Zenbu_model_api.Model_effect.t list, Zenbu_kernel.Error.t) result
