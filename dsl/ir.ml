type action =
  | Apply of {
      selector : Zenbu_model_api.Model_intent.selector;
      selector_id : string;
      transformation : Zenbu_model_api.Model_intent.transformation;
      transformation_id : string;
    }
  | Insert_capture of string
  | Invoke_command of {
      invocation : Zenbu_model_api.Command_invocation.t;
      command_id : string;
    }

type guard = Always | When_selection_any_nonempty | Else

type arm = {
  guard : guard;
  target : int;
  target_name : string;
  effects : action list;
  span : Source_span.t;
}

type action_declaration = {
  name : string;
  effects : action list;
  span : Source_span.t;
}

type transition = {
  id : int;
  pattern : string;
  patterns : Zenbu_model_api.Input_event.binding_pattern list;
  arms : arm list;
  span : Source_span.t;
  pattern_span : Source_span.t;
}

type state = {
  id : int;
  name : string;
  status_label : string;
  input_mode : Zenbu_model_api.Model_status.input_mode;
  transitions : transition list;
  span : Source_span.t;
}

type t = {
  version : int;
  model_id : string;
  title : string;
  source_name : string;
  actions : action_declaration list;
  states : state list;
  initial : int;
}
