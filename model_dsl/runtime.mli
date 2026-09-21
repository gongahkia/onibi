type state

val initialize : Compile.t -> state
val grammar : state -> Compile.t

val handle_input :
  state ->
  Zenbu_model_api.Input_event.t ->
  Zenbu_model_api.Editor_context.t ->
  state * Zenbu_model_api.Model_effect.t list

val reset : state -> state
val status : state -> Zenbu_model_api.Model_status.t
val input_rules : state -> Zenbu_model_api.Input_rule.t list
val descriptor_of_state : state -> Zenbu_model_api.Editing_model.descriptor

module Adapter : sig
  include Zenbu_model_api.Editing_model.S

  val configure : Compile.t -> unit
  (** [configure] supplies a grammar only for the immediately following model
      initialization, which consumes and clears this temporary process-local
      slot. The initialized state owns the grammar; input handling never reads
      the slot. Hosts must serialize configure/initialize pairs. *)

  val grammar : state -> Compile.t
  val configure_state : state -> unit
  val clear : unit -> unit
end
