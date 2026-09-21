type effect =
  | Apply of {
      selector : Zenbu_model_api.Model_intent.selector;
      selector_id : string;
      transformation : Zenbu_model_api.Model_intent.transformation;
      transformation_id : string;
    }
  | Insert_capture of string

type transition = {
  id : int;
  pattern : string;
  patterns : Zenbu_model_api.Input_event.binding_pattern list;
  capture : string option;
  target : int;
  target_name : string;
  effects : effect list;
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
  states : state list;
  initial : int;
}
