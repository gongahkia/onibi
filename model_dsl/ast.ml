type input_mode = Keys | Text
type status = { label : string; input_mode : input_mode; span : Source_span.t }

type action =
  | Apply of {
      selector : string;
      selector_span : Source_span.t;
      transformation : string;
      transformation_span : Source_span.t;
      span : Source_span.t;
    }
  | Insert_capture of {
      name : string;
      name_span : Source_span.t;
      span : Source_span.t;
    }
  | Do of { name : string; name_span : Source_span.t; span : Source_span.t }
  | Command of { id : string; id_span : Source_span.t; span : Source_span.t }

type guard =
  | Always
  | When_selection_any_nonempty of Source_span.t
  | Else of Source_span.t

type action_decl = {
  name : string;
  name_span : Source_span.t;
  effects : action list;
  span : Source_span.t;
}

type transition = {
  pattern : string;
  pattern_span : Source_span.t;
  capture : (string * Source_span.t) option;
  target : string;
  target_span : Source_span.t;
  guard : guard;
  effects : action list;
  span : Source_span.t;
}

type state = {
  name : string;
  name_span : Source_span.t;
  statuses : status list;
  transitions : transition list;
  span : Source_span.t;
}

type model = {
  id : string;
  id_span : Source_span.t;
  titles : (string * Source_span.t) list;
  initials : (string * Source_span.t) list;
  actions : action_decl list;
  states : state list;
  span : Source_span.t;
}

type file = { version : int; version_span : Source_span.t; model : model }
