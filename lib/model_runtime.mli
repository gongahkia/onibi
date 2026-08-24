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

  val with_model_state : t -> model_state -> t
  (** Replaces only the model-private state after the host has already validated
      a generation transition. Shared document/history state remains unchanged.
  *)

  val handle_input :
    t -> Input_event.t -> (t * step, Zenbu_kernel.Error.t) result

  val execute_effects :
    t ->
    ?augment_provenance:(Zenbu_kernel.Provenance.t -> Zenbu_kernel.Provenance.t) ->
    input:Input_event.t ->
    Model_effect.t list ->
    (t * step, Zenbu_kernel.Error.t) result

  val restore_selections :
    t ->
    selections:(int * int) list ->
    primary:int ->
    (t, Zenbu_kernel.Error.t) result
  (** Applies a host-restored selection set without changing the model's private
      input grammar state. Hosts use this when activating a saved view position.
  *)

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

  val with_macro_recording_register : t -> string option -> t
  (** Exposes only the active session macro register to the model input grammar.
      It does not expose stored macro contents or session state. *)

  val with_kill_ring : t -> Clipboard.entry list -> t
  (** Replaces the shared session kill history while retaining this runtime's
      ordinary clipboard slots. Hosts use this when synchronizing buffers. *)

  val kill_ring : t -> Clipboard.entry list

  val with_syntax_service :
    t ->
    syntax_service:Zenbu_syntax.Syntax.Service.t option ->
    (t, Zenbu_kernel.Error.t) result
  (** Rebinds the runtime to a host-selected syntax service while preserving
      shared semantic state and resetting only model-private grammar state. *)

  val context : t -> Editor_context.t
  val status : t -> Model_status.t
  val model_state : t -> model_state
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
