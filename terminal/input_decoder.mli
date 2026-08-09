(** Converts terminal-independent events to the model-neutral input protocol.
    Printable keys become committed text only when the model's generic status
    requests [Text_entry]; this module never inspects model identifiers. *)

val decode :
  input_mode:Zenbu_model_api.Model_status.input_mode ->
  Event.t ->
  (Zenbu_model_api.Input_event.t option, Zenbu_kernel.Error.t) result
