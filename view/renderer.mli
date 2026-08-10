(** Pure projection from editor context to terminal-independent cells. *)

type dimensions = { columns : int; rows : int }
type rendered = { frame : Frame.t; viewport : Viewport.t }
type syntax_class = Keyword | String | Number | Comment | Type | Constructor

type syntax_span = {
  start_offset : int;
  stop_offset : int;
  class_ : syntax_class;
}

type search_range = { start_offset : int; stop_offset : int }
type diagnostic_kind = Error | Warning | Information | Hint

type diagnostic_range = {
  start_offset : int;
  stop_offset : int;
  kind : diagnostic_kind;
}

val render :
  context:Zenbu_model_api.Editor_context.t ->
  status:Zenbu_model_api.Model_status.t ->
  filename:string ->
  dirty:bool ->
  message:string option ->
  viewport:Viewport.t ->
  dimensions:dimensions ->
  rendered

val render_with_inspector :
  inspector:string list option ->
  ?overlay:string list ->
  ?source_lines:Display.source_line list ->
  ?syntax_spans:syntax_span list ->
  ?search_ranges:search_range list ->
  ?diagnostic_ranges:diagnostic_range list ->
  ?diagnostic_summary:string ->
  context:Zenbu_model_api.Editor_context.t ->
  status:Zenbu_model_api.Model_status.t ->
  filename:string ->
  dirty:bool ->
  message:string option ->
  viewport:Viewport.t ->
  dimensions:dimensions ->
  unit ->
  rendered
