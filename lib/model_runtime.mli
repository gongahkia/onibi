module Make (Model : Editing_model.S) : sig
  type model_state = Model.state
  type t
  type step

  val create :
    ?commands:Command_registry.t ->
    ?syntax_service:Zenbu_syntax.Syntax.Service.t ->
    document:Zenbu_kernel.Document.t ->
    unit ->
    (t, Zenbu_kernel.Error.t) result

  val handle_input :
    t -> Input_event.t -> (t * step, Zenbu_kernel.Error.t) result

  val reset : t -> (t, Zenbu_kernel.Error.t) result
  val history : t -> Zenbu_kernel.History.t
  val context : t -> Editor_context.t
  val status : t -> Model_status.t
  val model_descriptor : t -> Editing_model.descriptor
  val input_trace : t -> Input_event.t list
  val input : step -> Input_event.t
  val effects : step -> Model_effect.t list
  val intents : step -> Model_intent.t list
  val messages : step -> Model_effect.message list
  val change_ids : step -> int list
  val document_version : step -> int
  val status_after : step -> Model_status.t
end
