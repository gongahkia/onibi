(** Pure projection from editor context to terminal-independent cells. *)

type dimensions = { columns : int; rows : int }
type rendered = { frame : Frame.t; viewport : Viewport.t }
type syntax_class = Keyword | String | Number | Comment | Type | Constructor

type syntax_span = {
  start_offset : int;
  stop_offset : int;
  class_ : syntax_class;
}
type semantic_class = Namespace | Semantic_type | Semantic_function | Semantic_variable | Semantic_property | Semantic_modifier
type semantic_span = { start_offset : int; stop_offset : int; class_ : semantic_class }

type search_range = { start_offset : int; stop_offset : int }
type diagnostic_kind = Error | Warning | Information | Hint

type diagnostic_range = {
  start_offset : int;
  stop_offset : int;
  kind : diagnostic_kind;
}

val gutter_width : Presentation.t -> Display.source_line list -> int -> int
(** Number of renderer-owned line-number columns for a pane. *)

val render_with_presentation :
  presentation:Presentation.t ->
  context:Zenbu_model_api.Editor_context.t ->
  status:Zenbu_model_api.Model_status.t ->
  filename:string ->
  dirty:bool ->
  message:string option ->
  viewport:Viewport.t ->
  dimensions:dimensions ->
  rendered

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
  ?presentation:Presentation.t ->
  ?overlay:string list ->
  ?source_lines:Display.source_line list ->
  ?syntax_spans:syntax_span list ->
  ?semantic_spans:semantic_span list ->
  ?search_ranges:search_range list ->
  ?diagnostic_ranges:diagnostic_range list ->
  ?fold_ranges:Fold.range list ->
  ?decorations:Decoration.response list ->
  ?diagnostic_summary:string ->
  ?scroll_margin:int ->
  context:Zenbu_model_api.Editor_context.t ->
  status:Zenbu_model_api.Model_status.t ->
  filename:string ->
  dirty:bool ->
  message:string option ->
  viewport:Viewport.t ->
  dimensions:dimensions ->
  unit ->
  rendered
