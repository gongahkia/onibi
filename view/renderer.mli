(** Pure projection from editor context to terminal-independent cells. *)

type dimensions = { columns : int; rows : int }
type rendered = { frame : Frame.t; viewport : Viewport.t }

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
  context:Zenbu_model_api.Editor_context.t ->
  status:Zenbu_model_api.Model_status.t ->
  filename:string ->
  dirty:bool ->
  message:string option ->
  viewport:Viewport.t ->
  dimensions:dimensions ->
  rendered
