type model = Vim | Selection | Direct | Structural

type buffer = {
  id : int;
  path : string;
  name : string option;
  language : string option;
  model : model;
}

type pane_buffer = { pane : int; buffer : int }

type viewport = {
  pane : int;
  top_line : int;
  left_column : int;
  follow_cursor : bool;
}

type selection = { anchor : int; head : int }

type view_position = {
  pane : int;
  buffer : int;
  selections : selection list;
  primary : int;
}

type t = {
  schema_version : int;
  buffers : buffer list;
  layout : Zenbu_view.Layout.persisted;
  focused_pane : int;
  pane_buffers : pane_buffer list;
  viewports : viewport list;
  view_positions : view_position list;
}

val current_schema_version : int
val model_name : model -> string
val encode : t -> string
val decode : string -> (t, string) result
