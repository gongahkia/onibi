val apply_id : Zenbu_model_api.Command_id.t
val apply_command : Zenbu_model_api.Command.t

val apply :
  selector:Zenbu_model_api.Model_intent.selector ->
  transformation:Zenbu_model_api.Model_intent.transformation ->
  Zenbu_model_api.Model_effect.t
