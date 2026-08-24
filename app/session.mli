type model = Vim | Selection | Direct | Structural | Script

type host_command =
  | Save
  | Save_as
  | Save_layout
  | Restore_layout
  | Set_project_root
  | Open_file_picker
  | Search_project
  | Quit
  | Force_quit
  | Reload_config
  | Start_search
  | Start_regexp_search
  | Replace_all_literal
  | Replace_all_regexp
  | Start_query_replace_literal
  | Start_query_replace_regexp
  | Search_next
  | Search_previous
  | Toggle_macro_recording
  | Replay_macro
  | Kill_ring_cut
  | Kill_ring_yank
  | System_clipboard_copy
  | System_clipboard_paste
  | Set_location
  | Jump_location
  | Push_jump
  | Jump_backward
  | Jump_forward
  | Open_palette
  | Switch_model
  | Help
  | Switch_presentation
  | Switch_theme
  | Background_jobs
  | Cancel_background_job
  | Open_background_job_output
  | Language_status
  | Language_restart
  | Language_hover
  | Language_definition
  | Language_complete
  | Language_rename
  | Language_diagnostic_next
  | Language_diagnostic_previous
  | Language_diagnostic_describe_current
  | Split_vertical
  | Split_horizontal
  | Focus_next_pane
  | Close_pane
  | Only_pane
  | Grow_pane_width
  | Shrink_pane_width
  | Grow_pane_height
  | Shrink_pane_height
  | Balance_panes
  | New_buffer
  | Open_buffer
  | List_buffers
  | Switch_buffer
  | Rename_buffer
  | Close_buffer
  | Force_close_buffer
  | Next_buffer
  | Previous_buffer
  | View_scroll_up
  | View_scroll_down
  | View_page_up
  | View_page_down
  | View_center

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
  | Plugins
  | Search
  | Macros
  | Locations
  | Jumps
  | Jobs
  | Buffers
  | Project
  | Project_search
  | Language

type t
type outcome = Continue of t | Exit of t

val create :
  model:model ->
  ?language:string ->
  ?file_path:string ->
  ?contents:string ->
  ?trace:Zenbu_model_api.Trace.t ->
  ?profiler:Zenbu_model_api.Profiler.t ->
  ?presentation:Zenbu_view.Presentation.t ->
  ?theme:Zenbu_view.Theme.t ->
  ?system_clipboard:System_clipboard.t ->
  ?config:Zenbu_scripting.Scripting.config ->
  ?plugins:Zenbu_extension.Plugin_host.config ->
  ?language_registry:Zenbu_language.Language.Registry.t ->
  dimensions:Zenbu_view.Renderer.dimensions ->
  unit ->
  (t, Zenbu_kernel.Error.t) result

val context : t -> Zenbu_model_api.Editor_context.t
val status : t -> Zenbu_model_api.Model_status.t
val model : t -> model
val filename : t -> string
val dirty : t -> bool
val viewport : t -> Zenbu_view.Viewport.t
val pane_count : t -> int
val focused_pane : t -> int
val buffer_count : t -> int
val focused_buffer : t -> int
val handle_input : t -> Zenbu_model_api.Input_event.t -> t

(* Apply a validated terminal pointer event through host semantic effects. *)
val handle_pointer : t -> Zenbu_model_api.Input_event.t -> t
val handle_host : t -> host_command -> outcome
val host_command_descriptors : unit -> Zenbu_model_api.Command_descriptor.t list
val host_binding_lines : t -> string list
val reload_config : t -> t
val resize : t -> columns:int -> rows:int -> t
val save_layout : t -> path:string -> (unit, Zenbu_kernel.Error.t) result
val restore_layout : t -> path:string -> (t, Zenbu_kernel.Error.t) result
val set_project_root : t -> path:string -> (t, Zenbu_kernel.Error.t) result
val project_root : t -> string option
val search_project : t -> query:string -> t
val render : t -> t * Zenbu_view.Frame.t
val contents : t -> string
val file_path : t -> string option
val dimensions : t -> Zenbu_view.Renderer.dimensions
val configuration_error : t -> Zenbu_kernel.Error.t option
val plugin_load_errors : t -> Zenbu_kernel.Error.t list
val notice : t -> string -> t
val inspect : t -> inspection -> string list
val toggle_inspector : t -> t
val inspector_open : t -> bool
val set_presentation : t -> profile:string -> t
val theme : t -> Zenbu_view.Theme.t
val set_theme : t -> theme:string -> t
val switch_buffer : t -> buffer_id:int -> t
val rename_buffer : t -> name:string -> t
val close_buffer : ?force:bool -> t -> t
val poll_language : t -> t
val poll_background : t -> t
val language_wakeup_fd : t -> Unix.file_descr option
val language_wakeup_fds : t -> Unix.file_descr list
val background_job_wakeup_fd : t -> Unix.file_descr option
val cancel_background_job : t -> job_id:int -> t
val open_background_job_output : t -> job_id:int -> t
val wakeup_fds : t -> Unix.file_descr list
val close : t -> unit
