type model = Vim | Selection | Structural
type host_command = Save | Quit | Force_quit | Reload_config

type inspection =
  | Why
  | Bindings
  | Commands
  | History
  | Selection_view
  | Syntax
  | Profile
  | Api
  | Scripts

type t
type outcome = Continue of t | Exit of t

val create :
  model:model ->
  ?language:string ->
  ?file_path:string ->
  ?contents:string ->
  ?trace:Zenbu_model_api.Trace.t ->
  ?profiler:Zenbu_model_api.Profiler.t ->
  ?config:Zenbu_scripting.Scripting.config ->
  dimensions:Zenbu_view.Renderer.dimensions ->
  unit ->
  (t, Zenbu_kernel.Error.t) result

val context : t -> Zenbu_model_api.Editor_context.t
val status : t -> Zenbu_model_api.Model_status.t
val filename : t -> string
val dirty : t -> bool
val handle_input : t -> Zenbu_model_api.Input_event.t -> t
val handle_host : t -> host_command -> outcome
val reload_config : t -> t
val resize : t -> columns:int -> rows:int -> t
val render : t -> t * Zenbu_view.Frame.t
val contents : t -> string
val file_path : t -> string option
val dimensions : t -> Zenbu_view.Renderer.dimensions
val notice : t -> string -> t
val inspect : t -> inspection -> string list
val toggle_inspector : t -> t
val inspector_open : t -> bool
