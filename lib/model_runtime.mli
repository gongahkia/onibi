type shared_state

module Make (Model : Editing_model.S) : sig
  type model_state = Model.state
  type t
  type step

  val create :
    ?commands:Command_registry.t ->
    ?semantic_behaviors:Semantic_behavior_registry.t ->
    ?syntax_service:Zenbu_syntax.Syntax.Service.t ->
    ?trace:Trace.t ->
    ?profiler:Profiler.t ->
    document:Zenbu_kernel.Document.t ->
    unit ->
    (t, Zenbu_kernel.Error.t) result

  val shared_state : t -> shared_state
  (** Preserves document history, clipboard contents, extension registries,
      trace/profiling handles, replayable intents, and execution identity while
      deliberately resetting model-private input grammar state. *)

  val create_from_shared : shared_state -> (t, Zenbu_kernel.Error.t) result

  val handle_input :
    t -> Input_event.t -> (t * step, Zenbu_kernel.Error.t) result

  val execute_effects :
    t ->
    ?augment_provenance:(Zenbu_kernel.Provenance.t -> Zenbu_kernel.Provenance.t) ->
    input:Input_event.t ->
    Model_effect.t list ->
    (t * step, Zenbu_kernel.Error.t) result

  val invoke_command :
    t ->
    ?augment_provenance:(Zenbu_kernel.Provenance.t -> Zenbu_kernel.Provenance.t) ->
    input:Input_event.t ->
    Command_invocation.t ->
    (t * step, Zenbu_kernel.Error.t) result

  val reset : t -> (t, Zenbu_kernel.Error.t) result
  val history : t -> Zenbu_kernel.History.t
  val commands : t -> Command_registry.t
  val semantic_behaviors : t -> Semantic_behavior_registry.t

  val with_extensions :
    t ->
    commands:Command_registry.t ->
    semantic_behaviors:Semantic_behavior_registry.t ->
    t

  val context : t -> Editor_context.t
  val status : t -> Model_status.t
  val model_descriptor : t -> Editing_model.descriptor
  val input_trace : t -> Input_event.t list
  val trace : t -> Trace.t
  val profiler : t -> Profiler.t
  val last_execution : t -> int option
  val input_rules : t -> Input_rule.t list
  val execution_id : step -> int
  val input : step -> Input_event.t
  val effects : step -> Model_effect.t list
  val intents : step -> Model_intent.t list
  val messages : step -> Model_effect.message list
  val change_ids : step -> int list
  val document_version : step -> int
  val status_before : step -> Model_status.t
  val status_after : step -> Model_status.t
end
