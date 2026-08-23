val apply_id : Zenbu_model_api.Command_id.t
val apply_command : Zenbu_model_api.Command.t

val selection_commands : Zenbu_model_api.Command.t list
(** Model-neutral selection algebra commands available to every editor model. *)

val apply :
  selector:Zenbu_model_api.Model_intent.selector ->
  transformation:Zenbu_model_api.Model_intent.transformation ->
  Zenbu_model_api.Model_effect.t

val merge_consecutive : Zenbu_model_api.Model_effect.t
val rotate_primary_forward : Zenbu_model_api.Model_effect.t
val rotate_primary_backward : Zenbu_model_api.Model_effect.t
val rotate_contents_forward : Zenbu_model_api.Model_effect.t
val rotate_contents_backward : Zenbu_model_api.Model_effect.t
val flip_selections : Zenbu_model_api.Model_effect.t
val ensure_selections_forward : Zenbu_model_api.Model_effect.t
