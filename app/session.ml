open Zenbu_kernel
open Zenbu_model_api
open Zenbu_syntax
open Zenbu_proof_models
open Zenbu_structural_model
module Scripting = Zenbu_scripting.Scripting
module Plugins = Zenbu_extension.Plugin_host
module Language = Zenbu_language.Language
module Language_commands = Zenbu_language.Commands
module Lsp = Zenbu_lsp.Client
module Layout = Zenbu_view.Layout
module Vim_runtime = Model_runtime.Make (Vim_model)
module Selection_runtime = Model_runtime.Make (Selection_model)
module Direct_runtime = Model_runtime.Make (Direct_model)
module Structural_runtime = Model_runtime.Make (Structural_model)
module Script_runtime = Model_runtime.Make (Script_model)

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
  | Open_command_line
  | Switch_model
  | Enable_binding_layer
  | Disable_binding_layer
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
  | File_watches
  | Language

type search = {
  kind : search_kind;
  query : string;
  matches : Zenbu_view.Renderer.search_range list;
  current : int option;
}

and search_kind = Literal | Regexp

type query_replace = {
  kind : search_kind;
  query : string;
  replacement : string;
  document_version : int;
  pending : Zenbu_view.Renderer.search_range list;
  replaced : int;
  skipped : int;
}

type palette_action =
  | Invoke_command of Command_id.t
  | Invoke_host_command of host_command

type palette_item = {
  id : string;
  title : string;
  description : string option;
  provider : Provider.t;
  action : palette_action;
  descriptor : Command_descriptor.t;
}

type command_prompt_action =
  | Palette_item of palette_item
  | Bound_command of Scripting.binding

type presentation_cache = {
  contents : string;
  source_lines : Zenbu_view.Display.source_line list;
  syntax_spans : Zenbu_view.Renderer.syntax_span list;
}

type interaction =
  | Idle
  | Search_prompt of {
      kind : search_kind;
      query : string;
      origin : Editor_context.selection_set;
      direction : Model_effect.search_direction;
    }
  | Palette of { query : string; selected : int }
  | Command_line of string
  | Command_prompt of {
      action : command_prompt_action;
      descriptor : Command_descriptor.t;
      remaining : Command_descriptor.parameter list;
      arguments_rev : Command_argument.t list;
      text : string;
    }
  | Save_as_prompt of string
  | Open_buffer_prompt of string
  | File_picker of { query : string; selected : int }
  | Project_search_view of {
      snapshot : Project_search.snapshot;
      selected : int;
    }
  | Query_replace of query_replace
  | Model_picker of int
  | Help_view
  | Hover_view of Language.hover
  | Completion_view of {
      items : Language.completion list;
      selected : int;
      query : string;
    }
  | Rename_prompt of string

type mouse_drag =
  | Selection_drag of { pane : int; anchor_offset : int }
  | Divider_drag of Layout.divider

type binding_resolution =
  | No_binding
  | Binding_prefix of Input_event.t list
  | Binding_resolved of Scripting.binding * string option
  | Binding_cancelled
  | Binding_rejected of Input_event.t list

type active =
  | Vim_runtime of Vim_runtime.t
  | Selection_runtime of Selection_runtime.t
  | Direct_runtime of Direct_runtime.t
  | Structural_runtime of Structural_runtime.t
  | Script_runtime of Script_runtime.t

type macro_recording = { register : string; inputs_rev : Input_event.t list }

type location = {
  name : string;
  buffer_id : int;
  document_version : int;
  selections : (int * int) list;
  primary : int;
  stale : bool;
}

type view_position = {
  buffer_id : int;
  document_version : int;
  selections : (int * int) list;
  primary : int;
  stale : bool;
}

type buffer = {
  id : int;
  active : active;
  file_path : string option;
  buffer_name : string option;
  language_override : string option;
  saved_version : int;
  saved_contents : string;
  saved_snapshot : File_io.snapshot option;
  language_client : Lsp.t option;
  diagnostics : Language.diagnostic list;
  presentation_cache : presentation_cache option;
  search : search option;
  active_modes : string list;
  active_binding_layers : Scripting.binding_layer list;
}

type t = {
  active : active;
  base_commands : Command_registry.t;
  base_semantics : Semantic_descriptor.t list;
  config : Scripting.config;
  generation : Scripting.t option;
  plugins : Plugins.t;
  plugins_config : Plugins.config;
  next_generation_id : int;
  last_reload_error : Error.t option;
  delivering_events : Scripting.event list;
  file_path : string option;
  buffer_name : string option;
  language_override : string option;
  saved_version : int;
  saved_contents : string;
  saved_snapshot : File_io.snapshot option;
  file_watcher : File_watcher.t;
  file_watch_notices : string list;
  project_root : Project_root.t option;
  project_search : Project_search.snapshot option;
  layout : Layout.t;
  focused_pane : int;
  pane_viewports : (int * Zenbu_view.Viewport.t) list;
  pane_view_positions : ((int * int) * view_position) list;
  next_pane_id : int;
  dimensions : Zenbu_view.Renderer.dimensions;
  presentation : Zenbu_view.Presentation.t;
  theme : Zenbu_view.Theme.t;
  message : string option;
  quit_armed : bool;
  inspector : string list option;
  presentation_cache : presentation_cache option;
  search : search option;
  interaction : interaction;
  language_client : Lsp.t option;
  diagnostics : Language.diagnostic list;
  language_registry : Language.Registry.t;
  current_buffer_id : int;
  inactive_buffers : buffer list;
  next_buffer_id : int;
  pane_buffers : (int * int) list;
  mouse_drag : mouse_drag option;
  pending_binding : Input_event.t list;
  active_modes : string list;
  active_binding_layers : Scripting.binding_layer list;
  macro_recording : macro_recording option;
  macros : (string * Input_event.t list) list;
  last_macro_register : string option;
  macro_replay_pending : (string * int) option;
  macro_replaying : bool;
  macro_control : bool;
  kill_ring : Clipboard.entry list;
  system_clipboard : System_clipboard.t;
  jobs : Background_job.t option;
  locations : location list;
  backward_jumps : location list;
  forward_jumps : location list;
}

type outcome = Continue of t | Exit of t

let static = function
  | Ok value -> value
  | Error error -> failwith (Error.to_string error)

let maximum_macro_events = 1024
let maximum_macro_registers = 64
let maximum_macro_register_bytes = 64
let maximum_macro_replay_count = 1024
let maximum_macro_replay_events = 65_536
let default_macro_register = "@"
let maximum_locations = 64
let maximum_location_name_bytes = 64
let maximum_jump_entries = 100
let maximum_file_watch_notices = 128
let maximum_active_binding_layers = 32

let provider_identity provider =
  match Provider.plugin_id provider with
  | Some id -> "plugin:" ^ id
  | None -> (
      match Provider.source provider with
      | Some source -> "source:" ^ source
      | None -> "provider:" ^ Provider.id provider)

let binding_layer_catalog generation plugins =
  (match generation with
    | None -> []
    | Some generation -> Scripting.binding_layers generation)
  @ Plugins.binding_layers plugins

let binding_catalog generation plugins =
  (match generation with
    | None -> []
    | Some generation -> Scripting.bindings generation)
  @ Plugins.bindings plugins

let same_binding_layer left right =
  String.equal
    (Scripting.binding_layer_id left)
    (Scripting.binding_layer_id right)
  && String.equal
       (provider_identity (Scripting.binding_layer_provider left))
       (provider_identity (Scripting.binding_layer_provider right))

let binding_belongs_to_layer layer binding =
  match Scripting.binding_layer binding with
  | None -> false
  | Some id ->
      String.equal id (Scripting.binding_layer_id layer)
      && String.equal
           (Provider.id (Scripting.binding_provider binding))
           (Provider.id (Scripting.binding_layer_provider layer))

let rec binding_pattern_sequence_is_prefix prefix sequence =
  match (prefix, sequence) with
  | [], _ -> true
  | _, [] -> false
  | pattern :: prefix_rest, candidate :: sequence_rest ->
      Input_event.binding_patterns_overlap pattern candidate
      && binding_pattern_sequence_is_prefix prefix_rest sequence_rest

let bindings_overlap left right =
  Scripting.binding_scope left = Scripting.binding_scope right
  && (binding_pattern_sequence_is_prefix
        (Scripting.binding_inputs left)
        (Scripting.binding_inputs right)
     || binding_pattern_sequence_is_prefix
          (Scripting.binding_inputs right)
          (Scripting.binding_inputs left))

let binding_layers_conflict bindings layers =
  let rec check = function
    | [] -> false
    | layer :: rest ->
        List.exists
          (fun other ->
            Scripting.binding_layer_priority layer
            = Scripting.binding_layer_priority other
            && List.exists
                 (fun left ->
                   binding_belongs_to_layer layer left
                   && List.exists
                        (fun right ->
                          binding_belongs_to_layer other right
                          && bindings_overlap left right)
                        bindings)
                 bindings)
          rest
        || check rest
  in
  check layers

let revalidate_active_binding_layers ~catalog ~bindings active =
  let rec keep retained dropped = function
    | [] -> (List.rev retained, List.rev dropped)
    | layer :: rest -> (
        match List.find_opt (same_binding_layer layer) catalog with
        | None ->
            keep retained (Scripting.binding_layer_id layer :: dropped) rest
        | Some replacement ->
            let candidate = List.rev (replacement :: retained) in
            if binding_layers_conflict bindings candidate then
              keep retained (Scripting.binding_layer_id layer :: dropped) rest
            else keep (replacement :: retained) dropped rest)
  in
  keep [] [] active

let synchronize_macro_context session =
  let register =
    Option.map
      (fun (recording : macro_recording) -> recording.register)
      session.macro_recording
  in
  let active =
    match session.active with
    | Vim_runtime runtime ->
        Vim_runtime.with_macro_recording_register runtime register
        |> fun runtime -> Vim_runtime runtime
    | Selection_runtime runtime ->
        Selection_runtime.with_macro_recording_register runtime register
        |> fun runtime -> Selection_runtime runtime
    | Direct_runtime runtime ->
        Direct_runtime.with_macro_recording_register runtime register
        |> fun runtime -> Direct_runtime runtime
    | Structural_runtime runtime ->
        Structural_runtime.with_macro_recording_register runtime register
        |> fun runtime -> Structural_runtime runtime
    | Script_runtime runtime ->
        Script_runtime.with_macro_recording_register runtime register
        |> fun runtime -> Script_runtime runtime
  in
  { session with active }

let active_with_kill_ring active kill_ring =
  match active with
  | Vim_runtime runtime ->
      Vim_runtime.with_kill_ring runtime kill_ring |> fun runtime ->
      Vim_runtime runtime
  | Selection_runtime runtime ->
      Selection_runtime.with_kill_ring runtime kill_ring |> fun runtime ->
      Selection_runtime runtime
  | Direct_runtime runtime ->
      Direct_runtime.with_kill_ring runtime kill_ring |> fun runtime ->
      Direct_runtime runtime
  | Structural_runtime runtime ->
      Structural_runtime.with_kill_ring runtime kill_ring |> fun runtime ->
      Structural_runtime runtime
  | Script_runtime runtime ->
      Script_runtime.with_kill_ring runtime kill_ring |> fun runtime ->
      Script_runtime runtime

let kill_ring_of_active = function
  | Vim_runtime runtime -> Vim_runtime.kill_ring runtime
  | Selection_runtime runtime -> Selection_runtime.kill_ring runtime
  | Direct_runtime runtime -> Direct_runtime.kill_ring runtime
  | Structural_runtime runtime -> Structural_runtime.kill_ring runtime
  | Script_runtime runtime -> Script_runtime.kill_ring runtime

let synchronize_kill_ring_from_active session =
  let kill_ring = kill_ring_of_active session.active in
  {
    session with
    kill_ring;
    active = active_with_kill_ring session.active kill_ring;
    inactive_buffers =
      List.map
        (fun (buffer : buffer) ->
          { buffer with active = active_with_kill_ring buffer.active kill_ring })
        session.inactive_buffers;
  }

let validate_macro_register register =
  if String.length register = 0 then
    Error (Error.Invalid_command_arguments "macro register must not be empty")
  else if String.length register > maximum_macro_register_bytes then
    Error
      (Error.Invalid_command_arguments
         "macro register exceeds the configured byte limit")
  else
    Text_buffer.of_utf8 register
    |> Result.map_error (fun _ ->
        Error.Invalid_command_arguments "macro register must be valid UTF-8")
    |> Result.map (fun _ -> register)

let find_macro session register = List.assoc_opt register session.macros

let store_macro session register inputs =
  let existing = List.mem_assoc register session.macros in
  if (not existing) && List.length session.macros >= maximum_macro_registers
  then
    Error
      (Error.Invalid_command_arguments
         "macro register store is full; replace an existing register")
  else Ok ((register, inputs) :: List.remove_assoc register session.macros)

let validate_location_name name =
  if String.length name = 0 then
    Error (Error.Invalid_command_arguments "location name must not be empty")
  else if String.length name > maximum_location_name_bytes then
    Error
      (Error.Invalid_command_arguments
         "location name exceeds the configured byte limit")
  else
    Text_buffer.of_utf8 name
    |> Result.map_error (fun _ ->
        Error.Invalid_command_arguments "location name must be valid UTF-8")
    |> Result.map (fun _ -> name)

let toggle_macro_recording ?(register = default_macro_register) session =
  match (validate_macro_register register, session.macro_recording) with
  | Error error, _ ->
      {
        session with
        macro_control = true;
        message = Some (Error.to_string error);
        quit_armed = false;
      }
  | Ok register, None ->
      synchronize_macro_context
        {
          session with
          macro_recording = Some { register; inputs_rev = [] };
          macro_control = true;
          message = Some ("macro recording started: " ^ register);
          quit_armed = false;
        }
  | Ok register, Some recording
    when not (String.equal register recording.register) ->
      {
        session with
        macro_control = true;
        message =
          Some
            ("macro recording is active for " ^ recording.register
           ^ "; stop it before selecting " ^ register);
        quit_armed = false;
      }
  | Ok _, Some { inputs_rev = []; _ } ->
      synchronize_macro_context
        {
          session with
          macro_recording = None;
          macro_control = true;
          message = Some "macro recording discarded: no keyboard input";
          quit_armed = false;
        }
  | Ok register, Some { inputs_rev; _ } -> (
      match store_macro session register (List.rev inputs_rev) with
      | Error error ->
          {
            session with
            macro_control = true;
            message = Some (Error.to_string error);
            quit_armed = false;
          }
      | Ok macros ->
          synchronize_macro_context
            {
              session with
              macro_recording = None;
              macros;
              last_macro_register = Some register;
              macro_control = true;
              message =
                Some
                  (Printf.sprintf "macro recorded to %s: %d keyboard inputs"
                     register (List.length inputs_rev));
              quit_armed = false;
            })

let validate_macro_replay_count count =
  if count <= 0 then
    Error
      (Error.Invalid_command_arguments "macro replay count must be positive")
  else if count > maximum_macro_replay_count then
    Error
      (Error.Invalid_command_arguments
         "macro replay count exceeds the configured limit")
  else Ok count

let request_macro_replay ?(register = default_macro_register) ?(count = 1)
    session =
  match
    ( validate_macro_register register,
      validate_macro_replay_count count,
      session.macro_recording,
      session.macro_replaying,
      find_macro session register )
  with
  | Error error, _, _, _, _ | _, Error error, _, _, _ ->
      {
        session with
        macro_control = true;
        message = Some (Error.to_string error);
        quit_armed = false;
      }
  | Ok _, Ok _, Some _, _, _ ->
      {
        session with
        macro_control = true;
        message = Some "macro replay rejected: finish recording first";
        quit_armed = false;
      }
  | Ok _, Ok _, None, true, _ ->
      {
        session with
        macro_control = true;
        message = Some "macro replay rejected: recursive replay is disabled";
        quit_armed = false;
      }
  | Ok register, Ok _, None, false, None ->
      {
        session with
        macro_control = true;
        message =
          Some ("macro replay rejected: register " ^ register ^ " is empty");
        quit_armed = false;
      }
  | Ok register, Ok count, None, false, Some inputs ->
      if List.length inputs > maximum_macro_replay_events / count then
        {
          session with
          macro_control = true;
          message =
            Some
              (Printf.sprintf
                 "macro replay rejected: %d iterations of register %s exceed \
                  the %d-event limit"
                 count register maximum_macro_replay_events);
          quit_armed = false;
        }
      else
        {
          session with
          macro_replay_pending = Some (register, count);
          macro_control = true;
          quit_armed = false;
        }

let record_macro_input session input =
  match session.macro_recording with
  | None -> session
  | Some recording -> (
      match input with
      | Input_event.Mouse _ -> session
      | Input_event.Key_press _ | Input_event.Text_input _ ->
          if List.length recording.inputs_rev >= maximum_macro_events then
            synchronize_macro_context
              {
                session with
                macro_recording = None;
                message =
                  Some
                    (Printf.sprintf
                       "macro recording stopped: register %s reached maximum \
                        of %d keyboard inputs"
                       recording.register maximum_macro_events);
              }
          else
            {
              session with
              macro_recording =
                Some
                  { recording with inputs_rev = input :: recording.inputs_rev };
            })

type host_command_entry = {
  command : host_command;
  descriptor : Command_descriptor.t;
  palette : bool;
}

let host_provider =
  Provider.create ~id:"zenbu.app" ~kind:Provider.Application |> static

let host_descriptor ?(parameters = []) id title description =
  Command_descriptor.create
    ~id:(Command_id.of_string id |> static)
    ~title ~description ~category:"host" ~parameters ~provider:host_provider ()
  |> static

let text_parameter ~name ~description:parameter_description ~required =
  Command_descriptor.
    { name; description = parameter_description; required; kind = Text }

let language_descriptor id title description =
  Command_descriptor.create
    ~id:(Command_id.of_string id |> static)
    ~title ~description ~category:"language"
    ~provider:Language_commands.provider ()
  |> static

let host_command_entries =
  lazy
    [
      {
        command = Save;
        descriptor =
          host_descriptor "editor.save" "Save buffer"
            "Save to the active path, or open the save-as prompt for an \
             unnamed buffer.";
        palette = true;
      };
      {
        command = Save_as;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"path"
                  ~description:"Destination path to replace atomically."
                  ~required:true;
              ]
            "editor.save-as" "Save buffer as"
            "Write the active buffer to a destination path atomically.";
        palette = true;
      };
      {
        command = Save_layout;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"path"
                  ~description:
                    "Destination JSON file for clean, file-backed workspace \
                     state."
                  ~required:true;
              ]
            "workspace.layout.save" "Save workspace layout"
            "Write the validated local buffer, split, viewport, and selection \
             layout.";
        palette = true;
      };
      {
        command = Restore_layout;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"path"
                  ~description:
                    "Validated workspace-layout JSON file to restore."
                  ~required:true;
              ]
            "workspace.layout.restore" "Restore workspace layout"
            "Replace this session only after validating every referenced local \
             file and view state.";
        palette = true;
      };
      {
        command = Set_project_root;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"path"
                  ~description:
                    "Readable directory to canonicalize as the host-owned \
                     project root."
                  ~required:true;
              ]
            "workspace.project.root.set" "Set project root"
            "Validate and select the directory used by the local file picker.";
        palette = true;
      };
      {
        command = Open_file_picker;
        descriptor =
          host_descriptor "workspace.file-picker" "Open project file picker"
            "Filter validated readable text files below the selected project \
             root.";
        palette = true;
      };
      {
        command = Search_project;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"query"
                  ~description:
                    "Literal UTF-8 text to search under the selected project \
                     root."
                  ~required:true;
              ]
            "workspace.project.search" "Search project text"
            "Run a bounded literal search below the selected project root; \
             results are host-owned and open through the normal buffer path.";
        palette = true;
      };
      {
        command = Quit;
        descriptor =
          host_descriptor "editor.quit" "Quit Zenbu"
            "Quit, asking for a second confirmation when the buffer is dirty.";
        palette = false;
      };
      {
        command = Force_quit;
        descriptor =
          host_descriptor "editor.force-quit" "Force quit Zenbu"
            "Quit without saving the active buffer.";
        palette = false;
      };
      {
        command = Reload_config;
        descriptor =
          host_descriptor "config.reload" "Reload configuration and plugins"
            "Stage Lua configuration and local plugins, retaining the previous \
             generation on failure.";
        palette = true;
      };
      {
        command = Start_search;
        descriptor =
          host_descriptor "search.start" "Search text"
            "Open a literal UTF-8 search prompt shared by every editing model.";
        palette = true;
      };
      {
        command = Start_regexp_search;
        descriptor =
          host_descriptor "search.regexp" "Search regexp"
            "Open an incremental, UTF-8-safe Str regexp search prompt shared \
             by every editing model.";
        palette = true;
      };
      {
        command = Replace_all_literal;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"query"
                  ~description:"Literal UTF-8 text to replace." ~required:true;
                text_parameter ~name:"replacement"
                  ~description:"Literal UTF-8 replacement text." ~required:true;
              ]
            "search.replace.literal" "Replace all literal matches"
            "Replace every non-overlapping literal match in one checked \
             transaction.";
        palette = true;
      };
      {
        command = Replace_all_regexp;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"query"
                  ~description:"UTF-8-safe Str regexp to replace."
                  ~required:true;
                text_parameter ~name:"replacement"
                  ~description:"Literal UTF-8 replacement text." ~required:true;
              ]
            "search.replace.regexp" "Replace all regexp matches"
            "Replace every non-empty UTF-8-safe Str regexp match in one \
             checked transaction; replacement text is literal.";
        palette = true;
      };
      {
        command = Start_query_replace_literal;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"query"
                  ~description:"Literal UTF-8 text to review and replace."
                  ~required:true;
                text_parameter ~name:"replacement"
                  ~description:"Literal UTF-8 replacement text." ~required:true;
              ]
            "search.query-replace.literal" "Query-replace literal matches"
            "Review literal matches one at a time: skip, replace, replace the \
             rest, or quit.";
        palette = true;
      };
      {
        command = Start_query_replace_regexp;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"query"
                  ~description:"UTF-8-safe Str regexp to review and replace."
                  ~required:true;
                text_parameter ~name:"replacement"
                  ~description:"Literal UTF-8 replacement text." ~required:true;
              ]
            "search.query-replace.regexp" "Query-replace regexp matches"
            "Review non-empty UTF-8-safe Str regexp matches one at a time; \
             replacement text is literal.";
        palette = true;
      };
      {
        command = Search_next;
        descriptor =
          host_descriptor "search.next" "Next search match"
            "Select the next active-search match, wrapping at the end.";
        palette = true;
      };
      {
        command = Search_previous;
        descriptor =
          host_descriptor "search.previous" "Previous search match"
            "Select the previous active-search match, wrapping at the \
             beginning.";
        palette = true;
      };
      {
        command = Toggle_macro_recording;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"register"
                  ~description:
                    "Optional named register; @ is used when this is blank."
                  ~required:false;
              ]
            "editor.macro.record" "Start or stop keyboard macro"
            "Record ordinary keyboard input while executing it; invoking the \
             command again stores it in the same named register.";
        palette = true;
      };
      {
        command = Replay_macro;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"register"
                  ~description:
                    "Optional named register to replay; @ is used when this is \
                     blank."
                  ~required:false;
                text_parameter ~name:"count"
                  ~description:
                    "Optional positive repeat count, bounded by the macro \
                     replay limit."
                  ~required:false;
              ]
            "editor.macro.replay" "Replay keyboard macro"
            "Replay a named keyboard macro through the normal input and \
             transaction pipeline.";
        palette = true;
      };
      {
        command = Kill_ring_cut;
        descriptor =
          host_descriptor "editor.kill-ring.cut" "Cut selection"
            "Delete non-empty selections and prepend their text to the bounded \
             shared kill history.";
        palette = true;
      };
      {
        command = Kill_ring_yank;
        descriptor =
          host_descriptor "editor.kill-ring.yank" "Yank latest kill"
            "Insert the newest shared kill-history entry at the active \
             selections.";
        palette = true;
      };
      {
        command = System_clipboard_copy;
        descriptor =
          host_descriptor "editor.clipboard.copy"
            "Copy selection to system clipboard"
            "Copy non-empty selections through the configured bounded system \
             clipboard provider.";
        palette = true;
      };
      {
        command = System_clipboard_paste;
        descriptor =
          host_descriptor "editor.clipboard.paste" "Paste system clipboard"
            "Replace active selections with UTF-8 text read through the \
             configured bounded system clipboard provider.";
        palette = true;
      };
      {
        command = Set_location;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"name"
                  ~description:
                    "Named location to capture from the current selection set."
                  ~required:true;
              ]
            "editor.location.set" "Set named location"
            "Capture the active buffer and ordered selections as a rebased \
             session location.";
        palette = true;
      };
      {
        command = Jump_location;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"name"
                  ~description:"Named location to activate and restore."
                  ~required:true;
              ]
            "editor.location.jump" "Jump to named location"
            "Activate the recorded local buffer and restore its rebased \
             selection set.";
        palette = true;
      };
      {
        command = Push_jump;
        descriptor =
          host_descriptor "editor.jump.push" "Add jump history entry"
            "Record the current buffer and ordered selection set as a \
             jump-history entry.";
        palette = true;
      };
      {
        command = Jump_backward;
        descriptor =
          host_descriptor "editor.jump.backward" "Jump backward"
            "Restore the previous rebased jump-history entry.";
        palette = true;
      };
      {
        command = Jump_forward;
        descriptor =
          host_descriptor "editor.jump.forward" "Jump forward"
            "Restore the next rebased jump-history entry.";
        palette = true;
      };
      {
        command = Open_palette;
        descriptor =
          host_descriptor "editor.command-palette" "Open command palette"
            "Discover commands contributed by Zenbu, models, scripts, and \
             plugins.";
        palette = false;
      };
      {
        command = Open_command_line;
        descriptor =
          host_descriptor "editor.command-line" "Run declared command line"
            "Parse one exact registered command ID and its declared typed \
             arguments without shell or product-command compatibility.";
        palette = true;
      };
      {
        command = Split_vertical;
        descriptor =
          host_descriptor "workspace.split.vertical" "Split view vertically"
            "Create a side-by-side viewport of the active buffer.";
        palette = true;
      };
      {
        command = Split_horizontal;
        descriptor =
          host_descriptor "workspace.split.horizontal" "Split view horizontally"
            "Create a stacked viewport of the active buffer.";
        palette = true;
      };
      {
        command = Focus_next_pane;
        descriptor =
          host_descriptor "workspace.pane.next" "Focus next view"
            "Move input focus to the next pane in layout order.";
        palette = true;
      };
      {
        command = Close_pane;
        descriptor =
          host_descriptor "workspace.pane.close" "Close current view"
            "Close the focused pane while retaining its buffer.";
        palette = true;
      };
      {
        command = Only_pane;
        descriptor =
          host_descriptor "workspace.pane.only" "Keep only current view"
            "Close every other pane while retaining the focused view.";
        palette = true;
      };
      {
        command = Grow_pane_width;
        descriptor =
          host_descriptor "workspace.pane.grow-width" "Grow current view width"
            "Move the nearest vertical divider one cell toward the other view.";
        palette = true;
      };
      {
        command = Shrink_pane_width;
        descriptor =
          host_descriptor "workspace.pane.shrink-width"
            "Shrink current view width"
            "Move the nearest vertical divider one cell toward the current \
             view.";
        palette = true;
      };
      {
        command = Grow_pane_height;
        descriptor =
          host_descriptor "workspace.pane.grow-height"
            "Grow current view height"
            "Move the nearest horizontal divider one cell toward the other \
             view.";
        palette = true;
      };
      {
        command = Shrink_pane_height;
        descriptor =
          host_descriptor "workspace.pane.shrink-height"
            "Shrink current view height"
            "Move the nearest horizontal divider one cell toward the current \
             view.";
        palette = true;
      };
      {
        command = Balance_panes;
        descriptor =
          host_descriptor "workspace.panes.balance" "Balance split views"
            "Restore equal proportions for every split in the current layout.";
        palette = true;
      };
      {
        command = New_buffer;
        descriptor =
          host_descriptor "workspace.buffer.new" "Create buffer"
            "Create an unnamed buffer in the focused view.";
        palette = true;
      };
      {
        command = Open_buffer;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"path"
                  ~description:"Path of the file to load into the focused view."
                  ~required:true;
              ]
            "workspace.buffer.open" "Open file in buffer"
            "Load a file path into the focused view.";
        palette = true;
      };
      {
        command = List_buffers;
        descriptor =
          host_descriptor "workspace.buffers" "List buffers"
            "Inspect the names, identities, paths, and dirty state of open \
             buffers.";
        palette = true;
      };
      {
        command = Switch_buffer;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"buffer-id"
                  ~description:
                    "Nonnegative identifier shown in workspace.buffers."
                  ~required:true;
              ]
            "workspace.buffer.switch" "Switch buffer"
            "Show the named buffer in the focused view.";
        palette = true;
      };
      {
        command = Rename_buffer;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"name"
                  ~description:
                    "Short UTF-8 display name for the focused buffer."
                  ~required:true;
              ]
            "workspace.buffer.rename" "Rename buffer"
            "Set the focused buffer's display name without changing its file \
             path.";
        palette = true;
      };
      {
        command = Close_buffer;
        descriptor =
          host_descriptor "workspace.buffer.close" "Close buffer"
            "Close the focused clean buffer and retarget every view that shows \
             it.";
        palette = true;
      };
      {
        command = Force_close_buffer;
        descriptor =
          host_descriptor "workspace.buffer.force-close" "Force-close buffer"
            "Discard the focused buffer's unsaved changes and retarget every \
             view that shows it.";
        palette = true;
      };
      {
        command = Next_buffer;
        descriptor =
          host_descriptor "workspace.buffer.next" "Next buffer"
            "Show the next open buffer in the focused view.";
        palette = true;
      };
      {
        command = Previous_buffer;
        descriptor =
          host_descriptor "workspace.buffer.previous" "Previous buffer"
            "Show the previous open buffer in the focused view.";
        palette = true;
      };
      {
        command = View_scroll_up;
        descriptor =
          host_descriptor "view.scroll.up" "Scroll view up"
            "Move the focused viewport up one source line without changing \
             selections.";
        palette = true;
      };
      {
        command = View_scroll_down;
        descriptor =
          host_descriptor "view.scroll.down" "Scroll view down"
            "Move the focused viewport down one source line without changing \
             selections.";
        palette = true;
      };
      {
        command = View_page_up;
        descriptor =
          host_descriptor "view.page.up" "Page view up"
            "Move the focused viewport up one visible page without changing \
             selections.";
        palette = true;
      };
      {
        command = View_page_down;
        descriptor =
          host_descriptor "view.page.down" "Page view down"
            "Move the focused viewport down one visible page without changing \
             selections.";
        palette = true;
      };
      {
        command = View_center;
        descriptor =
          host_descriptor "view.center" "Center view"
            "Center the focused viewport on the primary selection without \
             changing it.";
        palette = true;
      };
      {
        command = Switch_model;
        descriptor =
          host_descriptor "editor.model.switch" "Switch editing model"
            "Choose Vim-style, selection-first, or structural editing without \
             replacing semantic state.";
        palette = true;
      };
      {
        command = Enable_binding_layer;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"layer"
                  ~description:
                    "Declared data-only binding layer to enable in the current \
                     buffer."
                  ~required:true;
              ]
            "keymap.layer.enable" "Enable binding layer"
            "Atomically enable a declared binding layer for the current buffer.";
        palette = true;
      };
      {
        command = Disable_binding_layer;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"layer"
                  ~description:
                    "Declared data-only binding layer to disable in the \
                     current buffer."
                  ~required:true;
              ]
            "keymap.layer.disable" "Disable binding layer"
            "Disable a binding layer in the current buffer without changing \
             its document.";
        palette = true;
      };
      {
        command = Help;
        descriptor =
          host_descriptor "editor.help" "Show help"
            "Show host controls and current model input rules from runtime \
             metadata.";
        palette = true;
      };
      {
        command = Switch_presentation;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"profile"
                  ~description:
                    "Built-in default|numbered|relative|minimal|bare|buffered \
                     or a validated presentation TOML file."
                  ~required:true;
              ]
            "view.presentation.switch" "Switch terminal presentation"
            "Change host-owned line-number, status-row, and buffer-line policy \
             without changing semantic editor state.";
        palette = true;
      };
      {
        command = Switch_theme;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"theme"
                  ~description:
                    "Built-in default|dark|light or a validated theme TOML \
                     file."
                  ~required:true;
              ]
            "view.theme.switch" "Switch terminal theme"
            "Change host-owned terminal colours without changing semantic \
             editor state.";
        palette = true;
      };
      {
        command = Background_jobs;
        descriptor =
          host_descriptor "process.jobs" "Show background jobs"
            "Inspect bounded background processes started by trusted editing \
             models.";
        palette = true;
      };
      {
        command = Cancel_background_job;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"job-id"
                  ~description:"Positive identifier shown in process.jobs."
                  ~required:true;
              ]
            "process.job.cancel" "Cancel background job"
            "Terminate one running trusted-local background program.";
        palette = true;
      };
      {
        command = Open_background_job_output;
        descriptor =
          host_descriptor
            ~parameters:
              [
                text_parameter ~name:"job-id"
                  ~description:"Positive identifier shown in process.jobs."
                  ~required:true;
              ]
            "process.job.open-output" "Open background-job output"
            "Open the bounded final job report in a named normal buffer.";
        palette = true;
      };
      {
        command = Language_status;
        descriptor =
          language_descriptor "language.status" "Show language-service status"
            "Inspect the active language server without exposing protocol \
             objects.";
        palette = true;
      };
      {
        command = Language_restart;
        descriptor =
          language_descriptor "language.restart" "Restart language server"
            "Restart the active optional language server and resynchronize the \
             document.";
        palette = true;
      };
      {
        command = Language_hover;
        descriptor =
          language_descriptor "language.hover" "Show language hover"
            "Request bounded hover information at the primary caret.";
        palette = true;
      };
      {
        command = Language_definition;
        descriptor =
          language_descriptor "language.definition" "Go to definition"
            "Navigate to a same-document language definition when available.";
        palette = true;
      };
      {
        command = Language_complete;
        descriptor =
          language_descriptor "language.complete" "Request completion"
            "Request explicit language completion at the primary caret.";
        palette = true;
      };
      {
        command = Language_rename;
        descriptor =
          language_descriptor "language.rename" "Rename symbol"
            "Prompt for a new name and apply supported current-document edits \
             atomically.";
        palette = true;
      };
      {
        command = Language_diagnostic_next;
        descriptor =
          language_descriptor "language.diagnostic.next" "Next diagnostic"
            "Move the primary selection to the next current diagnostic.";
        palette = true;
      };
      {
        command = Language_diagnostic_previous;
        descriptor =
          language_descriptor "language.diagnostic.previous"
            "Previous diagnostic"
            "Move the primary selection to the previous current diagnostic.";
        palette = true;
      };
      {
        command = Language_diagnostic_describe_current;
        descriptor =
          language_descriptor "language.diagnostic.describe-current"
            "Describe current diagnostic"
            "Show the diagnostic under the primary caret.";
        palette = true;
      };
    ]

let host_command_descriptors () =
  Lazy.force host_command_entries |> List.map (fun entry -> entry.descriptor)

let host_binding_lines session =
  let direct =
    match session.active with Direct_runtime _ -> true | _ -> false
  in
  (if direct then [] else [ "host reserved: Ctrl-S -> editor.save (zenbu.app)" ])
  @ [
      "host reserved: Ctrl-Shift-S -> editor.save-as (zenbu.app)";
      "host reserved: Ctrl-Q -> editor.quit (zenbu.app; press again when dirty)";
      "host reserved: Alt-R / Ctrl-Alt-R -> config.reload (zenbu.app)";
      "host reserved: Alt-M -> editor.model.switch (zenbu.app)";
      "host reserved: Alt-H -> editor.help (zenbu.app)";
      "host reserved: Ctrl-O -> why inspector (zenbu.app)";
      "host reserved: Ctrl-Space -> language.complete (zenbu.language)";
    ]
  @
  if direct then []
  else
    [
      "host reserved: Ctrl-G -> search.next (zenbu.app)";
      "host reserved: Ctrl-Shift-G -> search.previous (zenbu.app)";
    ]
    @
    if direct then []
    else
      [
        "host reserved: Ctrl-F -> search.start (zenbu.app)";
        "host reserved: Ctrl-P -> editor.command-palette (zenbu.app)";
      ]

let command_prompt_message descriptor parameter =
  Printf.sprintf "command %s: enter %s (%s)"
    (Command_descriptor.id descriptor |> Command_id.to_string)
    parameter.Command_descriptor.name parameter.description

let begin_command_prompt session ~action ~descriptor =
  match Command_descriptor.parameters descriptor with
  | [] -> session
  | parameter :: remaining ->
      {
        session with
        interaction =
          Command_prompt
            {
              action;
              descriptor;
              remaining = parameter :: remaining;
              arguments_rev = [];
              text = "";
            };
        message = Some (command_prompt_message descriptor parameter);
        quit_armed = false;
        inspector = None;
      }

let command_argument_of_text parameter text =
  let value =
    match parameter.Command_descriptor.kind with
    | Command_descriptor.Text -> Ok (Command_argument.Text text)
    | Command_descriptor.Selector ->
        Model_intent.selector_of_string text
        |> Result.map (fun selector -> Command_argument.Selector selector)
    | Command_descriptor.Transformation ->
        Model_intent.transformation_of_string text
        |> Result.map (fun transformation ->
            Command_argument.Transformation transformation)
  in
  Result.bind value (fun value ->
      Command_argument.make ~name:parameter.name ~value)

let commands () =
  List.fold_left
    (fun registry command ->
      match registry with
      | Error _ -> registry
      | Ok registry -> Command_registry.register registry command)
    (Ok Command_registry.empty)
    ((Semantic_commands.apply_command :: Semantic_commands.selection_commands)
    @ Syntax_commands.commands ())

let base_semantics () =
  (Inspector.semantic_registry () |> Semantic_registry.descriptors)
  @ Language_commands.descriptors ()

let commands_with_generation base generation =
  match generation with
  | None -> Ok base
  | Some generation ->
      List.fold_left
        (fun registry command ->
          Result.bind registry (fun registry ->
              Command_registry.register registry command))
        (Ok base)
        (Scripting.commands generation)

let commands_with_plugins base plugins =
  List.fold_left
    (fun registry command ->
      Result.bind registry (fun registry ->
          Command_registry.register registry command))
    (Ok base) (Plugins.commands plugins)

let semantic_behaviors = function
  | None -> Semantic_behavior_registry.empty
  | Some generation -> Scripting.semantic_behaviors generation

let semantic_behaviors_with_plugins generation plugins =
  Semantic_behavior_registry.merge Language_commands.behaviors
    (semantic_behaviors generation)
  |> Result.get_ok
  |> fun values ->
  Semantic_behavior_registry.merge values (Plugins.semantic_behaviors plugins)
  |> Result.get_ok

let document ~contents =
  Document.create
    ~id:(static (Document_id.of_string "terminal-buffer"))
    ~contents ()

let syntax_service ?language file_path =
  match language with
  | Some id -> (
      match Syntax.Language.find id with
      | Some language -> Ok (Some (Syntax.Service.create language))
      | None ->
          Error (Error.Invalid_command_arguments ("unknown language: " ^ id)))
  | None -> (
      match Option.bind file_path Syntax.Language.detect_path with
      | Some language -> Ok (Some (Syntax.Service.create language))
      | None -> Ok None)

let trace_of_active = function
  | Vim_runtime runtime -> Vim_runtime.trace runtime
  | Selection_runtime runtime -> Selection_runtime.trace runtime
  | Direct_runtime runtime -> Direct_runtime.trace runtime
  | Structural_runtime runtime -> Structural_runtime.trace runtime
  | Script_runtime runtime -> Script_runtime.trace runtime

let profiler_of_active = function
  | Vim_runtime runtime -> Vim_runtime.profiler runtime
  | Selection_runtime runtime -> Selection_runtime.profiler runtime
  | Direct_runtime runtime -> Direct_runtime.profiler runtime
  | Structural_runtime runtime -> Structural_runtime.profiler runtime
  | Script_runtime runtime -> Script_runtime.profiler runtime

let active_with_syntax_service active syntax_service =
  match active with
  | Vim_runtime runtime ->
      Vim_runtime.with_syntax_service runtime ~syntax_service
      |> Result.map (fun runtime -> Vim_runtime runtime)
  | Selection_runtime runtime ->
      Selection_runtime.with_syntax_service runtime ~syntax_service
      |> Result.map (fun runtime -> Selection_runtime runtime)
  | Direct_runtime runtime ->
      Direct_runtime.with_syntax_service runtime ~syntax_service
      |> Result.map (fun runtime -> Direct_runtime runtime)
  | Structural_runtime runtime ->
      Structural_runtime.with_syntax_service runtime ~syntax_service
      |> Result.map (fun runtime -> Structural_runtime runtime)
  | Script_runtime runtime ->
      Script_model.configure_state (Script_runtime.model_state runtime);
      Script_runtime.with_syntax_service runtime ~syntax_service
      |> Result.map (fun runtime -> Script_runtime runtime)

let last_execution_of_active = function
  | Vim_runtime runtime -> Vim_runtime.last_execution runtime
  | Selection_runtime runtime -> Selection_runtime.last_execution runtime
  | Direct_runtime runtime -> Direct_runtime.last_execution runtime
  | Structural_runtime runtime -> Structural_runtime.last_execution runtime
  | Script_runtime runtime -> Script_runtime.last_execution runtime

let lifecycle trace ~execution_id ~phase ?generation ?provider ~outcome ?reason
    () =
  let provider =
    match generation with
    | Some generation -> Some (Scripting.provider generation)
    | None -> provider
  in
  Trace.emit_lazy trace (fun () ->
      Trace_event.Script_lifecycle
        {
          execution_id;
          phase;
          generation_id = Option.map Scripting.generation_id generation;
          provider;
          outcome;
          reason;
        })

let extension_lifecycle trace ~execution_id ~phase ?provider ~outcome ?reason ()
    =
  Trace.emit_lazy trace (fun () ->
      Trace_event.Extension_lifecycle
        { execution_id; phase; provider; outcome; reason })

let extension_callback trace ~execution_id ~kind ~provider ?semantic_id ?reason
    outcome =
  Trace.emit_lazy trace (fun () ->
      match Provider.kind provider with
      | Provider.Plugin ->
          Trace_event.Extension_callback
            { execution_id; kind; provider; semantic_id; outcome; reason }
      | Provider.Script ->
          Trace_event.Script_callback
            { execution_id; kind; provider; semantic_id; outcome; reason }
      | Provider.Builtin | Provider.Editing_model | Provider.Syntax
      | Provider.Application ->
          assert false)

let capability_denied trace ~execution_id ~provider = function
  | Error.Extension_error
      {
        code = Error.Capability_denied;
        operation = Some operation;
        required = Some required;
        granted;
        _;
      } ->
      Trace.emit_lazy trace (fun () ->
          Trace_event.Capability_denied
            { execution_id; provider; operation; required; granted })
  | _ -> ()

let trace_plugins trace ~execution_id ~phase plugins =
  let provider_for_view view =
    match Plugins.view_id view with
    | None -> None
    | Some id ->
        Plugins.providers plugins
        |> List.find_opt (fun provider ->
            Provider.plugin_id provider
            |> Option.map
                 (String.equal (Zenbu_extension.Plugin_id.to_string id))
            |> Option.value ~default:false)
  in
  Plugins.views plugins
  |> List.iter (fun view ->
      match (Plugins.view_state view, Plugins.view_error view) with
      | Plugins.Active, None ->
          extension_lifecycle trace ~execution_id ~phase
            ?provider:(provider_for_view view) ~outcome:"succeeded" ()
      | Plugins.Active, Some error ->
          extension_lifecycle trace ~execution_id ~phase
            ?provider:(provider_for_view view) ~outcome:"failed"
            ~reason:(Error.to_string error) ()
      | Plugins.Failed, Some error ->
          extension_lifecycle trace ~execution_id ~phase ~outcome:"failed"
            ~reason:(Error.to_string error) ()
      | Plugins.Failed, None -> ())

let wasm_profile_stage = function
  | "compile" -> Profiler.Extension_wasm_compile
  | "instantiate" -> Profiler.Extension_wasm_instantiate
  | "register" -> Profiler.Extension_wasm_register
  | "call" -> Profiler.Extension_wasm_call
  | stage -> invalid_arg ("unknown Wasm telemetry stage " ^ stage)

let trace_runtime_events trace profiler ~execution_id plugins =
  Plugins.drain_runtime_events plugins
  |> List.iter (fun (event : Plugins.runtime_event) ->
      Trace.emit_lazy trace (fun () ->
          Trace_event.Extension_runtime
            {
              execution_id;
              provider = event.provider;
              runtime = event.runtime;
              stage = event.stage;
              operation = event.operation;
              outcome = event.outcome;
              duration_seconds = event.duration_seconds;
              fuel_consumed = event.fuel_consumed;
              reason = event.reason;
            });
      Profiler.record profiler
        (wasm_profile_stage event.stage)
        ~seconds:event.duration_seconds)

let create ~model ?language ?file_path ?(contents = "") ?trace ?profiler
    ?(presentation = Zenbu_view.Presentation.default)
    ?(theme = Zenbu_view.Theme.default) ?system_clipboard
    ?(config = Scripting.Default) ?(plugins = Plugins.Disabled)
    ?(language_registry = Language.Registry.default ()) ?file_watcher
    ~dimensions () =
  let saved_snapshot =
    Option.bind file_path (fun path ->
        Result.to_option (File_io.snapshot ~path ~contents))
  in
  match document ~contents with
  | Error _ as error -> error
  | Ok document -> (
      match syntax_service ?language file_path with
      | Error _ as error -> error
      | Ok syntax_service -> (
          match commands () with
          | Error _ as error -> error
          | Ok base_commands ->
              let base_semantics = base_semantics () in
              let trace = Option.value trace ~default:(Trace.disabled ()) in
              let profiler =
                Option.value profiler ~default:(Profiler.disabled ())
              in
              let system_clipboard =
                Option.value system_clipboard
                  ~default:(System_clipboard.default ())
              in
              lifecycle trace ~execution_id:0 ~phase:"load" ~outcome:"started"
                ();
              let generation, config_error =
                match
                  Profiler.measure profiler Profiler.Script_load (fun () ->
                      Scripting.load ~generation_id:1 ~base_commands
                        ~base_semantics config)
                with
                | Ok generation -> (generation, None)
                | Error error -> (None, Some error)
              in
              let config_message =
                Option.map
                  (fun error ->
                    "configuration not loaded: " ^ Error.to_string error)
                  config_error
              in
              (match (generation, config_message) with
              | Some generation, None ->
                  lifecycle trace ~execution_id:0 ~phase:"load" ~generation
                    ~outcome:"succeeded" ()
              | None, Some reason ->
                  lifecycle trace ~execution_id:0 ~phase:"load"
                    ~outcome:"failed" ~reason ()
              | None, None ->
                  lifecycle trace ~execution_id:0 ~phase:"load"
                    ~outcome:"succeeded" ()
              | Some _, Some _ -> assert false);
              let commands =
                match commands_with_generation base_commands generation with
                | Ok commands -> commands
                | Error error ->
                    failwith
                      ("script generation invariant violated: "
                     ^ Error.to_string error)
              in
              let configured_semantics =
                base_semantics
                @
                match generation with
                | None -> []
                | Some generation -> Scripting.descriptors generation
              in
              let plugin_host =
                Profiler.measure profiler Profiler.Extension_load (fun () ->
                    Plugins.load ~config:plugins ~base_commands:commands
                      ~base_semantics:configured_semantics
                      ?base_bindings:
                        (match generation with
                        | None -> None
                        | Some generation ->
                            Some (Scripting.bindings generation))
                      ?base_binding_layers:
                        (match generation with
                        | None -> None
                        | Some generation ->
                            Some (Scripting.binding_layers generation))
                      ())
              in
              trace_plugins trace ~execution_id:0 ~phase:"load" plugin_host;
              trace_runtime_events trace profiler ~execution_id:0 plugin_host;
              let commands =
                match commands_with_plugins commands plugin_host with
                | Ok commands -> commands
                | Error error ->
                    failwith
                      ("plugin snapshot invariant violated: "
                     ^ Error.to_string error)
              in
              let semantic_behaviors =
                semantic_behaviors_with_plugins generation plugin_host
              in
              let runtime =
                match model with
                | Vim ->
                    Vim_runtime.create ~commands ~semantic_behaviors
                      ?syntax_service ~trace ~profiler ~document ()
                    |> Result.map (fun runtime -> Vim_runtime runtime)
                | Selection ->
                    Selection_runtime.create ~commands ~semantic_behaviors
                      ?syntax_service ~trace ~profiler ~document ()
                    |> Result.map (fun runtime -> Selection_runtime runtime)
                | Direct ->
                    Direct_runtime.create ~commands ~semantic_behaviors
                      ?syntax_service ~trace ~profiler ~document ()
                    |> Result.map (fun runtime -> Direct_runtime runtime)
                | Structural ->
                    Structural_runtime.create ~commands ~semantic_behaviors
                      ?syntax_service ~trace ~profiler ~document ()
                    |> Result.map (fun runtime -> Structural_runtime runtime)
                | Script -> (
                    match Option.bind generation Scripting.model with
                    | None ->
                        Error
                          (Error.Invalid_command_arguments
                             "--model script requires a Lua zenbu.model \
                              declaration")
                    | Some model ->
                        Script_model.configure model;
                        Script_runtime.create ~commands ~semantic_behaviors
                          ?syntax_service ~trace ~profiler ~document ()
                        |> Result.map (fun runtime -> Script_runtime runtime))
              in
              runtime
              |> Result.map (fun active ->
                  let file_watcher =
                    Option.value file_watcher ~default:(File_watcher.create ())
                  in
                  let language_client =
                    Option.bind file_path (fun path ->
                        Language.Registry.find_for_path language_registry
                          ~language_id:language path
                        |> Option.map (fun server ->
                            Lsp.start ~config:server
                              ~document_id:"terminal-buffer" ~document_version:0
                              ~file_path:path ~contents ~trace ~profiler))
                  in
                  let session =
                    {
                      active;
                      base_commands;
                      base_semantics;
                      config;
                      generation;
                      plugins = plugin_host;
                      plugins_config = plugins;
                      next_generation_id = 2;
                      last_reload_error = config_error;
                      delivering_events = [];
                      file_path;
                      buffer_name = None;
                      language_override = language;
                      saved_version = 0;
                      saved_contents = contents;
                      saved_snapshot;
                      file_watcher;
                      file_watch_notices = [];
                      project_root = None;
                      project_search = None;
                      layout = Layout.single 0;
                      focused_pane = 0;
                      pane_viewports = [ (0, Zenbu_view.Viewport.origin) ];
                      pane_view_positions =
                        [
                          ( (0, 0),
                            {
                              buffer_id = 0;
                              document_version = 0;
                              selections = [ (0, 0) ];
                              primary = 0;
                              stale = false;
                            } );
                        ];
                      next_pane_id = 1;
                      dimensions;
                      presentation;
                      theme;
                      message = config_message;
                      quit_armed = false;
                      inspector = None;
                      presentation_cache = None;
                      search = None;
                      interaction = Idle;
                      language_client;
                      diagnostics = [];
                      language_registry;
                      current_buffer_id = 0;
                      inactive_buffers = [];
                      next_buffer_id = 1;
                      pane_buffers = [ (0, 0) ];
                      mouse_drag = None;
                      pending_binding = [];
                      active_modes =
                        (match generation with
                        | None -> []
                        | Some generation -> Scripting.initial_modes generation);
                      active_binding_layers = [];
                      macro_recording = None;
                      macros = [];
                      last_macro_register = None;
                      macro_replay_pending = None;
                      macro_replaying = false;
                      macro_control = false;
                      kill_ring = [];
                      system_clipboard;
                      jobs = None;
                      locations = [];
                      backward_jumps = [];
                      forward_jumps = [];
                    }
                  in
                  Option.iter
                    (fun snapshot ->
                      Option.iter
                        (fun path ->
                          File_watcher.watch file_watcher ~path ~snapshot)
                        file_path)
                    saved_snapshot;
                  session)))

let context_of_active = function
  | Vim_runtime runtime -> Vim_runtime.context runtime
  | Selection_runtime runtime -> Selection_runtime.context runtime
  | Direct_runtime runtime -> Direct_runtime.context runtime
  | Structural_runtime runtime -> Structural_runtime.context runtime
  | Script_runtime runtime -> Script_runtime.context runtime

let context session = context_of_active session.active

let history_of_active = function
  | Vim_runtime runtime -> Vim_runtime.history runtime
  | Selection_runtime runtime -> Selection_runtime.history runtime
  | Direct_runtime runtime -> Direct_runtime.history runtime
  | Structural_runtime runtime -> Structural_runtime.history runtime
  | Script_runtime runtime -> Script_runtime.history runtime

let rebase_view_position position history =
  if position.stale then position
  else
    let current_version =
      History.current history |> Document.version |> Document_version.to_int
    in
    if position.document_version = current_version then position
    else
      let rec replay version selections = function
        | [] -> if version = current_version then Some selections else None
        | change :: rest ->
            let before_version =
              History.before change |> Document.version
              |> Document_version.to_int
            in
            if before_version < version then replay version selections rest
            else if before_version > version then None
            else
              let edits = Transaction.edits (History.transaction change) in
              let selections =
                List.map
                  (fun (anchor_offset, head_offset) ->
                    ( Document.transform_offset edits anchor_offset,
                      Document.transform_offset edits head_offset ))
                  selections
              in
              let version =
                History.after change |> Document.version
                |> Document_version.to_int
              in
              replay version selections rest
      in
      match
        replay position.document_version position.selections
          (History.lineage history)
      with
      | Some selections ->
          { position with document_version = current_version; selections }
      | None -> { position with stale = true }

let active_status = function
  | Vim_runtime runtime -> Vim_runtime.status runtime
  | Selection_runtime runtime -> Selection_runtime.status runtime
  | Direct_runtime runtime -> Direct_runtime.status runtime
  | Structural_runtime runtime -> Structural_runtime.status runtime
  | Script_runtime runtime -> Script_runtime.status runtime

let host_status ~id ~label ~description ?(text_entry = false) () =
  Model_status.create ~id ~label ~description
    ~input_mode:
      (if text_entry then Model_status.Text_entry else Model_status.Key_commands)
    ()
  |> Result.get_ok

let active_script_mode session =
  match (session.active_modes, session.generation) with
  | id :: _, Some generation ->
      List.find_opt
        (fun mode -> String.equal (Scripting.mode_id mode) id)
        (Scripting.modes generation)
  | [], _ | _, None -> None

let status session =
  match session.interaction with
  | Idle -> (
      match active_script_mode session with
      | None -> active_status session.active
      | Some mode ->
          host_status
            ~id:("host-custom-mode:" ^ Scripting.mode_id mode)
            ~label:(Scripting.mode_title mode)
            ~description:(Scripting.mode_description mode)
            ~text_entry:
              (Scripting.mode_input_mode mode = Model_status.Text_entry)
            ())
  | Search_prompt { kind; _ } ->
      host_status ~id:"host-search" ~label:"SEARCH"
        ~description:
          (match kind with
          | Literal ->
              "enter a literal Unicode search; Enter confirms and Escape \
               cancels"
          | Regexp ->
              "enter a UTF-8-safe Str regexp search; Enter confirms and Escape \
               cancels")
        ~text_entry:true ()
  | Palette _ ->
      host_status ~id:"host-palette" ~label:"COMMAND"
        ~description:"filter registered commands from all active providers"
        ~text_entry:true ()
  | Command_line _ ->
      host_status ~id:"host-command-line" ~label:"COMMAND LINE"
        ~description:
          "enter a colon-prefixed registered command ID and space-separated \
           typed arguments"
        ~text_entry:true ()
  | Command_prompt _ ->
      host_status ~id:"host-command-argument" ~label:"ARGUMENT"
        ~description:"enter the current typed command argument; Escape cancels"
        ~text_entry:true ()
  | Save_as_prompt _ ->
      host_status ~id:"host-save-as" ~label:"SAVE AS"
        ~description:"enter a destination path; Enter saves atomically"
        ~text_entry:true ()
  | Open_buffer_prompt _ ->
      host_status ~id:"host-open-buffer" ~label:"OPEN"
        ~description:"enter a path; Enter opens it in the focused view"
        ~text_entry:true ()
  | File_picker _ ->
      host_status ~id:"host-file-picker" ~label:"FILES"
        ~description:
          "filter files under the selected project root; Enter opens and \
           Escape cancels"
        ~text_entry:true ()
  | Project_search_view _ ->
      host_status ~id:"host-project-search" ~label:"PROJECT SEARCH"
        ~description:
          "browse bounded project-search results; Enter opens and Escape \
           cancels"
        ()
  | Query_replace _ ->
      host_status ~id:"host-query-replace" ~label:"QUERY REPLACE"
        ~description:"s skip, r replace, a replace remaining, q quit" ()
  | Model_picker _ ->
      host_status ~id:"host-model-picker" ~label:"MODEL"
        ~description:"choose an editing model without replacing semantic state"
        ()
  | Help_view ->
      host_status ~id:"host-help" ~label:"HELP"
        ~description:"host and active-model discovery" ()
  | Hover_view _ ->
      host_status ~id:"language-hover" ~label:"HOVER"
        ~description:"read-only language hover; Escape closes" ()
  | Completion_view _ ->
      host_status ~id:"language-completion" ~label:"COMPLETE"
        ~description:"filter and choose a language completion item"
        ~text_entry:true ()
  | Rename_prompt _ ->
      host_status ~id:"language-rename" ~label:"RENAME"
        ~description:"enter a new symbol name; Enter requests rename"
        ~text_entry:true ()

let model_of_active = function
  | Vim_runtime _ -> Vim
  | Selection_runtime _ -> Selection
  | Direct_runtime _ -> Direct
  | Structural_runtime _ -> Structural
  | Script_runtime _ -> Script

let script_runtime_active = function Script_runtime _ -> true | _ -> false
let model session = model_of_active session.active
let pane_ids session = Layout.panes session.layout
let pane_count session = List.length (pane_ids session)
let focused_pane session = session.focused_pane

let buffer_line_rows session =
  if session.dimensions.rows <= 0 then 0
  else
    match Zenbu_view.Presentation.buffer_line session.presentation with
    | Zenbu_view.Presentation.Visible -> 1
    | Zenbu_view.Presentation.Hidden_buffer_line -> 0

let workspace_height session =
  max 0 (session.dimensions.rows - buffer_line_rows session)

let layout_bounds session =
  Layout.bounds session.layout ~width:session.dimensions.columns
    ~height:(workspace_height session)

let current_buffer session =
  {
    id = session.current_buffer_id;
    active = session.active;
    file_path = session.file_path;
    buffer_name = session.buffer_name;
    language_override = session.language_override;
    saved_version = session.saved_version;
    saved_contents = session.saved_contents;
    saved_snapshot = session.saved_snapshot;
    language_client = session.language_client;
    diagnostics = session.diagnostics;
    presentation_cache = session.presentation_cache;
    search = session.search;
    active_modes = session.active_modes;
    active_binding_layers = session.active_binding_layers;
  }

let buffer_ids session =
  session.current_buffer_id
  :: List.map (fun (buffer : buffer) -> buffer.id) session.inactive_buffers

let buffer_count session = List.length (buffer_ids session)

let buffer_for_id session id =
  if id = session.current_buffer_id then Some (current_buffer session)
  else
    List.find_opt
      (fun (buffer : buffer) -> buffer.id = id)
      session.inactive_buffers

let view_position_of_context ~buffer_id context =
  let selections = Editor_context.selections context in
  {
    buffer_id;
    document_version = Editor_context.document_version context;
    selections =
      List.map
        (fun (selection : Editor_context.selection) ->
          (selection.anchor_offset, selection.head_offset))
        selections.selections;
    primary = selections.primary_index;
    stale = false;
  }

let pane_view_position session ~pane ~buffer_id =
  List.assoc_opt (pane, buffer_id) session.pane_view_positions

let set_pane_view_position session ~pane position =
  {
    session with
    pane_view_positions =
      ((pane, position.buffer_id), position)
      :: List.filter
           (fun ((candidate_pane, candidate_buffer), _) ->
             candidate_pane <> pane || candidate_buffer <> position.buffer_id)
           session.pane_view_positions;
  }

let workspace_documents session =
  current_buffer session :: session.inactive_buffers
  |> List.filter_map (fun (buffer : buffer) ->
      Option.map
        (fun path ->
          ( Language.Uri.file_of_path path,
            Editor_context.contents (context_of_active buffer.active) ))
        buffer.file_path)

let synchronize_workspace_documents session =
  let documents = workspace_documents session in
  current_buffer session :: session.inactive_buffers
  |> List.iter (fun (buffer : buffer) ->
      Option.iter
        (fun client -> Lsp.set_workspace_documents client documents)
        buffer.language_client);
  session

let buffer_id_for_pane session pane =
  List.assoc_opt pane session.pane_buffers
  |> Option.value ~default:session.current_buffer_id

let focused_buffer session = buffer_id_for_pane session session.focused_pane

let set_pane_buffer session pane buffer =
  let pane_buffers =
    (pane, buffer)
    :: List.filter
         (fun (candidate, _) -> candidate <> pane)
         session.pane_buffers
  in
  { session with pane_buffers }

let capture_pane_view_position session pane =
  let buffer_id = buffer_id_for_pane session pane in
  if buffer_id <> session.current_buffer_id then session
  else
    set_pane_view_position session ~pane
      (view_position_of_context ~buffer_id (context session))

let capture_focused_view_position session =
  capture_pane_view_position session session.focused_pane

let restore_active_view_position active position =
  let restore restore_runtime wrap runtime =
    restore_runtime runtime ~selections:position.selections
      ~primary:position.primary
    |> Result.map wrap
  in
  match active with
  | Vim_runtime runtime ->
      restore Vim_runtime.restore_selections
        (fun runtime -> Vim_runtime runtime)
        runtime
  | Selection_runtime runtime ->
      restore Selection_runtime.restore_selections
        (fun runtime -> Selection_runtime runtime)
        runtime
  | Direct_runtime runtime ->
      restore Direct_runtime.restore_selections
        (fun runtime -> Direct_runtime runtime)
        runtime
  | Structural_runtime runtime ->
      restore Structural_runtime.restore_selections
        (fun runtime -> Structural_runtime runtime)
        runtime
  | Script_runtime runtime ->
      restore Script_runtime.restore_selections
        (fun runtime -> Script_runtime runtime)
        runtime

let same_view_position context position =
  let selections = Editor_context.selections context in
  position.document_version = Editor_context.document_version context
  && position.primary = selections.primary_index
  && position.selections
     = List.map
         (fun (selection : Editor_context.selection) ->
           (selection.anchor_offset, selection.head_offset))
         selections.selections

let restore_pane_view_position session pane =
  let buffer_id = buffer_id_for_pane session pane in
  if buffer_id <> session.current_buffer_id then session
  else
    let position =
      match pane_view_position session ~pane ~buffer_id with
      | None -> view_position_of_context ~buffer_id (context session)
      | Some position ->
          rebase_view_position position (history_of_active session.active)
    in
    let session = set_pane_view_position session ~pane position in
    if position.stale || same_view_position (context session) position then
      if position.stale then capture_pane_view_position session pane
      else session
    else
      match restore_active_view_position session.active position with
      | Error error ->
          {
            session with
            message =
              Some
                ("workspace: view position restore failed: "
               ^ Error.to_string error);
          }
      | Ok active ->
          { session with active } |> fun session ->
          capture_pane_view_position session pane

let refresh_pane_view_positions session =
  {
    session with
    pane_view_positions =
      List.map
        (fun ((pane, buffer_id), position) ->
          let position =
            match buffer_for_id session buffer_id with
            | None -> { position with stale = true }
            | Some buffer ->
                rebase_view_position position (history_of_active buffer.active)
          in
          ((pane, buffer_id), position))
        session.pane_view_positions;
  }

let load_buffer ?(reset_interaction = true) session (buffer : buffer) =
  let next =
    {
      session with
      active = buffer.active;
      file_path = buffer.file_path;
      buffer_name = buffer.buffer_name;
      language_override = buffer.language_override;
      saved_version = buffer.saved_version;
      saved_contents = buffer.saved_contents;
      saved_snapshot = buffer.saved_snapshot;
      language_client = buffer.language_client;
      diagnostics = buffer.diagnostics;
      presentation_cache = buffer.presentation_cache;
      search = buffer.search;
      active_modes = buffer.active_modes;
      active_binding_layers = buffer.active_binding_layers;
      current_buffer_id = buffer.id;
      inactive_buffers =
        current_buffer session
        :: List.filter
             (fun (candidate : buffer) -> candidate.id <> buffer.id)
             session.inactive_buffers;
    }
  in
  let next = synchronize_macro_context next in
  let next =
    { next with active = active_with_kill_ring next.active next.kill_ring }
  in
  if reset_interaction then
    { next with interaction = Idle; inspector = None; quit_armed = false }
  else next

let activate_buffer ?reset_interaction session buffer_id =
  if buffer_id = session.current_buffer_id then session
  else
    match buffer_for_id session buffer_id with
    | None ->
        {
          session with
          message = Some ("workspace: unknown buffer " ^ string_of_int buffer_id);
          interaction = Idle;
          inspector = None;
        }
    | Some buffer -> load_buffer ?reset_interaction session buffer

let pane_viewport session pane =
  match List.assoc_opt pane session.pane_viewports with
  | Some viewport -> viewport
  | None -> Zenbu_view.Viewport.origin

let set_pane_viewport session pane viewport =
  {
    session with
    pane_viewports =
      List.map
        (fun (candidate, current) ->
          if candidate = pane then (candidate, viewport)
          else (candidate, current))
        session.pane_viewports;
  }

let pane_rectangle session pane = layout_bounds session |> List.assoc_opt pane

let focus_pane session pane =
  if not (List.mem pane (pane_ids session)) then session
  else
    let session = capture_focused_view_position session in
    let session = { session with focused_pane = pane } in
    activate_buffer session (focused_buffer session) |> fun session ->
    restore_pane_view_position session pane

let split_pane session orientation =
  let available =
    match pane_rectangle session session.focused_pane with
    | None -> false
    | Some rectangle -> (
        match orientation with
        | Layout.Vertical -> rectangle.width >= 3
        | Layout.Horizontal -> rectangle.height >= 3)
  in
  if not available then
    {
      session with
      message = Some "workspace: terminal is too small to split this pane";
      inspector = None;
    }
  else
    match
      Layout.split session.layout ~pane:session.focused_pane
        ~new_pane:session.next_pane_id orientation
    with
    | Error error ->
        {
          session with
          message = Some ("workspace: " ^ Layout.error_to_string error);
          inspector = None;
        }
    | Ok layout ->
        let session = capture_focused_view_position session in
        let buffer_id = focused_buffer session in
        let position =
          pane_view_position session ~pane:session.focused_pane ~buffer_id
          |> Option.value
               ~default:(view_position_of_context ~buffer_id (context session))
        in
        {
          session with
          layout;
          focused_pane = session.next_pane_id;
          pane_viewports =
            (session.next_pane_id, pane_viewport session session.focused_pane)
            :: session.pane_viewports;
          pane_view_positions =
            ((session.next_pane_id, buffer_id), position)
            :: session.pane_view_positions;
          pane_buffers =
            (session.next_pane_id, buffer_id) :: session.pane_buffers;
          next_pane_id = session.next_pane_id + 1;
          message = Some "workspace: split current view";
          inspector = None;
        }

let focus_next_pane session =
  match pane_ids session with
  | [] -> session
  | panes ->
      let next =
        match
          List.find_index (fun pane -> pane = session.focused_pane) panes
        with
        | None -> List.hd panes
        | Some index -> List.nth panes ((index + 1) mod List.length panes)
      in
      let session = focus_pane session next in
      {
        session with
        message = Some "workspace: focused next view";
        inspector = None;
      }

let close_pane session =
  match Layout.close session.layout ~pane:session.focused_pane with
  | Error error ->
      {
        session with
        message = Some ("workspace: " ^ Layout.error_to_string error);
        inspector = None;
      }
  | Ok layout ->
      let panes = Layout.panes layout in
      let next =
        let session = capture_focused_view_position session in
        {
          session with
          layout;
          focused_pane = List.hd panes;
          pane_viewports =
            List.filter
              (fun (pane, _) -> List.mem pane panes)
              session.pane_viewports;
          pane_view_positions =
            List.filter
              (fun ((pane, _), _) -> List.mem pane panes)
              session.pane_view_positions;
          pane_buffers =
            List.filter
              (fun (pane, _) -> List.mem pane panes)
              session.pane_buffers;
          message = Some "workspace: closed current view";
          inspector = None;
        }
      in
      activate_buffer next (focused_buffer next) |> fun session ->
      restore_pane_view_position session next.focused_pane

let only_pane session =
  let session = capture_focused_view_position session in
  let pane = session.focused_pane in
  {
    session with
    layout = Layout.single pane;
    pane_viewports = [ (pane, pane_viewport session pane) ];
    pane_view_positions =
      List.filter
        (fun ((candidate_pane, _), _) -> candidate_pane = pane)
        session.pane_view_positions;
    pane_buffers = [ (pane, focused_buffer session) ];
    message = Some "workspace: kept current view";
    inspector = None;
  }

let resize_focused_pane session ~dimension ~delta =
  match
    Layout.resize session.layout ~pane:session.focused_pane ~dimension ~delta
      ~width:session.dimensions.columns ~height:(workspace_height session)
  with
  | Error error ->
      {
        session with
        message = Some ("workspace: " ^ Layout.error_to_string error);
        inspector = None;
      }
  | Ok layout ->
      {
        session with
        layout;
        message = Some "workspace: resized current view";
        inspector = None;
      }

let balance_panes session =
  {
    session with
    layout = Layout.balance session.layout;
    message = Some "workspace: balanced split views";
    inspector = None;
  }

let buffer_label ~buffer_name ~file_path =
  match buffer_name with
  | Some name -> name
  | None -> (
      match file_path with
      | None -> "[No Name]"
      | Some path -> Filename.basename path)

let filename session =
  buffer_label ~buffer_name:session.buffer_name ~file_path:session.file_path

let configuration_error session = session.last_reload_error

let plugin_load_errors session =
  Plugins.views session.plugins
  |> List.filter_map (fun view ->
      match (Plugins.view_state view, Plugins.view_error view) with
      | Plugins.Failed, Some error -> Some error
      | Plugins.Active, None | Plugins.Active, Some _ | Plugins.Failed, None ->
          None)

let current_dirty session =
  let context = context session in
  Editor_context.document_version context <> session.saved_version
  && not (String.equal (Editor_context.contents context) session.saved_contents)

let buffer_dirty (buffer : buffer) =
  let context = context_of_active buffer.active in
  Editor_context.document_version context <> buffer.saved_version
  && not (String.equal (Editor_context.contents context) buffer.saved_contents)

let take_last maximum values =
  let excess = List.length values - maximum in
  if excess <= 0 then values
  else
    let rec drop remaining = function
      | values when remaining <= 0 -> values
      | [] -> []
      | _ :: rest -> drop (remaining - 1) rest
    in
    drop excess values

let file_watch_notice session (event : File_watcher.event) =
  let path = Option.value ~default:"all watched paths" event.path in
  let open_buffers = current_buffer session :: session.inactive_buffers in
  let buffers =
    match event.path with
    | None -> open_buffers
    | Some path ->
        List.filter
          (fun (buffer : buffer) -> buffer.file_path = Some path)
          open_buffers
  in
  let buffer_state =
    match buffers with
    | [] -> "no open buffer retained"
    | buffers when List.exists buffer_dirty buffers ->
        "dirty buffer retained; no automatic reload"
    | _ -> "clean buffer retained; no automatic reload"
  in
  Printf.sprintf "file watch: %s: %s; %s" path
    (File_watcher.event_kind_name event.kind)
    buffer_state

let record_file_watch session event =
  let notice = file_watch_notice session event in
  {
    session with
    file_watch_notices =
      take_last maximum_file_watch_notices
        (session.file_watch_notices @ [ notice ]);
    message = Some notice;
    quit_armed = false;
  }

let poll_file_watcher session =
  File_watcher.drain session.file_watcher
  |> List.fold_left record_file_watch session

let file_watch_lines session =
  [
    "File watches";
    "policy: report only; no automatic reload, overwrite, or document mutation";
    "retained-events: " ^ string_of_int (List.length session.file_watch_notices);
  ]
  @
  if session.file_watch_notices = [] then [ "events: none" ]
  else session.file_watch_notices

let dirty session =
  current_dirty session || List.exists buffer_dirty session.inactive_buffers

let primary_offset session =
  let selections = Editor_context.selections (context session) in
  let primary = List.nth selections.selections selections.primary_index in
  primary.Editor_context.head_offset

let language_status_lines session =
  match session.language_client with
  | None ->
      [
        "Language service";
        "state: unavailable";
        "No configured language server matches this buffer path.";
      ]
  | Some client ->
      let status = Lsp.status client in
      [
        "Language service";
        "language: " ^ Option.value ~default:"none" status.language_id;
        "server: " ^ Option.value ~default:"none" status.server_id;
        "executable: " ^ Option.value ~default:"none" status.executable;
        "workspace: " ^ Option.value ~default:"none" status.workspace_root;
        "state: " ^ Language.server_state_name status.state;
        "position encoding: "
        ^ (Option.map Language.Position.encoding_name status.position_encoding
          |> Option.value ~default:"not negotiated");
        "synchronization: "
        ^ (Option.map
             (function
               | `None -> "none"
               | `Full -> "full"
               | `Incremental -> "incremental")
             status.sync_kind
          |> Option.value ~default:"not negotiated");
        "pending requests: " ^ string_of_int status.pending_requests;
        "diagnostics: " ^ string_of_int (List.length session.diagnostics);
        "last error: " ^ Option.value ~default:"none" status.last_error;
      ]

let language_unavailable session =
  {
    session with
    message = Some "language server is unavailable; inspect language.status";
    quit_armed = false;
  }

let request_language session request =
  match session.language_client with
  | None -> language_unavailable session
  | Some client -> (
      Lsp.set_execution_id client
        ~execution_id:
          (Option.value ~default:0 (last_execution_of_active session.active));
      match request client with
      | Ok request_id ->
          {
            session with
            message =
              Some ("language request " ^ string_of_int request_id ^ " pending");
            quit_armed = false;
          }
      | Error reason ->
          {
            session with
            message = Some ("language request failed: " ^ reason);
            quit_armed = false;
          })

let begin_hover session =
  request_language session (fun client ->
      Lsp.request_hover client ~byte_offset:(primary_offset session))

let begin_definition session =
  request_language session (fun client ->
      Lsp.request_definition client ~byte_offset:(primary_offset session))

let begin_completion session =
  request_language session (fun client ->
      Lsp.request_completion client ~byte_offset:(primary_offset session))

let begin_rename session =
  match session.language_client with
  | None -> language_unavailable session
  | Some _ ->
      {
        session with
        interaction = Rename_prompt "";
        message = Some "rename: enter a new symbol name";
        quit_armed = false;
        inspector = None;
      }

let replace_language_client session path =
  Option.iter Lsp.close session.language_client;
  match syntax_service ?language:session.language_override (Some path) with
  | Error error ->
      {
        session with
        language_client = None;
        diagnostics = [];
        message = Some ("syntax activation failed: " ^ Error.to_string error);
      }
  | Ok syntax_service -> (
      match active_with_syntax_service session.active syntax_service with
      | Error error ->
          {
            session with
            language_client = None;
            diagnostics = [];
            message = Some ("syntax activation failed: " ^ Error.to_string error);
          }
      | Ok active ->
          let language_client =
            Language.Registry.find_for_path session.language_registry
              ~language_id:session.language_override path
            |> Option.map (fun server ->
                Lsp.start ~config:server ~document_id:"terminal-buffer"
                  ~document_version:
                    (Editor_context.document_version (context session))
                  ~file_path:path
                  ~contents:(Editor_context.contents (context session))
                  ~trace:(trace_of_active active)
                  ~profiler:(profiler_of_active active))
          in
          { session with active; language_client; diagnostics = [] })

let observe_language_document_version session =
  Option.iter
    (fun client ->
      Lsp.observe_document_version client
        ~document_version:(Editor_context.document_version (context session)))
    session.language_client;
  session

let last_message messages =
  match List.rev messages with
  | [] -> None
  | message :: _ -> Some message.Model_effect.text

let handle_model_input session input =
  let next =
    match session.active with
    | Vim_runtime runtime -> (
        match Vim_runtime.handle_input runtime input with
        | Error error ->
            ( {
                session with
                message = Some (Error.to_string error);
                quit_armed = false;
                inspector = None;
              },
              [] )
        | Ok (runtime, step) ->
            ( {
                session with
                active = Vim_runtime runtime;
                message = last_message (Vim_runtime.messages step);
                quit_armed = false;
                inspector = None;
              },
              Vim_runtime.effects step ))
    | Selection_runtime runtime -> (
        match Selection_runtime.handle_input runtime input with
        | Error error ->
            ( {
                session with
                message = Some (Error.to_string error);
                quit_armed = false;
                inspector = None;
              },
              [] )
        | Ok (runtime, step) ->
            ( {
                session with
                active = Selection_runtime runtime;
                message = last_message (Selection_runtime.messages step);
                quit_armed = false;
                inspector = None;
              },
              Selection_runtime.effects step ))
    | Direct_runtime runtime -> (
        match Direct_runtime.handle_input runtime input with
        | Error error ->
            ( {
                session with
                message = Some (Error.to_string error);
                quit_armed = false;
                inspector = None;
              },
              [] )
        | Ok (runtime, step) ->
            ( {
                session with
                active = Direct_runtime runtime;
                message = last_message (Direct_runtime.messages step);
                quit_armed = false;
                inspector = None;
              },
              Direct_runtime.effects step ))
    | Structural_runtime runtime -> (
        match Structural_runtime.handle_input runtime input with
        | Error error ->
            ( {
                session with
                message = Some (Error.to_string error);
                quit_armed = false;
                inspector = None;
              },
              [] )
        | Ok (runtime, step) ->
            ( {
                session with
                active = Structural_runtime runtime;
                message = last_message (Structural_runtime.messages step);
                quit_armed = false;
                inspector = None;
              },
              Structural_runtime.effects step ))
    | Script_runtime runtime -> (
        match Script_runtime.handle_input runtime input with
        | Error error ->
            ( {
                session with
                message = Some (Error.to_string error);
                quit_armed = false;
                inspector = None;
              },
              [] )
        | Ok (runtime, step) ->
            ( {
                session with
                active = Script_runtime runtime;
                message = last_message (Script_runtime.messages step);
                quit_armed = false;
                inspector = None;
              },
              Script_runtime.effects step ))
  in
  next

let active_with_extensions active ~commands ~semantic_behaviors =
  match active with
  | Vim_runtime runtime ->
      Vim_runtime.with_extensions runtime ~commands ~semantic_behaviors
      |> fun runtime -> Vim_runtime runtime
  | Selection_runtime runtime ->
      Selection_runtime.with_extensions runtime ~commands ~semantic_behaviors
      |> fun runtime -> Selection_runtime runtime
  | Direct_runtime runtime ->
      Direct_runtime.with_extensions runtime ~commands ~semantic_behaviors
      |> fun runtime -> Direct_runtime runtime
  | Structural_runtime runtime ->
      Structural_runtime.with_extensions runtime ~commands ~semantic_behaviors
      |> fun runtime -> Structural_runtime runtime
  | Script_runtime runtime ->
      Script_runtime.with_extensions runtime ~commands ~semantic_behaviors
      |> fun runtime -> Script_runtime runtime

let active_commands = function
  | Vim_runtime runtime -> Vim_runtime.commands runtime
  | Selection_runtime runtime -> Selection_runtime.commands runtime
  | Direct_runtime runtime -> Direct_runtime.commands runtime
  | Structural_runtime runtime -> Structural_runtime.commands runtime
  | Script_runtime runtime -> Script_runtime.commands runtime

let active_semantic_behaviors = function
  | Vim_runtime runtime -> Vim_runtime.semantic_behaviors runtime
  | Selection_runtime runtime -> Selection_runtime.semantic_behaviors runtime
  | Direct_runtime runtime -> Direct_runtime.semantic_behaviors runtime
  | Structural_runtime runtime -> Structural_runtime.semantic_behaviors runtime
  | Script_runtime runtime -> Script_runtime.semantic_behaviors runtime

let shared_state = function
  | Vim_runtime runtime -> Vim_runtime.shared_state runtime
  | Selection_runtime runtime -> Selection_runtime.shared_state runtime
  | Direct_runtime runtime -> Direct_runtime.shared_state runtime
  | Structural_runtime runtime -> Structural_runtime.shared_state runtime
  | Script_runtime runtime -> Script_runtime.shared_state runtime

let active_from_shared ?script_model model shared =
  match model with
  | Vim ->
      Vim_runtime.create_from_shared shared
      |> Result.map (fun value -> Vim_runtime value)
  | Selection ->
      Selection_runtime.create_from_shared shared
      |> Result.map (fun value -> Selection_runtime value)
  | Direct ->
      Direct_runtime.create_from_shared shared
      |> Result.map (fun value -> Direct_runtime value)
  | Structural ->
      Structural_runtime.create_from_shared shared
      |> Result.map (fun value -> Structural_runtime value)
  | Script -> (
      match script_model with
      | None ->
          Error
            (Error.Invalid_command_arguments
               "script model is unavailable in the active Lua configuration")
      | Some model ->
          Script_model.configure model;
          Script_runtime.create_from_shared shared
          |> Result.map (fun value -> Script_runtime value))

let create_active ~model ~commands ~semantic_behaviors ?syntax_service ~trace
    ~profiler ?script_model ~document () =
  match model with
  | Vim ->
      Vim_runtime.create ~commands ~semantic_behaviors ?syntax_service ~trace
        ~profiler ~document ()
      |> Result.map (fun runtime -> Vim_runtime runtime)
  | Selection ->
      Selection_runtime.create ~commands ~semantic_behaviors ?syntax_service
        ~trace ~profiler ~document ()
      |> Result.map (fun runtime -> Selection_runtime runtime)
  | Direct ->
      Direct_runtime.create ~commands ~semantic_behaviors ?syntax_service ~trace
        ~profiler ~document ()
      |> Result.map (fun runtime -> Direct_runtime runtime)
  | Structural ->
      Structural_runtime.create ~commands ~semantic_behaviors ?syntax_service
        ~trace ~profiler ~document ()
      |> Result.map (fun runtime -> Structural_runtime runtime)
  | Script -> (
      match script_model with
      | None ->
          Error
            (Error.Invalid_command_arguments
               "script model is unavailable in the active Lua configuration")
      | Some model ->
          Script_model.configure model;
          Script_runtime.create ~commands ~semantic_behaviors ?syntax_service
            ~trace ~profiler ~document ()
          |> Result.map (fun runtime -> Script_runtime runtime))

let create_buffer session ~id ?file_path ?buffer_name ?language ?saved_snapshot
    ?editing_model ~contents () =
  let language = Option.value ~default:session.language_override language in
  Result.bind (document ~contents) (fun document ->
      Result.bind (syntax_service ?language file_path) (fun syntax_service ->
          let model = Option.value ~default:(model session) editing_model in
          let commands = active_commands session.active in
          let semantic_behaviors = active_semantic_behaviors session.active in
          let trace = trace_of_active session.active in
          let profiler = profiler_of_active session.active in
          create_active ~model ~commands ~semantic_behaviors ?syntax_service
            ~trace ~profiler
            ?script_model:(Option.bind session.generation Scripting.model)
            ~document ()
          |> Result.map (fun active ->
              let active = active_with_kill_ring active session.kill_ring in
              let language_client =
                Option.bind file_path (fun path ->
                    Language.Registry.find_for_path session.language_registry
                      ~language_id:language path
                    |> Option.map (fun server ->
                        Lsp.start ~config:server
                          ~document_id:("terminal-buffer-" ^ string_of_int id)
                          ~document_version:0 ~file_path:path ~contents ~trace
                          ~profiler))
              in
              let buffer =
                {
                  id;
                  active;
                  file_path;
                  buffer_name;
                  language_override = language;
                  saved_version = 0;
                  saved_contents = contents;
                  saved_snapshot;
                  language_client;
                  diagnostics = [];
                  presentation_cache = None;
                  search = None;
                  active_modes =
                    (match session.generation with
                    | None -> []
                    | Some generation -> Scripting.initial_modes generation);
                  active_binding_layers = [];
                }
              in
              Option.iter
                (fun snapshot ->
                  Option.iter
                    (fun path ->
                      File_watcher.watch session.file_watcher ~path ~snapshot)
                    file_path)
                saved_snapshot;
              buffer)))

let show_new_buffer session (buffer : buffer) =
  let session = capture_focused_view_position session in
  let next =
    {
      session with
      active = buffer.active;
      file_path = buffer.file_path;
      buffer_name = buffer.buffer_name;
      language_override = buffer.language_override;
      saved_version = buffer.saved_version;
      saved_contents = buffer.saved_contents;
      saved_snapshot = buffer.saved_snapshot;
      language_client = buffer.language_client;
      diagnostics = buffer.diagnostics;
      presentation_cache = buffer.presentation_cache;
      search = buffer.search;
      active_modes = buffer.active_modes;
      active_binding_layers = buffer.active_binding_layers;
      current_buffer_id = buffer.id;
      inactive_buffers = current_buffer session :: session.inactive_buffers;
      next_buffer_id = buffer.id + 1;
      pane_buffers =
        (session.focused_pane, buffer.id)
        :: List.filter
             (fun (pane, _) -> pane <> session.focused_pane)
             session.pane_buffers;
      interaction = Idle;
      inspector = None;
      quit_armed = false;
    }
  in
  set_pane_view_position next ~pane:next.focused_pane
    (view_position_of_context ~buffer_id:buffer.id (context next))

let new_buffer_with_contents ?buffer_name session ~contents ~message =
  match
    create_buffer session ~id:session.next_buffer_id ?buffer_name ~contents ()
  with
  | Error error ->
      {
        session with
        message = Some ("workspace: new buffer failed: " ^ Error.to_string error);
        interaction = Idle;
      }
  | Ok buffer ->
      let session =
        show_new_buffer session buffer |> synchronize_workspace_documents
      in
      { session with message = Some message }

let new_buffer session =
  new_buffer_with_contents session ~contents:""
    ~message:"workspace: created unnamed buffer"

let show_buffer_in_focused_pane session buffer_id =
  let session = capture_focused_view_position session in
  let session = set_pane_buffer session session.focused_pane buffer_id in
  let session = activate_buffer session buffer_id in
  let session = restore_pane_view_position session session.focused_pane in
  {
    session with
    message = Some ("workspace: switched to buffer " ^ string_of_int buffer_id);
  }

let cycle_buffer session direction =
  let buffers = buffer_ids session |> List.sort_uniq Int.compare in
  match buffers with
  | [] -> session
  | _ ->
      let current = focused_buffer session in
      let index =
        List.find_index (fun id -> id = current) buffers
        |> Option.value ~default:0
      in
      let length = List.length buffers in
      let index = (index + direction + length) mod length in
      show_buffer_in_focused_pane session (List.nth buffers index)

let switch_buffer session ~buffer_id =
  match buffer_for_id session buffer_id with
  | Some _ -> show_buffer_in_focused_pane session buffer_id
  | None ->
      {
        session with
        interaction = Idle;
        message =
          Some (Printf.sprintf "workspace: buffer %d is not open" buffer_id);
        inspector = None;
        quit_armed = false;
      }

let valid_buffer_name name =
  let name = String.trim name in
  if String.length name = 0 then
    Error (Error.Invalid_command_arguments "buffer name must not be empty")
  else if String.length name > 120 then
    Error
      (Error.Invalid_command_arguments
         "buffer name exceeds the 120-byte display limit")
  else if
    String.exists
      (fun character ->
        let code = Char.code character in
        code < 32 || code = 127)
      name
  then
    Error
      (Error.Invalid_command_arguments
         "buffer name must not contain control characters")
  else
    match Text_buffer.of_utf8 name with
    | Error _ ->
        Error (Error.Invalid_command_arguments "buffer name must be UTF-8")
    | Ok _ -> Ok name

let rename_buffer session ~name =
  match valid_buffer_name name with
  | Error error ->
      {
        session with
        interaction = Idle;
        message = Some (Error.to_string error);
        inspector = None;
        quit_armed = false;
      }
  | Ok buffer_name ->
      {
        session with
        buffer_name = Some buffer_name;
        interaction = Idle;
        message = Some ("workspace: renamed buffer to " ^ buffer_name);
        inspector = None;
        quit_armed = false;
      }

let buffer_lines session =
  let buffers =
    current_buffer session :: session.inactive_buffers
    |> List.sort (fun (left : buffer) right -> Int.compare left.id right.id)
  in
  "Buffers"
  :: List.map
       (fun (buffer : buffer) ->
         Printf.sprintf "%d: %s%s%s" buffer.id
           (buffer_label ~buffer_name:buffer.buffer_name
              ~file_path:buffer.file_path)
           (if buffer.id = session.current_buffer_id then " (current)" else "")
           (if buffer_dirty buffer then " [+]" else ""))
       buffers

let finish_buffer_close session ~(closing : buffer) ~replacement_id ~message =
  Option.iter Lsp.close closing.language_client;
  Option.iter
    (fun path ->
      let still_open =
        current_buffer session :: session.inactive_buffers
        |> List.exists (fun (buffer : buffer) ->
            buffer.id <> closing.id && buffer.file_path = Some path)
      in
      if not still_open then File_watcher.unwatch session.file_watcher ~path)
    closing.file_path;
  {
    session with
    inactive_buffers =
      List.filter
        (fun (buffer : buffer) -> buffer.id <> closing.id)
        session.inactive_buffers;
    pane_buffers =
      List.map
        (fun (pane, buffer_id) ->
          if buffer_id = closing.id then (pane, replacement_id)
          else (pane, buffer_id))
        session.pane_buffers;
    pane_view_positions =
      List.filter
        (fun ((_, buffer_id), _) -> buffer_id <> closing.id)
        session.pane_view_positions;
    interaction = Idle;
    message = Some message;
    inspector = None;
    quit_armed = false;
  }

let close_buffer ?(force = false) session =
  let closing = current_buffer session in
  if current_dirty session && not force then
    {
      session with
      interaction = Idle;
      message =
        Some
          "workspace: buffer has unsaved changes; use \
           workspace.buffer.force-close to discard them";
      inspector = None;
      quit_armed = false;
    }
  else
    match session.inactive_buffers with
    | replacement :: _ ->
        let session = load_buffer session replacement in
        finish_buffer_close session ~closing ~replacement_id:replacement.id
          ~message:(Printf.sprintf "workspace: closed buffer %d" closing.id)
    | [] -> (
        match
          create_buffer session ~id:session.next_buffer_id ~contents:"" ()
        with
        | Error error ->
            {
              session with
              interaction = Idle;
              message =
                Some
                  ("workspace: closing final buffer failed: "
                 ^ Error.to_string error);
              inspector = None;
              quit_armed = false;
            }
        | Ok replacement ->
            let session =
              show_new_buffer session replacement
              |> synchronize_workspace_documents
            in
            finish_buffer_close session ~closing ~replacement_id:replacement.id
              ~message:
                (Printf.sprintf
                   "workspace: closed buffer %d; created unnamed buffer %d"
                   closing.id replacement.id))

let open_buffer session path =
  if String.length path = 0 then
    { session with message = Some "workspace: file path is empty" }
  else
    match
      List.find_opt
        (fun (buffer : buffer) -> buffer.file_path = Some path)
        (current_buffer session :: session.inactive_buffers)
    with
    | Some buffer -> show_buffer_in_focused_pane session buffer.id
    | None -> (
        match File_io.read_snapshot path with
        | Error error ->
            {
              session with
              interaction = Idle;
              message = Some (File_io.to_string error);
            }
        | Ok (contents, saved_snapshot) -> (
            match
              create_buffer session ~id:session.next_buffer_id ~file_path:path
                ~saved_snapshot ~contents ()
            with
            | Error error ->
                {
                  session with
                  interaction = Idle;
                  message =
                    Some
                      ("workspace: opening buffer failed: "
                     ^ Error.to_string error);
                }
            | Ok buffer ->
                let session =
                  show_new_buffer session buffer
                  |> synchronize_workspace_documents
                in
                { session with message = Some ("workspace: opened " ^ path) }))

let request_workspace session = function
  | Model_effect.Split_view_vertical ->
      {
        (split_pane session Layout.Vertical) with
        interaction = Idle;
        quit_armed = false;
      }
  | Model_effect.Split_view_horizontal ->
      {
        (split_pane session Layout.Horizontal) with
        interaction = Idle;
        quit_armed = false;
      }
  | Model_effect.Focus_next_view ->
      { (focus_next_pane session) with interaction = Idle; quit_armed = false }
  | Model_effect.Close_view ->
      { (close_pane session) with interaction = Idle; quit_armed = false }
  | Model_effect.Keep_only_view ->
      { (only_pane session) with interaction = Idle; quit_armed = false }
  | Model_effect.Resize_view_width delta ->
      {
        (resize_focused_pane session ~dimension:Layout.Width ~delta) with
        interaction = Idle;
        quit_armed = false;
      }
  | Model_effect.Resize_view_height delta ->
      {
        (resize_focused_pane session ~dimension:Layout.Height ~delta) with
        interaction = Idle;
        quit_armed = false;
      }
  | Model_effect.Balance_views ->
      { (balance_panes session) with interaction = Idle; quit_armed = false }
  | Model_effect.New_buffer ->
      { (new_buffer session) with interaction = Idle; quit_armed = false }
  | Model_effect.Open_buffer ->
      {
        session with
        interaction = Open_buffer_prompt "";
        message = Some "workspace: enter a file path";
        inspector = None;
        quit_armed = false;
      }
  | Model_effect.Close_buffer -> close_buffer session
  | Model_effect.Next_buffer ->
      { (cycle_buffer session 1) with interaction = Idle; quit_armed = false }
  | Model_effect.Previous_buffer ->
      {
        (cycle_buffer session (-1)) with
        interaction = Idle;
        quit_armed = false;
      }

let workspace_request_of_binding = function
  | "workspace.split.vertical" -> Some Model_effect.Split_view_vertical
  | "workspace.split.horizontal" -> Some Model_effect.Split_view_horizontal
  | "workspace.pane.next" -> Some Model_effect.Focus_next_view
  | "workspace.pane.close" -> Some Model_effect.Close_view
  | "workspace.pane.only" -> Some Model_effect.Keep_only_view
  | "workspace.pane.grow-width" -> Some (Model_effect.Resize_view_width 1)
  | "workspace.pane.shrink-width" -> Some (Model_effect.Resize_view_width (-1))
  | "workspace.pane.grow-height" -> Some (Model_effect.Resize_view_height 1)
  | "workspace.pane.shrink-height" ->
      Some (Model_effect.Resize_view_height (-1))
  | "workspace.panes.balance" -> Some Model_effect.Balance_views
  | "workspace.buffer.new" -> Some Model_effect.New_buffer
  | "workspace.buffer.open" -> Some Model_effect.Open_buffer
  | "workspace.buffer.close" -> Some Model_effect.Close_buffer
  | "workspace.buffer.next" -> Some Model_effect.Next_buffer
  | "workspace.buffer.previous" -> Some Model_effect.Previous_buffer
  | _ -> None

let source_rows_for_pane session rectangle =
  let status_rows =
    match Zenbu_view.Presentation.status_line session.presentation with
    | Zenbu_view.Presentation.Hidden_status -> 0
    | Zenbu_view.Presentation.Detailed | Zenbu_view.Presentation.Minimal -> 1
  in
  max 0 (rectangle.Layout.height - status_rows)

let settle_viewport session pane viewport =
  {
    (set_pane_viewport session pane viewport) with
    mouse_drag = None;
    interaction = Idle;
    inspector = None;
    message = None;
    quit_armed = false;
  }

let scroll_pane session pane ~lines =
  let session = focus_pane session pane in
  match pane_rectangle session pane with
  | None -> session
  | Some rectangle ->
      let source_rows = source_rows_for_pane session rectangle in
      if source_rows = 0 then session
      else
        let contents = Editor_context.contents (context session) in
        let line_count =
          List.length (Zenbu_view.Display.source_lines contents)
        in
        let maximum_top_line = max 0 (line_count - source_rows) in
        let lines = min maximum_top_line (max (-maximum_top_line) lines) in
        let viewport =
          Zenbu_view.Viewport.scroll
            (pane_viewport session pane)
            ~lines ~maximum_top_line
        in
        settle_viewport session pane viewport

let scroll_pane_pages session pane ~pages =
  let session = focus_pane session pane in
  match pane_rectangle session pane with
  | None -> session
  | Some rectangle ->
      let source_rows = source_rows_for_pane session rectangle in
      if source_rows = 0 then session
      else
        let contents = Editor_context.contents (context session) in
        let line_count =
          List.length (Zenbu_view.Display.source_lines contents)
        in
        let maximum_top_line = max 0 (line_count - source_rows) in
        let lines =
          if pages >= 0 then
            if pages > maximum_top_line / source_rows then maximum_top_line
            else pages * source_rows
          else if pages < -maximum_top_line / source_rows then -maximum_top_line
          else pages * source_rows
        in
        let viewport =
          Zenbu_view.Viewport.scroll
            (pane_viewport session pane)
            ~lines ~maximum_top_line
        in
        settle_viewport session pane viewport

let center_pane_viewport session pane =
  let session = focus_pane session pane in
  match pane_rectangle session pane with
  | None -> session
  | Some rectangle ->
      let source_rows = source_rows_for_pane session rectangle in
      if source_rows = 0 then session
      else
        let contents = Editor_context.contents (context session) in
        let source_lines = Zenbu_view.Display.source_lines contents in
        let line_count = List.length source_lines in
        let maximum_top_line = max 0 (line_count - source_rows) in
        let primary_line =
          Zenbu_view.Display.source_line_at source_lines
            (primary_offset session)
        in
        let viewport =
          {
            (pane_viewport session pane) with
            top_line =
              min maximum_top_line
                (max 0 (primary_line.number - (source_rows / 2)));
            follow_cursor = false;
          }
        in
        settle_viewport session pane viewport

let request_viewport session = function
  | Model_effect.Scroll_view_lines lines ->
      scroll_pane session session.focused_pane ~lines
  | Model_effect.Scroll_view_pages pages ->
      scroll_pane_pages session session.focused_pane ~pages
  | Model_effect.Center_view ->
      center_pane_viewport session session.focused_pane

let viewport_request_of_binding = function
  | "view.scroll.up" -> Some (Model_effect.Scroll_view_lines (-1))
  | "view.scroll.down" -> Some (Model_effect.Scroll_view_lines 1)
  | "view.page.up" -> Some (Model_effect.Scroll_view_pages (-1))
  | "view.page.down" -> Some (Model_effect.Scroll_view_pages 1)
  | "view.center" -> Some Model_effect.Center_view
  | _ -> None

let search_kind_name = function Literal -> "literal" | Regexp -> "regexp"

let begin_search ?(kind = Literal)
    ?(direction : Model_effect.search_direction = Model_effect.Forward) session
    =
  {
    session with
    interaction =
      Search_prompt
        {
          kind;
          query = "";
          origin = Editor_context.selections (context session);
          direction;
        };
    search = None;
    message =
      Some
        (match kind with
        | Literal -> "search: enter a literal Unicode query"
        | Regexp -> "search: enter a UTF-8-safe Str regexp query");
    inspector = None;
  }

let request_bound_host_action session command =
  if String.equal command "editor.command-palette" then
    Some
      {
        session with
        interaction = Palette { query = ""; selected = 0 };
        message = Some "command palette: filter active commands";
        inspector = None;
        quit_armed = false;
      }
  else if String.equal command "search.start" then Some (begin_search session)
  else if String.equal command "search.regexp" then
    Some (begin_search ~kind:Regexp session)
  else
    match workspace_request_of_binding command with
    | Some request -> Some (request_workspace session request)
    | None ->
        Option.map (request_viewport session)
          (viewport_request_of_binding command)

let reload_config session =
  let trace = trace_of_active session.active in
  let profiler = profiler_of_active session.active in
  let execution_id =
    Option.value ~default:0 (last_execution_of_active session.active)
  in
  lifecycle trace ~execution_id ~phase:"reload" ~outcome:"started" ();
  match
    Profiler.measure profiler Profiler.Script_reload (fun () ->
        Scripting.load ~generation_id:session.next_generation_id
          ~base_commands:session.base_commands
          ~base_semantics:session.base_semantics session.config)
  with
  | Error error ->
      lifecycle trace ~execution_id ~phase:"reload" ~outcome:"failed"
        ~reason:(Error.to_string error) ();
      {
        session with
        message = Some ("configuration reload failed: " ^ Error.to_string error);
        last_reload_error = Some error;
        quit_armed = false;
      }
  | Ok generation -> (
      match commands_with_generation session.base_commands generation with
      | Error error ->
          lifecycle trace ~execution_id ~phase:"reload" ?generation
            ~outcome:"failed" ~reason:(Error.to_string error) ();
          {
            session with
            message =
              Some ("configuration reload failed: " ^ Error.to_string error);
            last_reload_error = Some error;
            quit_armed = false;
          }
      | Ok configured_commands -> (
          let script_model = Option.bind generation Scripting.model in
          let script_runtime_required =
            script_runtime_active session.active
            || List.exists
                 (fun (buffer : buffer) -> script_runtime_active buffer.active)
                 session.inactive_buffers
          in
          if script_runtime_required && Option.is_none script_model then (
            Option.iter Scripting.dispose generation;
            let error =
              Error.Invalid_command_arguments
                "configuration reload removed the active script editing model"
            in
            lifecycle trace ~execution_id ~phase:"reload" ?generation
              ~outcome:"failed" ~reason:(Error.to_string error) ();
            {
              session with
              message =
                Some ("configuration reload failed: " ^ Error.to_string error);
              last_reload_error = Some error;
              quit_armed = false;
            })
          else
            let migrate_active active =
              match (active, script_model) with
              | Script_runtime runtime, Some replacement ->
                  Scripting.migrate_model_state
                    ~previous:(Script_runtime.model_state runtime)
                    ~replacement
              | Script_runtime _, None -> assert false
              | ( ( Vim_runtime _ | Selection_runtime _ | Direct_runtime _
                  | Structural_runtime _ ),
                  _ ) ->
                  Ok None
            in
            let migration =
              Result.bind (migrate_active session.active) (fun active_state ->
                  let rec inactive states (buffers : buffer list) =
                    match buffers with
                    | [] -> Ok (active_state, List.rev states)
                    | (buffer : buffer) :: rest ->
                        Result.bind (migrate_active buffer.active) (fun state ->
                            inactive (state :: states) rest)
                  in
                  inactive [] session.inactive_buffers)
            in
            match migration with
            | Error error ->
                Option.iter Scripting.dispose generation;
                lifecycle trace ~execution_id ~phase:"reload" ?generation
                  ~outcome:"failed" ~reason:(Error.to_string error) ();
                {
                  session with
                  message =
                    Some
                      ("configuration reload failed: " ^ Error.to_string error);
                  last_reload_error = Some error;
                  quit_armed = false;
                }
            | Ok (active_state, inactive_states) ->
                let configured_semantics =
                  session.base_semantics
                  @
                  match generation with
                  | None -> []
                  | Some generation -> Scripting.descriptors generation
                in
                let plugin_host =
                  Profiler.measure profiler Profiler.Extension_reload (fun () ->
                      Plugins.reload session.plugins
                        ~base_commands:configured_commands
                        ~base_semantics:configured_semantics
                        ?base_bindings:
                          (match generation with
                          | None -> None
                          | Some generation ->
                              Some (Scripting.bindings generation))
                        ?base_binding_layers:
                          (match generation with
                          | None -> None
                          | Some generation ->
                              Some (Scripting.binding_layers generation))
                        ())
                in
                trace_plugins trace ~execution_id ~phase:"reload" plugin_host;
                trace_runtime_events trace profiler ~execution_id plugin_host;
                let commands =
                  match
                    commands_with_plugins configured_commands plugin_host
                  with
                  | Ok commands -> commands
                  | Error error ->
                      failwith
                        ("plugin snapshot invariant violated: "
                       ^ Error.to_string error)
                in
                let semantic_behaviors =
                  semantic_behaviors_with_plugins generation plugin_host
                in
                Option.iter Script_model.configure script_model;
                let refresh_active active migrated_state =
                  match active with
                  | Script_runtime runtime -> (
                      match script_model with
                      | Some _ ->
                          Script_runtime.create_from_shared
                            (Script_runtime.shared_state runtime)
                          |> Result.map (fun runtime ->
                              Option.value ~default:runtime
                                (Option.map
                                   (Script_runtime.with_model_state runtime)
                                   migrated_state)
                              |> fun runtime -> Script_runtime runtime)
                      | None -> assert false)
                  | active -> Ok active
                in
                let active =
                  match refresh_active session.active active_state with
                  | Ok active ->
                      active_with_extensions active ~commands
                        ~semantic_behaviors
                  | Error error ->
                      failwith
                        ("script model reload invariant violated: "
                       ^ Error.to_string error)
                in
                let inactive_buffers =
                  List.map2
                    (fun (buffer : buffer) migrated_state ->
                      let active =
                        match refresh_active buffer.active migrated_state with
                        | Ok active -> active
                        | Error error ->
                            failwith
                              ("script model reload invariant violated: "
                             ^ Error.to_string error)
                      in
                      {
                        buffer with
                        active =
                          active_with_extensions active ~commands
                            ~semantic_behaviors;
                      })
                    session.inactive_buffers inactive_states
                in
                Option.iter Scripting.dispose session.generation;
                lifecycle trace ~execution_id ~phase:"reload" ?generation
                  ~outcome:"succeeded" ();
                let valid_active_modes modes =
                  match generation with
                  | Some generation
                    when List.for_all
                           (fun id ->
                             List.exists
                               (fun mode ->
                                 String.equal (Scripting.mode_id mode) id)
                               (Scripting.modes generation))
                           modes ->
                      modes
                  | None | Some _ -> []
                in
                let active_modes = valid_active_modes session.active_modes in
                let layer_catalog =
                  binding_layer_catalog generation plugin_host
                in
                let bindings = binding_catalog generation plugin_host in
                let active_binding_layers, dropped_active_layers =
                  revalidate_active_binding_layers ~catalog:layer_catalog
                    ~bindings session.active_binding_layers
                in
                let inactive_binding_layer_drops = ref [] in
                let inactive_buffers =
                  List.map
                    (fun (buffer : buffer) ->
                      let active_binding_layers, dropped_layers =
                        revalidate_active_binding_layers ~catalog:layer_catalog
                          ~bindings buffer.active_binding_layers
                      in
                      inactive_binding_layer_drops :=
                        dropped_layers @ !inactive_binding_layer_drops;
                      {
                        buffer with
                        active_modes = valid_active_modes buffer.active_modes;
                        active_binding_layers;
                      })
                    inactive_buffers
                in
                let message =
                  let plugin_count =
                    List.length (Plugins.providers plugin_host)
                  in
                  match generation with
                  | None ->
                      Printf.sprintf
                        "configuration reloaded: no active script generation; \
                         %d active plugins"
                        plugin_count
                  | Some generation ->
                      let commands, selectors, transformations, bindings, hooks
                          =
                        Scripting.counts generation
                      in
                      Printf.sprintf
                        "configuration reloaded: %d commands, %d selectors, %d \
                         transformations, %d bindings, %d hooks, %d modes"
                        commands selectors transformations bindings hooks
                        (List.length (Scripting.modes generation))
                      ^ Printf.sprintf "; %d active plugins" plugin_count
                in
                let dropped_layer_count =
                  List.length dropped_active_layers
                  + List.length !inactive_binding_layer_drops
                in
                let message =
                  if dropped_layer_count = 0 then message
                  else
                    message
                    ^ Printf.sprintf
                        "; disabled %d stale or conflicting binding layer%s"
                        dropped_layer_count
                        (if dropped_layer_count = 1 then "" else "s")
                in
                {
                  session with
                  active;
                  generation;
                  plugins = plugin_host;
                  next_generation_id = session.next_generation_id + 1;
                  last_reload_error = None;
                  inactive_buffers;
                  message = Some message;
                  quit_armed = false;
                  pending_binding = [];
                  active_modes;
                  active_binding_layers;
                }))

let model_descriptor = function
  | Vim_runtime runtime -> Vim_runtime.model_descriptor runtime
  | Selection_runtime runtime -> Selection_runtime.model_descriptor runtime
  | Direct_runtime runtime -> Direct_runtime.model_descriptor runtime
  | Structural_runtime runtime -> Structural_runtime.model_descriptor runtime
  | Script_runtime runtime -> Script_runtime.model_descriptor runtime

let binding_rank session binding =
  let model = model_descriptor session.active |> Editing_model.id in
  let status = active_status session.active |> Model_status.id in
  let layer_priority =
    match Scripting.binding_layer binding with
    | None -> Some 0
    | Some _ ->
        List.find_opt
          (fun layer -> binding_belongs_to_layer layer binding)
          session.active_binding_layers
        |> Option.map Scripting.binding_layer_priority
  in
  Option.bind layer_priority (fun layer_priority ->
      match Scripting.binding_scope binding with
      | Scripting.Global -> Some (0, layer_priority)
      | Scripting.Model candidate when String.equal candidate model ->
          Some (1, layer_priority)
      | Scripting.Model_status { model = candidate; status = candidate_status }
        when String.equal candidate model
             && String.equal candidate_status status ->
          Some (2, layer_priority)
      | Scripting.Mode candidate ->
          let rec mode_rank rank = function
            | [] -> None
            | mode :: rest ->
                if String.equal candidate mode then Some (rank, layer_priority)
                else mode_rank (rank - 1) rest
          in
          mode_rank (3 + List.length session.active_modes) session.active_modes
      | Scripting.Model _ | Scripting.Model_status _ -> None)

let rec binding_sequence_has_prefix events patterns =
  match (events, patterns) with
  | [], _ -> true
  | _, [] -> false
  | event :: event_rest, pattern :: pattern_rest ->
      Input_event.binding_pattern_matches pattern event
      && binding_sequence_has_prefix event_rest pattern_rest

let binding_text_input binding events =
  List.combine (Scripting.binding_inputs binding) events
  |> List.find_map (function
    | Input_event.Any_text_input, Input_event.Text_input text -> Some text
    | Input_event.Exact_event _, _ | Input_event.Any_text_input, _ -> None)

let binding_event_is_escape = function
  | Input_event.Key_press { key = Input_event.Named_key Input_event.Escape; _ }
    ->
      true
  | Input_event.Key_press _ | Input_event.Text_input _ | Input_event.Mouse _ ->
      false

let active_bindings session = binding_catalog session.generation session.plugins

let pop_active_mode session =
  match session.active_modes with
  | [] -> session
  | _ :: active_modes ->
      {
        session with
        active_modes;
        message =
          Some
            (if active_modes = [] then "custom modes exited"
             else "custom mode exited; resumed: " ^ List.hd active_modes);
      }

let transition_binding_mode session binding =
  match Scripting.binding_mode_transition binding with
  | None -> session
  | Some Scripting.Clear_modes ->
      { session with active_modes = []; message = Some "custom modes exited" }
  | Some Scripting.Pop_mode -> pop_active_mode session
  | Some (Scripting.Replace_mode mode) ->
      {
        session with
        active_modes = [ mode ];
        message = Some ("custom mode entered: " ^ mode);
      }
  | Some (Scripting.Push_mode mode) ->
      {
        session with
        active_modes = mode :: session.active_modes;
        message = Some ("custom mode pushed: " ^ mode);
      }

let matching_binding session input =
  let sequence = session.pending_binding @ [ input ] in
  let bindings =
    active_bindings session
    |> List.filter_map (fun binding ->
        Option.bind (binding_rank session binding) (fun rank ->
            if
              binding_sequence_has_prefix sequence
                (Scripting.binding_inputs binding)
            then Some (rank, binding)
            else None))
  in
  match bindings with
  | [] ->
      if session.pending_binding = [] && session.active_modes = [] then
        No_binding
      else if binding_event_is_escape input then Binding_cancelled
      else Binding_rejected sequence
  | _ -> (
      let highest_rank candidates =
        List.fold_left
          (fun maximum (rank, _) ->
            if compare rank maximum > 0 then rank else maximum)
          (min_int, min_int) candidates
      in
      let completed =
        List.filter
          (fun (_, binding) ->
            List.length (Scripting.binding_inputs binding)
            = List.length sequence)
          bindings
      in
      match completed with
      | [] -> Binding_prefix sequence
      | _ -> (
          let completed_rank = highest_rank completed in
          let has_more_specific_prefix =
            List.exists
              (fun (rank, binding) ->
                compare rank completed_rank > 0
                && List.length (Scripting.binding_inputs binding)
                   > List.length sequence)
              bindings
          in
          if has_more_specific_prefix then Binding_prefix sequence
          else
            match
              List.filter (fun (rank, _) -> rank = completed_rank) completed
            with
            | [ (_, binding) ] ->
                Binding_resolved (binding, binding_text_input binding sequence)
            | _ -> Binding_rejected sequence))

let find_binding_layer session id =
  binding_layer_catalog session.generation session.plugins
  |> List.find_opt (fun layer ->
      String.equal (Scripting.binding_layer_id layer) id)

let enable_binding_layer session ~id =
  let id = String.trim id in
  match Command_id.of_string id with
  | Error error ->
      {
        session with
        message = Some (Error.to_string error);
        inspector = None;
        quit_armed = false;
      }
  | Ok _ -> (
      match find_binding_layer session id with
      | None ->
          {
            session with
            message = Some ("binding layer is not declared: " ^ id);
            inspector = None;
            quit_armed = false;
          }
      | Some layer
        when List.exists
               (fun active -> same_binding_layer active layer)
               session.active_binding_layers ->
          {
            session with
            pending_binding = [];
            message = Some ("binding layer is already enabled: " ^ id);
            inspector = None;
            quit_armed = false;
          }
      | Some _
        when List.length session.active_binding_layers
             >= maximum_active_binding_layers ->
          {
            session with
            message =
              Some
                (Printf.sprintf "binding layer limit reached (%d active layers)"
                   maximum_active_binding_layers);
            inspector = None;
            quit_armed = false;
          }
      | Some layer ->
          let candidate = session.active_binding_layers @ [ layer ] in
          if binding_layers_conflict (active_bindings session) candidate then
            {
              session with
              message =
                Some
                  (Printf.sprintf
                     "binding layer enable rejected: %s has an equal-priority \
                      overlapping binding"
                     id);
              inspector = None;
              quit_armed = false;
            }
          else
            {
              session with
              active_binding_layers = candidate;
              pending_binding = [];
              message =
                Some
                  (Printf.sprintf "binding layer enabled: %s (priority %d)" id
                     (Scripting.binding_layer_priority layer));
              inspector = None;
              quit_armed = false;
            })

let disable_binding_layer session ~id =
  let id = String.trim id in
  match
    List.partition
      (fun layer -> String.equal (Scripting.binding_layer_id layer) id)
      session.active_binding_layers
  with
  | [], _ ->
      {
        session with
        message = Some ("binding layer is not enabled: " ^ id);
        inspector = None;
        quit_armed = false;
      }
  | _, active_binding_layers ->
      {
        session with
        active_binding_layers;
        pending_binding = [];
        message = Some ("binding layer disabled: " ^ id);
        inspector = None;
        quit_armed = false;
      }

let rebase_location (location : location) history =
  if location.stale then location
  else
    let current_version =
      History.current history |> Document.version |> Document_version.to_int
    in
    if location.document_version = current_version then location
    else
      let rec replay version selections = function
        | [] -> if version = current_version then Some selections else None
        | change :: rest ->
            let before_version =
              History.before change |> Document.version
              |> Document_version.to_int
            in
            if before_version < version then replay version selections rest
            else if before_version > version then None
            else
              let transaction = History.transaction change in
              let edits = Transaction.edits transaction in
              let selections =
                List.map
                  (fun (anchor_offset, head_offset) ->
                    ( Document.transform_offset edits anchor_offset,
                      Document.transform_offset edits head_offset ))
                  selections
              in
              let version =
                History.after change |> Document.version
                |> Document_version.to_int
              in
              replay version selections rest
      in
      match
        replay location.document_version location.selections
          (History.lineage history)
      with
      | Some selections ->
          { location with document_version = current_version; selections }
      | None -> { location with stale = true }

let refresh_location session (location : location) =
  match buffer_for_id session location.buffer_id with
  | None -> { location with stale = true }
  | Some buffer -> rebase_location location (history_of_active buffer.active)

let refresh_locations session =
  {
    session with
    locations = List.map (refresh_location session) session.locations;
    backward_jumps = List.map (refresh_location session) session.backward_jumps;
    forward_jumps = List.map (refresh_location session) session.forward_jumps;
  }

let capture_location session ~name : location =
  let selections = Editor_context.selections (context session) in
  {
    name;
    buffer_id = session.current_buffer_id;
    document_version = Editor_context.document_version (context session);
    selections =
      List.map
        (fun (selection : Editor_context.selection) ->
          (selection.anchor_offset, selection.head_offset))
        selections.selections;
    primary = selections.primary_index;
    stale = false;
  }

let same_location_position (left : location) (right : location) =
  left.buffer_id = right.buffer_id
  && left.selections = right.selections
  && left.primary = right.primary

let take_jump_entries values =
  let rec take remaining result = function
    | _ when remaining = 0 -> List.rev result
    | [] -> List.rev result
    | value :: rest -> take (remaining - 1) (value :: result) rest
  in
  take maximum_jump_entries [] values

let push_current_jump session =
  let location = capture_location session ~name:"<jump>" in
  {
    session with
    backward_jumps = take_jump_entries (location :: session.backward_jumps);
    forward_jumps = [];
    message = Some "jump history entry added";
    quit_armed = false;
  }

let set_location session name =
  match validate_location_name name with
  | Error error -> { session with message = Some (Error.to_string error) }
  | Ok name ->
      let existing =
        List.exists (fun location -> location.name = name) session.locations
      in
      if (not existing) && List.length session.locations >= maximum_locations
      then
        {
          session with
          message = Some "location store is full; replace an existing location";
        }
      else
        let location = capture_location session ~name in
        {
          session with
          locations =
            location
            :: List.filter (fun item -> item.name <> name) session.locations;
          message = Some ("location set: " ^ name);
          quit_armed = false;
        }

let transaction_edits transaction =
  Transaction.edits transaction
  |> List.map (fun edit ->
      let range = Edit.range edit in
      {
        Language.start_offset = Anchor.byte_offset (Range.start range);
        stop_offset = Anchor.byte_offset (Range.stop range);
        replacement = Edit.text edit;
      })

let synchronize_language_after_change session ~fallback_contents =
  match session.language_client with
  | None -> session
  | Some client ->
      Lsp.set_execution_id client
        ~execution_id:
          (Option.value ~default:0 (last_execution_of_active session.active));
      let history = history_of_active session.active in
      let source_contents, edits =
        match History.current_change history with
        | Some change ->
            let transaction = History.transaction change in
            ( Document.snapshot (History.before change)
              |> Document_snapshot.contents,
              transaction_edits transaction )
        | None ->
            ( fallback_contents,
              [
                {
                  Language.start_offset = 0;
                  stop_offset = String.length fallback_contents;
                  replacement = Editor_context.contents (context session);
                };
              ] )
      in
      Lsp.notify_change client ~source_contents
        ~contents:(Editor_context.contents (context session))
        ~document_version:(Editor_context.document_version (context session))
        ~edits;
      { session with diagnostics = [] }

let execute_active_effects ?augment_provenance session input effects =
  let result =
    match session.active with
    | Vim_runtime runtime -> (
        match
          Vim_runtime.execute_effects runtime ?augment_provenance ~input effects
        with
        | Error error ->
            ( {
                session with
                message = Some (Error.to_string error);
                quit_armed = false;
              },
              false )
        | Ok (runtime, step) ->
            ( {
                session with
                active = Vim_runtime runtime;
                message = last_message (Vim_runtime.messages step);
                quit_armed = false;
                inspector = None;
              },
              Vim_runtime.change_ids step <> [] ))
    | Selection_runtime runtime -> (
        match
          Selection_runtime.execute_effects runtime ?augment_provenance ~input
            effects
        with
        | Error error ->
            ( {
                session with
                message = Some (Error.to_string error);
                quit_armed = false;
              },
              false )
        | Ok (runtime, step) ->
            ( {
                session with
                active = Selection_runtime runtime;
                message = last_message (Selection_runtime.messages step);
                quit_armed = false;
                inspector = None;
              },
              Selection_runtime.change_ids step <> [] ))
    | Direct_runtime runtime -> (
        match
          Direct_runtime.execute_effects runtime ?augment_provenance ~input
            effects
        with
        | Error error ->
            ( {
                session with
                message = Some (Error.to_string error);
                quit_armed = false;
              },
              false )
        | Ok (runtime, step) ->
            ( {
                session with
                active = Direct_runtime runtime;
                message = last_message (Direct_runtime.messages step);
                quit_armed = false;
                inspector = None;
              },
              Direct_runtime.change_ids step <> [] ))
    | Structural_runtime runtime -> (
        match
          Structural_runtime.execute_effects runtime ?augment_provenance ~input
            effects
        with
        | Error error ->
            ( {
                session with
                message = Some (Error.to_string error);
                quit_armed = false;
              },
              false )
        | Ok (runtime, step) ->
            ( {
                session with
                active = Structural_runtime runtime;
                message = last_message (Structural_runtime.messages step);
                quit_armed = false;
                inspector = None;
              },
              Structural_runtime.change_ids step <> [] ))
    | Script_runtime runtime -> (
        match
          Script_runtime.execute_effects runtime ?augment_provenance ~input
            effects
        with
        | Error error ->
            ( {
                session with
                message = Some (Error.to_string error);
                quit_armed = false;
              },
              false )
        | Ok (runtime, step) ->
            ( {
                session with
                active = Script_runtime runtime;
                message = last_message (Script_runtime.messages step);
                quit_armed = false;
                inspector = None;
              },
              Script_runtime.change_ids step <> [] ))
  in
  let next, changed = result in
  (observe_language_document_version next, changed)

let apply_kill_ring_effect session input ~effect_id runtime_effect =
  let next, _ =
    execute_active_effects
      ~augment_provenance:(fun provenance ->
        Provenance.add provenance (Provenance.Effect effect_id))
      session input [ runtime_effect ]
  in
  let next = synchronize_kill_ring_from_active next in
  { next with interaction = Idle; inspector = None; quit_armed = false }

let cut_to_kill_ring session input =
  apply_kill_ring_effect session input ~effect_id:"host.kill-ring.cut"
    (Model_effect.Cut_to_clipboard
       {
         slot = Clipboard.unnamed;
         selector = Model_intent.Current_selections;
         kind = Clipboard.Characterwise;
       })

let yank_latest_kill session input =
  apply_kill_ring_effect session input ~effect_id:"host.kill-ring.yank"
    (Model_effect.Paste_from_kill_ring
       { index = 0; placement = Clipboard.Replace })

let selected_contents_for_system_clipboard session =
  let context = context session in
  let contents = Editor_context.contents context in
  let selected =
    Editor_context.selections context |> fun selections ->
    selections.selections
    |> List.map (fun (selection : Editor_context.selection) ->
        let start = min selection.anchor_offset selection.head_offset in
        let stop = max selection.anchor_offset selection.head_offset in
        String.sub contents start (stop - start))
    |> String.concat ""
  in
  if String.length selected = 0 then
    Error
      (Error.Invalid_command_arguments
         "system clipboard copy requires a non-empty selection")
  else Ok selected

let copy_to_system_clipboard session input =
  match selected_contents_for_system_clipboard session with
  | Error error -> { session with message = Some (Error.to_string error) }
  | Ok contents -> (
      match System_clipboard.write session.system_clipboard contents with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok () ->
          let next, _ =
            execute_active_effects
              ~augment_provenance:(fun provenance ->
                Provenance.add provenance
                  (Provenance.Effect "host.system-clipboard.copy"))
              session input
              [
                Model_effect.Copy_to_clipboard
                  {
                    slot = Clipboard.unnamed;
                    selector = Model_intent.Current_selections;
                    kind = Clipboard.Characterwise;
                  };
              ]
          in
          {
            next with
            interaction = Idle;
            inspector = None;
            quit_armed = false;
            message =
              Some
                ("system clipboard: copied via "
                ^ System_clipboard.name session.system_clipboard);
          })

let paste_from_system_clipboard session input =
  match System_clipboard.read session.system_clipboard with
  | Error error -> { session with message = Some (Error.to_string error) }
  | Ok contents when String.length contents = 0 ->
      { session with message = Some "system clipboard is empty" }
  | Ok contents ->
      let next, _ =
        execute_active_effects
          ~augment_provenance:(fun provenance ->
            Provenance.add provenance
              (Provenance.Effect "host.system-clipboard.paste"))
          session input
          [
            Model_effect.Execute_intent
              (Model_intent.replace_selected_ranges contents);
          ]
      in
      {
        next with
        interaction = Idle;
        inspector = None;
        quit_armed = false;
        message =
          Some
            ("system clipboard: pasted via "
            ^ System_clipboard.name session.system_clipboard);
      }

let restore_location session input (location : location) ~effect_id ~message =
  if location.stale then { session with message = Some "jump target is stale" }
  else
    match buffer_for_id session location.buffer_id with
    | None ->
        { session with message = Some "jump target buffer is unavailable" }
    | Some _ -> (
        let session = capture_focused_view_position session in
        let session =
          set_pane_buffer session session.focused_pane location.buffer_id
        in
        let session = activate_buffer session location.buffer_id in
        match
          Model_intent.set_selections ~selections:location.selections
            ~primary:location.primary
        with
        | Error error -> { session with message = Some (Error.to_string error) }
        | Ok intent ->
            let next, _ =
              execute_active_effects
                ~augment_provenance:(fun provenance ->
                  Provenance.add provenance (Provenance.Effect effect_id))
                session input
                [ Model_effect.Execute_intent intent ]
            in
            {
              next with
              interaction = Idle;
              inspector = None;
              message = Some message;
              quit_armed = false;
            }
            |> fun session -> capture_focused_view_position session)

let push_jump_before_location session (location : location) =
  let current = capture_location session ~name:"<jump>" in
  if same_location_position current location then session
  else
    {
      session with
      backward_jumps = take_jump_entries (current :: session.backward_jumps);
      forward_jumps = [];
    }

let jump_to_location session input name =
  match validate_location_name name with
  | Error error -> { session with message = Some (Error.to_string error) }
  | Ok name -> (
      let session = refresh_locations session in
      match
        List.find_opt
          (fun location -> String.equal location.name name)
          session.locations
      with
      | None -> { session with message = Some ("location not found: " ^ name) }
      | Some { stale = true; _ } ->
          { session with message = Some ("location is stale: " ^ name) }
      | Some location ->
          push_jump_before_location session location |> fun session ->
          restore_location session input location
            ~effect_id:("location.jump:" ^ name)
            ~message:("location jumped: " ^ name))

let rec next_active_jump (values : location list) =
  match values with
  | [] -> None
  | location :: rest when location.stale -> next_active_jump rest
  | location :: rest -> Some (location, rest)

let jump_step session input ~(direction : Model_effect.jump_direction) =
  let session = refresh_locations session in
  let candidates, other =
    match direction with
    | Model_effect.Backward -> (session.backward_jumps, session.forward_jumps)
    | Model_effect.Forward -> (session.forward_jumps, session.backward_jumps)
  in
  match next_active_jump candidates with
  | None ->
      ( {
          session with
          backward_jumps =
            (if direction = Model_effect.Backward then []
             else session.backward_jumps);
          forward_jumps =
            (if direction = Model_effect.Forward then []
             else session.forward_jumps);
          message =
            Some
              (match direction with
              | Model_effect.Backward -> "jump history: no older entry"
              | Model_effect.Forward -> "jump history: no newer entry");
        },
        false )
  | Some (target, remaining) ->
      let current = capture_location session ~name:"<jump>" in
      let session =
        match direction with
        | Model_effect.Backward ->
            {
              session with
              backward_jumps = remaining;
              forward_jumps = take_jump_entries (current :: other);
            }
        | Model_effect.Forward ->
            {
              session with
              backward_jumps = take_jump_entries (current :: other);
              forward_jumps = remaining;
            }
      in
      let effect_id =
        match direction with
        | Model_effect.Backward -> "jump.backward"
        | Model_effect.Forward -> "jump.forward"
      in
      let message =
        match direction with
        | Model_effect.Backward -> "jumped backward"
        | Model_effect.Forward -> "jumped forward"
      in
      (restore_location session input target ~effect_id ~message, true)

let traverse_jumps session input ~(direction : Model_effect.jump_direction)
    ~count =
  let rec traverse remaining session =
    if remaining = 0 then session
    else
      let next, moved = jump_step session input ~direction in
      if moved then traverse (remaining - 1) next else next
  in
  traverse count session

let diagnostics_sorted session =
  List.sort
    (fun (left : Language.diagnostic) (right : Language.diagnostic) ->
      match Int.compare left.Language.start_offset right.start_offset with
      | 0 -> Int.compare left.stop_offset right.stop_offset
      | value -> value)
    session.diagnostics

let move_to_diagnostic session input direction =
  let diagnostics = diagnostics_sorted session in
  match diagnostics with
  | [] -> { session with message = Some "language: no current diagnostics" }
  | _ -> (
      let caret = primary_offset session in
      let candidate =
        match direction with
        | 1 ->
            diagnostics
            |> List.find_opt (fun (diagnostic : Language.diagnostic) ->
                diagnostic.start_offset > caret)
            |> Option.value ~default:(List.hd diagnostics)
        | _ ->
            diagnostics |> List.rev
            |> List.find_opt (fun (diagnostic : Language.diagnostic) ->
                diagnostic.start_offset < caret)
            |> Option.value ~default:(List.hd (List.rev diagnostics))
      in
      match
        Model_intent.set_selections
          ~selections:[ (candidate.start_offset, candidate.stop_offset) ]
          ~primary:0
      with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok intent ->
          let next, _ =
            execute_active_effects
              ~augment_provenance:(fun provenance ->
                Provenance.add provenance
                  (Provenance.Effect "language.diagnostic.navigate"))
              session input
              [ Model_effect.Execute_intent intent ]
          in
          { next with message = Some ("diagnostic: " ^ candidate.message) })

let describe_diagnostic session =
  let caret = primary_offset session in
  match
    diagnostics_sorted session
    |> List.find_opt (fun (diagnostic : Language.diagnostic) ->
        diagnostic.start_offset <= caret && caret <= diagnostic.stop_offset)
  with
  | None -> { session with message = Some "language: no diagnostic at caret" }
  | Some diagnostic ->
      {
        session with
        message =
          Some
            (Language.diagnostic_severity_name diagnostic.severity
            ^ ": " ^ diagnostic.message);
      }

let completion_edits session (item : Language.completion) =
  if item.snippet then Error "completion uses unsupported snippet text"
  else
    let primary = primary_offset session in
    let main =
      match item.text_edit with
      | Some edit -> Some edit
      | None ->
          Option.map
            (fun replacement ->
              {
                Language.start_offset = primary;
                stop_offset = primary;
                replacement;
              })
            item.insert_text
    in
    match main with
    | None -> Error "completion has no supported text edit"
    | Some main -> Ok (main :: item.additional_text_edits)

let apply_language_edits session input ~effect_id ~edits =
  let before = Editor_context.contents (context session) in
  let next, changed =
    execute_active_effects
      ~augment_provenance:(fun provenance ->
        Provenance.add provenance (Provenance.Effect effect_id))
      session input
      [ Language_commands.apply_edits edits ]
  in
  if changed then
    synchronize_language_after_change next ~fallback_contents:before
  else next

let accept_completion session input item =
  match completion_edits session item with
  | Error reason -> { session with interaction = Idle; message = Some reason }
  | Ok edits ->
      let next =
        apply_language_edits session input ~effect_id:"language.complete" ~edits
      in
      {
        next with
        interaction = Idle;
        message = Some ("completed " ^ item.label);
      }

let select_definition session input (target : Language.definition_target)
    message =
  match
    Model_intent.set_selections
      ~selections:[ (target.start_offset, target.stop_offset) ]
      ~primary:0
  with
  | Error error -> { session with message = Some (Error.to_string error) }
  | Ok intent ->
      let next, _ =
        execute_active_effects
          ~augment_provenance:(fun provenance ->
            Provenance.add provenance (Provenance.Effect "language.definition"))
          session input
          [ Model_effect.Execute_intent intent ]
      in
      { next with interaction = Idle; message = Some message }

let apply_definition session input (target : Language.definition_target) =
  match session.file_path with
  | Some path
    when String.equal (Language.Uri.file_of_path path) target.Language.uri ->
      select_definition session input target "definition: same document"
  | None | Some _ -> (
      match Language.Uri.path_of_file target.Language.uri with
      | Error reason ->
          {
            session with
            message = Some ("definition target is not a local file: " ^ reason);
          }
      | Ok path ->
          let next = open_buffer session path in
          if next.file_path <> Some path then next
          else
            select_definition next input target
              ("definition: opened " ^ Filename.basename path))

let execute_effects_in_active ?augment_provenance active input effects =
  match active with
  | Vim_runtime runtime ->
      Vim_runtime.execute_effects runtime ?augment_provenance ~input effects
      |> Result.map (fun (runtime, step) ->
          ( Vim_runtime runtime,
            Vim_runtime.change_ids step <> [],
            last_message (Vim_runtime.messages step) ))
  | Selection_runtime runtime ->
      Selection_runtime.execute_effects runtime ?augment_provenance ~input
        effects
      |> Result.map (fun (runtime, step) ->
          ( Selection_runtime runtime,
            Selection_runtime.change_ids step <> [],
            last_message (Selection_runtime.messages step) ))
  | Direct_runtime runtime ->
      Direct_runtime.execute_effects runtime ?augment_provenance ~input effects
      |> Result.map (fun (runtime, step) ->
          ( Direct_runtime runtime,
            Direct_runtime.change_ids step <> [],
            last_message (Direct_runtime.messages step) ))
  | Structural_runtime runtime ->
      Structural_runtime.execute_effects runtime ?augment_provenance ~input
        effects
      |> Result.map (fun (runtime, step) ->
          ( Structural_runtime runtime,
            Structural_runtime.change_ids step <> [],
            last_message (Structural_runtime.messages step) ))
  | Script_runtime runtime ->
      Script_runtime.execute_effects runtime ?augment_provenance ~input effects
      |> Result.map (fun (runtime, step) ->
          ( Script_runtime runtime,
            Script_runtime.change_ids step <> [],
            last_message (Script_runtime.messages step) ))

let synchronize_buffer_after_change (buffer : buffer) ~fallback_contents =
  match buffer.language_client with
  | None -> { buffer with diagnostics = []; search = None }
  | Some client ->
      Lsp.set_execution_id client
        ~execution_id:
          (Option.value ~default:0 (last_execution_of_active buffer.active));
      let history = history_of_active buffer.active in
      let source_contents, edits =
        match History.current_change history with
        | Some change ->
            let transaction = History.transaction change in
            ( Document.snapshot (History.before change)
              |> Document_snapshot.contents,
              transaction_edits transaction )
        | None ->
            ( fallback_contents,
              [
                {
                  Language.start_offset = 0;
                  stop_offset = String.length fallback_contents;
                  replacement =
                    Editor_context.contents (context_of_active buffer.active);
                };
              ] )
      in
      Lsp.notify_change client ~source_contents
        ~contents:(Editor_context.contents (context_of_active buffer.active))
        ~document_version:
          (Editor_context.document_version (context_of_active buffer.active))
        ~edits;
      { buffer with diagnostics = []; search = None }

let update_current_from_buffer session (buffer : buffer) =
  {
    session with
    active = buffer.active;
    file_path = buffer.file_path;
    buffer_name = buffer.buffer_name;
    language_override = buffer.language_override;
    saved_version = buffer.saved_version;
    saved_contents = buffer.saved_contents;
    saved_snapshot = buffer.saved_snapshot;
    language_client = buffer.language_client;
    diagnostics = buffer.diagnostics;
    presentation_cache = buffer.presentation_cache;
    search = buffer.search;
    active_modes = buffer.active_modes;
    active_binding_layers = buffer.active_binding_layers;
  }

let workspace_edit_targets session edits =
  let buffers = current_buffer session :: session.inactive_buffers in
  let find uri =
    List.find_opt
      (fun (buffer : buffer) ->
        Option.map
          (fun path -> String.equal uri (Language.Uri.file_of_path path))
          buffer.file_path
        |> Option.value ~default:false)
      buffers
  in
  let add targets (edit : Lsp.workspace_edit) =
    match find edit.uri with
    | None -> Error ("workspace edit targets unopened buffer: " ^ edit.uri)
    | Some buffer ->
        let contents =
          Editor_context.contents (context_of_active buffer.active)
        in
        if not (String.equal contents edit.source_contents) then
          Error ("workspace edit source changed for " ^ edit.uri)
        else
          let rec append = function
            | [] -> [ (buffer, edit.edits) ]
            | (existing, edits) :: rest when existing.id = buffer.id ->
                (existing, edits @ edit.edits) :: rest
            | target :: rest -> target :: append rest
          in
          Ok (append targets)
  in
  List.fold_left
    (fun targets edit -> Result.bind targets (fun targets -> add targets edit))
    (Ok []) edits

let apply_workspace_edits session input ~effect_id edits =
  Result.bind (workspace_edit_targets session edits) (fun targets ->
      let apply ((buffer : buffer), edits) =
        let before =
          Editor_context.contents (context_of_active buffer.active)
        in
        execute_effects_in_active
          ~augment_provenance:(fun provenance ->
            Provenance.add provenance (Provenance.Effect effect_id))
          buffer.active input
          [ Language_commands.apply_edits edits ]
        |> Result.map_error Error.to_string
        |> Result.map (fun (active, changed, message) ->
            (buffer, { buffer with active }, changed, message, before))
      in
      let rec stage values = function
        | [] -> Ok (List.rev values)
        | target :: rest ->
            Result.bind (apply target) (fun value ->
                stage (value :: values) rest)
      in
      Result.bind (stage [] targets) (fun staged ->
          let staged =
            List.map
              (fun (before, after, changed, message, contents_before) ->
                let after =
                  if changed then
                    synchronize_buffer_after_change after
                      ~fallback_contents:contents_before
                  else after
                in
                (before, after, changed, message))
              staged
          in
          let replacement buffer =
            staged
            |> List.find_map (fun (before, after, _, _) ->
                if before.id = buffer.id then Some after else None)
            |> Option.value ~default:buffer
          in
          let session =
            update_current_from_buffer session
              (replacement (current_buffer session))
          in
          let session =
            {
              session with
              inactive_buffers = List.map replacement session.inactive_buffers;
              interaction = Idle;
              inspector = None;
              quit_armed = false;
            }
          in
          let session = synchronize_workspace_documents session in
          let changed =
            List.exists (fun (_, _, changed, _) -> changed) staged
          in
          let message =
            staged |> List.find_map (fun (_, _, _, message) -> message)
          in
          Ok
            ( {
                session with
                message =
                  (if changed then Some "workspace edit applied" else message);
              },
              changed )))

let poll_active_language ?(background = false) session =
  let current_version = Editor_context.document_version (context session) in
  let handle session = function
    | Lsp.Initialized ->
        if background then session
        else { session with message = Some "language server ready" }
    | Lsp.Diagnostics { document_version = Some version; diagnostics }
      when version = current_version ->
        { session with diagnostics }
    | Lsp.Diagnostics { document_version = None; diagnostics }
      when current_version = 0 ->
        (* Unversioned diagnostics are safe only for the original didOpen
           snapshot. After an edit, prefer dropping them to displaying stale
           information as current. *)
        { session with diagnostics }
    | Lsp.Diagnostics _ -> session
    | Lsp.Hover_result { document_version; byte_offset; hover; _ }
      when document_version = current_version
           && byte_offset = primary_offset session
           && not background -> (
        match hover with
        | None ->
            { session with message = Some "language: no hover information" }
        | Some hover ->
            { session with interaction = Hover_view hover; message = None })
    | Lsp.Hover_result _ when background -> session
    | Lsp.Hover_result _ ->
        {
          session with
          message = Some "language: stale hover response discarded";
        }
    | Lsp.Definition_result { document_version; byte_offset; targets; _ }
      when document_version = current_version
           && byte_offset = primary_offset session
           && not background -> (
        match targets with
        | [] -> { session with message = Some "language: no definition found" }
        | target :: _ ->
            apply_definition session
              (Input_event.key_press (Input_event.named_key Input_event.Enter))
              target)
    | Lsp.Definition_result _ -> session
    | Lsp.Completion_result { document_version; byte_offset; items; _ }
      when document_version = current_version
           && byte_offset = primary_offset session
           && not background ->
        if items = [] then
          { session with message = Some "language: no completions" }
        else
          {
            session with
            interaction = Completion_view { items; selected = 0; query = "" };
            message = None;
          }
    | Lsp.Completion_result _ -> session
    | Lsp.Rename_result { document_version; edits; _ }
      when document_version = current_version && not background -> (
        let input =
          Input_event.key_press (Input_event.named_key Input_event.Enter)
        in
        match
          apply_workspace_edits session input ~effect_id:"language.rename" edits
        with
        | Error reason ->
            { session with message = Some ("rename rejected: " ^ reason) }
        | Ok (next, _) ->
            { next with interaction = Idle; message = Some "rename applied" })
    | Lsp.Rename_result _ -> session
    | Lsp.Apply_edit { request_id; edits } -> (
        let input =
          Input_event.key_press (Input_event.named_key Input_event.Enter)
        in
        match
          apply_workspace_edits session input ~effect_id:"language.apply-edit"
            edits
        with
        | Error reason ->
            Option.iter
              (fun client ->
                Lsp.respond_apply_edit client ~request_id ~applied:false
                  ~reason:(Some reason))
              session.language_client;
            {
              session with
              message = Some ("workspace/applyEdit rejected: " ^ reason);
            }
        | Ok (next, _) ->
            Option.iter
              (fun client ->
                Lsp.respond_apply_edit client ~request_id ~applied:true
                  ~reason:None)
              next.language_client;
            { next with message = Some "workspace/applyEdit applied" })
    | Lsp.Server_message _ when background -> session
    | Lsp.Server_message message ->
        { session with message = Some ("language: " ^ message) }
    | Lsp.Request_failed _ when background -> session
    | Lsp.Request_failed { kind; reason; _ } ->
        let kind =
          match kind with
          | Lsp.Hover -> "hover"
          | Lsp.Definition -> "definition"
          | Lsp.Completion -> "completion"
          | Lsp.Rename -> "rename"
        in
        {
          session with
          message = Some ("language " ^ kind ^ " failed: " ^ reason);
        }
    | (Lsp.Server_failed _ | Lsp.Server_exited _) when background -> session
    | Lsp.Server_failed reason | Lsp.Server_exited reason ->
        {
          session with
          message = Some ("language server unavailable: " ^ reason);
        }
  in
  let session =
    match session.language_client with
    | None -> session
    | Some client -> List.fold_left handle session (Lsp.drain client)
  in
  session |> refresh_locations |> refresh_pane_view_positions

let poll_language session =
  let session = poll_active_language session in
  let foreground = session.current_buffer_id in
  let session =
    buffer_ids session
    |> List.filter (fun buffer_id -> buffer_id <> foreground)
    |> List.fold_left
         (fun session buffer_id ->
           let session =
             if buffer_id = session.current_buffer_id then session
             else activate_buffer ~reset_interaction:false session buffer_id
           in
           poll_active_language ~background:true session)
         session
  in
  if session.current_buffer_id = foreground then session
  else activate_buffer ~reset_interaction:false session foreground

let optional_text_argument arguments name =
  match
    List.find_opt
      (fun argument -> Command_argument.name argument = name)
      arguments
  with
  | None -> Ok None
  | Some argument -> (
      match Command_argument.value argument with
      | Command_argument.Text value -> Ok (Some value)
      | Command_argument.Selector _ | Command_argument.Transformation _ ->
          Error
            (Error.Invalid_command_arguments ("expected text argument " ^ name))
      )

let optional_positive_int_argument arguments name =
  match optional_text_argument arguments name with
  | Error _ as error -> error
  | Ok None -> Ok None
  | Ok (Some value) -> (
      match int_of_string_opt value with
      | Some count when count > 0 -> Ok (Some count)
      | Some _ ->
          Error
            (Error.Invalid_command_arguments
               ("expected positive integer argument " ^ name))
      | None ->
          Error
            (Error.Invalid_command_arguments
               ("expected integer argument " ^ name)))

let invoke_bound_command ?(arguments = []) session input binding =
  let command = Scripting.binding_command binding in
  let trace_binding next =
    let execution_id =
      Option.value ~default:0 (last_execution_of_active next.active)
    in
    let scope =
      match Scripting.binding_scope binding with
      | Scripting.Global -> "global"
      | Scripting.Model model -> "model:" ^ model
      | Scripting.Model_status { model; status } ->
          "model:" ^ model ^ ":" ^ status
      | Scripting.Mode mode -> "mode:" ^ mode
    in
    let scope =
      match Scripting.binding_layer binding with
      | None -> scope
      | Some layer -> scope ^ "; layer=" ^ layer
    in
    Trace.emit_lazy (trace_of_active next.active) (fun () ->
        Trace_event.Binding_resolved
          {
            execution_id;
            input =
              Input_event.binding_pattern_sequence_to_string
                (Scripting.binding_inputs binding);
            command_id = command;
            provider = Scripting.binding_provider binding;
            scope;
          });
    next
  in
  if String.equal command "config.reload" then
    let next = reload_config session in
    (trace_binding next, false)
  else if String.equal command "editor.macro.record" then
    match optional_text_argument arguments "register" with
    | Ok register ->
        (trace_binding (toggle_macro_recording ?register session), false)
    | Error error ->
        ( trace_binding { session with message = Some (Error.to_string error) },
          false )
  else if String.equal command "editor.macro.replay" then
    match
      ( optional_text_argument arguments "register",
        optional_positive_int_argument arguments "count" )
    with
    | Ok register, Ok count ->
        (trace_binding (request_macro_replay ?register ?count session), false)
    | Error error, _ | _, Error error ->
        ( trace_binding { session with message = Some (Error.to_string error) },
          false )
  else if String.equal command "editor.kill-ring.cut" then
    (trace_binding (cut_to_kill_ring session input), false)
  else if String.equal command "editor.kill-ring.yank" then
    (trace_binding (yank_latest_kill session input), false)
  else if String.equal command "editor.clipboard.copy" then
    (trace_binding (copy_to_system_clipboard session input), false)
  else if String.equal command "editor.clipboard.paste" then
    (trace_binding (paste_from_system_clipboard session input), false)
  else
    match request_bound_host_action session command with
    | Some next -> (trace_binding next, false)
    | None -> (
        match Command_id.of_string command with
        | Error error ->
            ({ session with message = Some (Error.to_string error) }, false)
        | Ok id ->
            if List.length arguments = 0 then
              match
                Command_registry.find (active_commands session.active) id
              with
              | Ok command
                when Command_descriptor.parameters (Command.descriptor command)
                     <> [] ->
                  ( begin_command_prompt session ~action:(Bound_command binding)
                      ~descriptor:(Command.descriptor command),
                    false )
              | Ok _ | Error _ ->
                  let invocation =
                    Command_invocation.create ~id ~arguments |> Result.get_ok
                  in
                  let next, changed =
                    execute_active_effects
                      ~augment_provenance:(fun provenance ->
                        Provenance.add provenance
                          (Provenance.Binding
                             {
                               input =
                                 Input_event.binding_pattern_sequence_to_string
                                   (Scripting.binding_inputs binding);
                               command;
                               provider = Scripting.binding_provider binding;
                             }))
                      session input
                      [ Model_effect.Invoke_command invocation ]
                  in
                  (trace_binding next, changed)
            else
              let invocation =
                Command_invocation.create ~id ~arguments |> Result.get_ok
              in
              let next, changed =
                execute_active_effects
                  ~augment_provenance:(fun provenance ->
                    Provenance.add provenance
                      (Provenance.Binding
                         {
                           input =
                             Input_event.binding_pattern_sequence_to_string
                               (Scripting.binding_inputs binding);
                           command;
                           provider = Scripting.binding_provider binding;
                         }))
                  session input
                  [ Model_effect.Invoke_command invocation ]
              in
              (trace_binding next, changed))

let rec run_event_hooks session event input =
  if List.mem event session.delivering_events then session
  else
    let hooks =
      (match session.generation with
        | None -> []
        | Some generation -> Scripting.hooks generation)
      @ Plugins.hooks session.plugins
      |> List.filter (fun hook -> Scripting.hook_event hook = event)
    in
    if hooks = [] then session
    else
      let started =
        { session with delivering_events = event :: session.delivering_events }
      in
      let completed =
        List.fold_left
          (fun session hook ->
            let trace = trace_of_active session.active in
            let profiler = profiler_of_active session.active in
            let execution_id =
              Option.value ~default:0 (last_execution_of_active session.active)
            in
            let event_name =
              match event with
              | Scripting.Document_changed -> "document-changed"
              | Scripting.After_save -> "after-save"
            in
            let provider = Scripting.hook_provider hook in
            extension_callback trace ~execution_id ~kind:"event" ~provider
              ~semantic_id:event_name "started";
            match
              Profiler.measure profiler
                (match Provider.kind provider with
                | Provider.Plugin -> Profiler.Extension_event
                | Provider.Script -> Profiler.Script_event
                | Provider.Builtin | Provider.Editing_model | Provider.Syntax
                | Provider.Application ->
                    Profiler.Model_handle)
                (fun () -> Scripting.run_hook hook (context session))
            with
            | Error error ->
                capability_denied trace ~execution_id ~provider error;
                extension_callback trace ~execution_id ~kind:"event" ~provider
                  ~semantic_id:event_name ~reason:(Error.to_string error)
                  "failed";
                {
                  session with
                  message = Some (Error.to_string error);
                  quit_armed = false;
                }
            | Ok effects ->
                extension_callback trace ~execution_id ~kind:"event" ~provider
                  ~semantic_id:event_name "succeeded";
                let next, changed =
                  execute_active_effects
                    ~augment_provenance:(fun provenance ->
                      Provenance.add provenance
                        (Provenance.Event { name = event_name; provider }))
                    session input effects
                in
                if changed then
                  run_event_hooks next Scripting.Document_changed input
                else next)
          started hooks
      in
      {
        completed with
        delivering_events =
          List.filter
            (fun active -> active <> event)
            completed.delivering_events;
      }

let event_is_named input named =
  match Input_event.key input with
  | Some (Input_event.Named_key value) -> value = named
  | Some (Input_event.Logical_text _) | None -> false

let event_text input =
  match Input_event.text input with
  | Some text -> Some text
  | None -> (
      match Input_event.key input with
      | Some (Input_event.Logical_text text)
        when Input_event.modifiers input = [] ->
          Some text
      | Some (Input_event.Logical_text _)
      | Some (Input_event.Named_key _)
      | None ->
          None)

let is_shortcut input ~text ~modifiers =
  match Input_event.key input with
  | Some (Input_event.Logical_text value) ->
      String.equal value text && Input_event.modifiers input = modifiers
  | Some (Input_event.Named_key _) | None -> false

let drop_last_utf8 text =
  let rec start index =
    if index <= 0 || Char.code text.[index] land 0xc0 <> 0x80 then index
    else start (index - 1)
  in
  if String.length text = 0 then text
  else String.sub text 0 (start (String.length text - 1))

let literal_matches contents query =
  if String.length query = 0 then []
  else
    let length = String.length contents in
    let query_length = String.length query in
    let rec find_at index =
      if index + query_length > length then []
      else if String.sub contents index query_length = query then
        {
          Zenbu_view.Renderer.start_offset = index;
          stop_offset = index + query_length;
        }
        :: find_at (index + 1)
      else find_at (index + 1)
    in
    find_at 0

let regexp_matches contents query =
  let buffer = Text_buffer.of_utf8 contents in
  match buffer with
  | Error error -> Error (Error.to_string error)
  | Ok buffer -> (
      try
        let regexp = Str.regexp query in
        let rec collect cursor matches =
          try
            ignore (Str.search_forward regexp contents cursor);
            let start_offset = Str.match_beginning () in
            let stop_offset = Str.match_end () in
            if start_offset = stop_offset then
              Error "regexp search rejects zero-width matches"
            else if
              not
                (Text_buffer.is_code_point_boundary buffer start_offset
                && Text_buffer.is_code_point_boundary buffer stop_offset)
            then
              Error
                "regexp search rejects matches that split a UTF-8 code point"
            else
              collect stop_offset
                ({ Zenbu_view.Renderer.start_offset; stop_offset } :: matches)
          with Not_found -> Ok (List.rev matches)
        in
        collect 0 []
      with Failure reason | Invalid_argument reason ->
        Error ("invalid regexp: " ^ reason))

let search_with_query session kind query =
  let contents = Editor_context.contents (context session) in
  let matches =
    match kind with
    | Literal -> Ok (literal_matches contents query)
    | Regexp -> regexp_matches contents query
  in
  Result.map
    (fun matches ->
      {
        kind;
        query;
        matches;
        current = (if matches = [] then None else Some 0);
      })
    matches

let non_overlapping_matches matches =
  let rec collect previous_stop accepted = function
    | [] -> List.rev accepted
    | (range : Zenbu_view.Renderer.search_range) :: rest ->
        if range.start_offset >= previous_stop then
          collect range.stop_offset (range :: accepted) rest
        else collect previous_stop accepted rest
  in
  collect 0 [] matches

let replace_all session input ~kind ~query ~replacement =
  if String.length query = 0 then
    { session with message = Some "replace: query must not be empty" }
  else
    match search_with_query session kind query with
    | Error reason -> { session with message = Some ("replace: " ^ reason) }
    | Ok search -> (
        let matches = non_overlapping_matches search.matches in
        if matches = [] then
          {
            session with
            message =
              Some
                ("replace: no " ^ search_kind_name kind ^ " matches for "
               ^ query);
          }
        else
          let ranges =
            List.map
              (fun (range : Zenbu_view.Renderer.search_range) ->
                (range.start_offset, range.stop_offset))
              matches
          in
          let contents = List.map (fun _ -> replacement) matches in
          match Model_intent.replace_ranges ~ranges ~primary:0 ~contents with
          | Error error ->
              { session with message = Some (Error.to_string error) }
          | Ok intent ->
              let next, changed =
                execute_active_effects
                  ~augment_provenance:(fun provenance ->
                    Provenance.add provenance
                      (Provenance.Effect "host.search.replace"))
                  session input
                  [
                    Model_effect.execute ~selector_id:"search.matches"
                      ~transformation_id:"replace-all" intent;
                  ]
              in
              if not changed then next
              else
                {
                  next with
                  interaction = Idle;
                  search = None;
                  inspector = None;
                  message =
                    Some
                      (Printf.sprintf "replace: %d %s match%s"
                         (List.length matches) (search_kind_name kind)
                         (if List.length matches = 1 then "" else "es"));
                })

let query_replace_message (state : query_replace) action =
  Printf.sprintf "query-replace: %s; %d replaced, %d skipped" action
    state.replaced state.skipped

let finish_query_replace session (state : query_replace) action =
  {
    session with
    interaction = Idle;
    search = None;
    inspector = None;
    message = Some (query_replace_message state action);
    quit_armed = false;
  }

let begin_query_replace session ~kind ~query ~replacement =
  if String.length query = 0 then
    { session with message = Some "query-replace: query must not be empty" }
  else
    match search_with_query session kind query with
    | Error reason ->
        { session with message = Some ("query-replace: " ^ reason) }
    | Ok search ->
        let pending = non_overlapping_matches search.matches in
        if pending = [] then
          {
            session with
            message =
              Some
                ("query-replace: no " ^ search_kind_name kind ^ " matches for "
               ^ query);
            quit_armed = false;
          }
        else
          {
            session with
            interaction =
              Query_replace
                {
                  kind;
                  query;
                  replacement;
                  document_version =
                    Editor_context.document_version (context session);
                  pending;
                  replaced = 0;
                  skipped = 0;
                };
            search = None;
            inspector = None;
            message =
              Some
                "query-replace: s skip, r replace, a replace remaining, q quit";
            quit_armed = false;
          }

let query_replace_stale session (state : query_replace) =
  if Editor_context.document_version (context session) = state.document_version
  then None
  else
    Some
      {
        session with
        interaction = Idle;
        search = None;
        inspector = None;
        message =
          Some
            "query-replace cancelled: the document changed since this review \
             began";
        quit_armed = false;
      }

let query_replace_next session (state : query_replace) =
  match state.pending with
  | [] -> finish_query_replace session state "complete"
  | _ ->
      {
        session with
        interaction = Query_replace state;
        message =
          Some
            (Printf.sprintf
               "query-replace: %d remaining; s skip, r replace, a all, q quit"
               (List.length state.pending));
        quit_armed = false;
      }

let replace_query_ranges session input (_state : query_replace) ranges
    replacement =
  let ranges =
    List.map
      (fun (range : Zenbu_view.Renderer.search_range) ->
        (range.start_offset, range.stop_offset))
      ranges
  in
  let contents = List.map (fun _ -> replacement) ranges in
  match Model_intent.replace_ranges ~ranges ~primary:0 ~contents with
  | Error error ->
      ({ session with message = Some (Error.to_string error) }, false)
  | Ok intent ->
      execute_active_effects
        ~augment_provenance:(fun provenance ->
          Provenance.add provenance
            (Provenance.Effect "host.search.query-replace"))
        session input
        [
          Model_effect.execute ~selector_id:"query-replace.matches"
            ~transformation_id:"replace" intent;
        ]

let replace_query_current session input (state : query_replace) current rest =
  let next, changed =
    replace_query_ranges session input state [ current ] state.replacement
  in
  if not changed then
    {
      next with
      interaction = Idle;
      message = Some "query-replace cancelled: current replacement was rejected";
      inspector = None;
      quit_armed = false;
    }
  else
    let delta =
      String.length state.replacement
      - (current.stop_offset - current.start_offset)
    in
    let pending =
      List.map
        (fun (range : Zenbu_view.Renderer.search_range) ->
          {
            Zenbu_view.Renderer.start_offset = range.start_offset + delta;
            stop_offset = range.stop_offset + delta;
          })
        rest
    in
    query_replace_next next
      {
        state with
        document_version = Editor_context.document_version (context next);
        pending;
        replaced = state.replaced + 1;
      }

let replace_query_remaining session input (state : query_replace) =
  let next, changed =
    replace_query_ranges session input state state.pending state.replacement
  in
  if not changed then
    {
      next with
      interaction = Idle;
      message =
        Some "query-replace cancelled: remaining replacements were rejected";
      inspector = None;
      quit_armed = false;
    }
  else
    finish_query_replace next
      {
        state with
        pending = [];
        document_version = Editor_context.document_version (context next);
        replaced = state.replaced + List.length state.pending;
      }
      "complete"

let handle_query_replace_input session (state : query_replace) input =
  match query_replace_stale session state with
  | Some session -> session
  | None -> (
      match state.pending with
      | [] -> finish_query_replace session state "complete"
      | current :: rest ->
          if
            event_is_named input Input_event.Escape
            || is_shortcut input ~text:"q" ~modifiers:[]
          then finish_query_replace session state "quit"
          else if is_shortcut input ~text:"s" ~modifiers:[] then
            query_replace_next session
              { state with pending = rest; skipped = state.skipped + 1 }
          else if is_shortcut input ~text:"r" ~modifiers:[] then
            replace_query_current session input state current rest
          else if is_shortcut input ~text:"a" ~modifiers:[] then
            replace_query_remaining session input state
          else session)

let refresh_search_after_document_change session =
  match session.search with
  | None -> session
  | Some previous -> (
      match search_with_query session previous.kind previous.query with
      | Error reason ->
          {
            session with
            search = None;
            message = Some ("search cleared after document change: " ^ reason);
          }
      | Ok refreshed ->
          let selections = Editor_context.selections (context session) in
          let primary =
            List.nth selections.selections selections.primary_index
          in
          let current =
            refreshed.matches
            |> List.find_index
                 (fun (range : Zenbu_view.Renderer.search_range) ->
                   range.start_offset = primary.anchor_offset
                   && range.stop_offset = primary.head_offset)
            |> function
            | Some index -> Some index
            | None -> refreshed.current
          in
          { session with search = Some { refreshed with current } })

let move_to_search_match session input search index =
  match List.nth_opt search.matches index with
  | None -> { session with search = Some search }
  | Some range -> (
      match
        Model_intent.set_selections
          ~selections:[ (range.start_offset, range.stop_offset) ]
          ~primary:0
      with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok intent ->
          let next, _ =
            execute_active_effects
              ~augment_provenance:(fun provenance ->
                Provenance.add provenance (Provenance.Effect "host.search"))
              session input
              [ Model_effect.Execute_intent intent ]
          in
          { next with search = Some { search with current = Some index } })

let move_search session input direction =
  match session.search with
  | None -> { session with message = Some "search: no active query" }
  | Some { matches = []; query; _ } ->
      { session with message = Some ("search: no matches for " ^ query) }
  | Some search ->
      let count = List.length search.matches in
      let current = Option.value ~default:0 search.current in
      let index = (current + direction + count) mod count in
      move_to_search_match session input search index

let restore_search_origin session input origin =
  let selections =
    origin.Editor_context.selections
    |> List.map (fun selection ->
        (selection.Editor_context.anchor_offset, selection.head_offset))
  in
  match
    Model_intent.set_selections ~selections ~primary:origin.primary_index
  with
  | Error error ->
      {
        session with
        interaction = Idle;
        search = None;
        message = Some ("search cancel failed: " ^ Error.to_string error);
      }
  | Ok intent ->
      let next, _ =
        execute_active_effects
          ~augment_provenance:(fun provenance ->
            Provenance.add provenance (Provenance.Effect "host.search.cancel"))
          session input
          [ Model_effect.Execute_intent intent ]
      in
      {
        next with
        interaction = Idle;
        search = None;
        message = Some "search cancelled; restored the pre-search selection";
      }

let contains_casefold ~needle text =
  let needle = String.lowercase_ascii needle in
  let text = String.lowercase_ascii text in
  let needle_length = String.length needle in
  let rec loop index =
    if needle_length = 0 then true
    else if index + needle_length > String.length text then false
    else if String.sub text index needle_length = needle then true
    else loop (index + 1)
  in
  loop 0

let matching_completion_items items query =
  items
  |> List.filter (fun (item : Language.completion) ->
      contains_casefold ~needle:query item.label
      || Option.value ~default:false
           (Option.map (contains_casefold ~needle:query) item.filter_text)
      || Option.value ~default:false
           (Option.map (contains_casefold ~needle:query) item.detail))

let language_host_command = function
  | Language_status | Language_restart | Language_hover | Language_definition
  | Language_complete | Language_rename | Language_diagnostic_next
  | Language_diagnostic_previous | Language_diagnostic_describe_current ->
      true
  | Save | Save_as | Save_layout | Restore_layout | Set_project_root
  | Open_file_picker | Search_project | Quit | Force_quit | Reload_config
  | Start_search | Start_regexp_search | Replace_all_literal
  | Replace_all_regexp | Start_query_replace_literal
  | Start_query_replace_regexp | Search_next | Search_previous
  | Toggle_macro_recording | Replay_macro | Kill_ring_cut | Kill_ring_yank
  | System_clipboard_copy | System_clipboard_paste | Set_location
  | Jump_location | Push_jump | Jump_backward | Jump_forward | Open_palette
  | Open_command_line | Switch_model | Enable_binding_layer
  | Disable_binding_layer | Help | Switch_presentation | Switch_theme
  | Background_jobs | Cancel_background_job | Open_background_job_output
  | Split_vertical | Split_horizontal | Focus_next_pane | Close_pane | Only_pane
  | Grow_pane_width | Shrink_pane_width | Grow_pane_height | Shrink_pane_height
  | Balance_panes | New_buffer | Open_buffer | List_buffers | Switch_buffer
  | Rename_buffer | Close_buffer | Force_close_buffer | Next_buffer
  | Previous_buffer | View_scroll_up | View_scroll_down | View_page_up
  | View_page_down | View_center ->
      false

let palette_items session =
  let from_descriptor action descriptor =
    {
      id = Command_descriptor.id descriptor |> Command_id.to_string;
      title = Command_descriptor.title descriptor;
      description = Command_descriptor.description descriptor;
      provider = Command_descriptor.provider descriptor;
      action;
      descriptor;
    }
  in
  let host =
    Lazy.force host_command_entries
    |> List.filter (fun entry ->
        entry.palette
        && ((not (language_host_command entry.command))
           || Option.is_some session.language_client))
    |> List.map (fun entry ->
        from_descriptor (Invoke_host_command entry.command) entry.descriptor)
  in
  let model_and_extensions =
    active_commands session.active
    |> Command_registry.descriptors
    |> List.map (fun descriptor ->
        from_descriptor
          (Invoke_command (Command_descriptor.id descriptor))
          descriptor)
  in
  List.sort
    (fun (left : palette_item) (right : palette_item) ->
      String.compare left.id right.id)
    (host @ model_and_extensions)

let matching_palette_items session query =
  palette_items session
  |> List.filter (fun (item : palette_item) ->
      contains_casefold ~needle:query item.id
      || contains_casefold ~needle:query item.title
      || Option.value ~default:false
           (Option.map (contains_casefold ~needle:query) item.description)
      || contains_casefold ~needle:query (Provider.id item.provider))

let switch_to_model session target =
  if model session = target then
    {
      session with
      interaction = Idle;
      message = Some "editing model is already active";
      inspector = None;
    }
  else
    match
      active_from_shared
        ?script_model:(Option.bind session.generation Scripting.model)
        target
        (shared_state session.active)
    with
    | Error error ->
        {
          session with
          interaction = Idle;
          message = Some ("model switch failed: " ^ Error.to_string error);
          inspector = None;
        }
    | Ok active ->
        let name =
          match active with
          | Vim_runtime _ -> "Vim-style editing model"
          | Selection_runtime _ -> "Selection-first editing model"
          | Direct_runtime _ -> "Direct editing model"
          | Structural_runtime _ -> "Structural editing model"
          | Script_runtime _ -> "Script editing model"
        in
        {
          session with
          active;
          interaction = Idle;
          message = Some ("switched editing model to " ^ name);
          inspector = None;
          quit_armed = false;
        }

let save_to ?(overwrite = false) session path =
  let contents = Editor_context.contents (context session) in
  let previous_path = session.file_path in
  let complete saved_snapshot =
    let saved =
      {
        session with
        file_path = Some path;
        saved_version = Editor_context.document_version (context session);
        saved_contents = contents;
        saved_snapshot = Some saved_snapshot;
        interaction = Idle;
        message = Some ("saved " ^ path);
        quit_armed = false;
      }
    in
    File_watcher.watch saved.file_watcher ~path ~snapshot:saved_snapshot;
    Option.iter
      (fun previous ->
        let still_open =
          current_buffer saved :: saved.inactive_buffers
          |> List.exists (fun (buffer : buffer) ->
              buffer.file_path = Some previous)
        in
        if (not (String.equal previous path)) && not still_open then
          File_watcher.unwatch saved.file_watcher ~path:previous)
      previous_path;
    let saved =
      match session.file_path with
      | Some previous when String.equal previous path -> saved
      | None | Some _ -> replace_language_client saved path
    in
    Option.iter
      (fun client ->
        Lsp.set_execution_id client
          ~execution_id:
            (Option.value ~default:0 (last_execution_of_active saved.active));
        Lsp.notify_save client
          ~contents:(Editor_context.contents (context saved))
          ~document_version:(Editor_context.document_version (context saved)))
      saved.language_client;
    let input =
      Input_event.logical_text "s"
      |> Result.get_ok
      |> Input_event.key_press ~modifiers:[ Input_event.Control ]
    in
    run_event_hooks saved Scripting.After_save input
  in
  let completed =
    match (overwrite, session.saved_snapshot) with
    | false, None ->
        {
          session with
          interaction = Idle;
          message =
            Some
              ("save conflict for " ^ path
             ^ ": no on-disk baseline; use save as to replace the target");
          quit_armed = false;
        }
    | true, _ -> (
        match File_io.save_atomic_snapshot ~path ~contents with
        | Error error ->
            {
              session with
              interaction = Idle;
              message = Some (File_io.to_string error);
              quit_armed = false;
            }
        | Ok saved_snapshot -> complete saved_snapshot)
    | false, Some saved_snapshot -> (
        match
          Result.bind (File_io.check_snapshot saved_snapshot ~path) (fun () ->
              File_io.save_atomic_snapshot ~path ~contents)
        with
        | Error error ->
            {
              session with
              interaction = Idle;
              message = Some (File_io.to_string error);
              quit_armed = false;
            }
        | Ok saved_snapshot -> complete saved_snapshot)
  in
  let completed = synchronize_workspace_documents completed in
  let execution_id =
    Option.value ~default:0 (last_execution_of_active completed.active)
  in
  trace_runtime_events
    (trace_of_active completed.active)
    (profiler_of_active completed.active)
    ~execution_id completed.plugins;
  completed

let model_choices session =
  [ Vim; Selection; Structural; Direct ]
  @
  match Option.bind session.generation Scripting.model with
  | Some _ -> [ Script ]
  | None -> []

let search_index ~origin ~(direction : Model_effect.search_direction) matches =
  let primary =
    List.nth origin.Editor_context.selections origin.primary_index
  in
  let offset = primary.Editor_context.head_offset in
  let rec first_after index = function
    | [] -> None
    | (range : Zenbu_view.Renderer.search_range) :: rest ->
        if range.start_offset >= offset then Some index
        else first_after (index + 1) rest
  in
  let rec last_before index best = function
    | [] -> best
    | (range : Zenbu_view.Renderer.search_range) :: rest ->
        let best = if range.stop_offset < offset then Some index else best in
        last_before (index + 1) best rest
  in
  match direction with
  | Model_effect.Forward -> Option.value ~default:0 (first_after 0 matches)
  | Model_effect.Backward ->
      Option.value
        ~default:(max 0 (List.length matches - 1))
        (last_before 0 None matches)

let update_search session input ~kind ~origin
    ~(direction : Model_effect.search_direction) query =
  if String.length query = 0 then
    {
      session with
      search = None;
      message = Some ("search: enter " ^ search_kind_name kind ^ " text");
    }
  else
    match search_with_query session kind query with
    | Error reason ->
        { session with search = None; message = Some ("search: " ^ reason) }
    | Ok search -> (
        match search.matches with
        | [] ->
            {
              session with
              search = Some search;
              message =
                Some
                  ("search: no " ^ search_kind_name kind ^ " matches for "
                 ^ query);
            }
        | matches ->
            move_to_search_match session input search
              (search_index ~origin ~direction matches))

let request_save session =
  match session.file_path with
  | None ->
      {
        session with
        interaction = Save_as_prompt "";
        message = Some "save-as: enter a destination path";
        quit_armed = false;
      }
  | Some path -> save_to session path

let filter_current_selections session request =
  let contents = Editor_context.contents (context session) in
  let selections = Editor_context.selections (context session) in
  let rec collect total outputs = function
    | [] -> Ok (List.rev outputs)
    | (selection : Editor_context.selection) :: rest ->
        let start = min selection.anchor_offset selection.head_offset in
        let stop = max selection.anchor_offset selection.head_offset in
        let selected = String.sub contents start (stop - start) in
        Result.bind (External_filter.run request selected) (fun output ->
            if total + String.length output > External_filter.maximum_bytes then
              Error
                (Error.External_filter_error
                   (Printf.sprintf
                      "combined selection output exceeds the %d-byte limit"
                      External_filter.maximum_bytes))
            else collect (total + String.length output) (output :: outputs) rest)
  in
  collect 0 [] selections.selections

let apply_external_filter session input request =
  match filter_current_selections session request with
  | Error error ->
      {
        session with
        message = Some (Error.to_string error);
        inspector = None;
        quit_armed = false;
      }
  | Ok outputs ->
      let next, _ =
        execute_active_effects
          ~augment_provenance:(fun provenance ->
            Provenance.add provenance (Provenance.Effect "host.external-filter"))
          session input
          [
            Model_effect.Execute_intent
              (Model_intent.replace_selection_contents outputs);
          ]
      in
      {
        next with
        interaction = Idle;
        inspector = None;
        quit_armed = false;
        message = Some ("external filter: " ^ request.program);
      }

let background_job_lines session =
  match session.jobs with
  | None -> [ "Jobs"; "no jobs" ]
  | Some jobs -> Background_job.lines jobs

let start_background_process session request =
  let jobs =
    match session.jobs with
    | Some jobs -> jobs
    | None -> Background_job.create ()
  in
  let session = { session with jobs = Some jobs } in
  match Background_job.start jobs request with
  | Error error ->
      {
        session with
        message = Some (Error.to_string error);
        inspector = None;
        quit_armed = false;
      }
  | Ok id ->
      {
        session with
        message =
          Some
            (Printf.sprintf "background job %d started: %s" id
               request.Model_effect.program);
        inspector = None;
        quit_armed = false;
      }

let handle_model_host_request session input = function
  | Model_effect.Request_search direction -> begin_search ~direction session
  | Model_effect.Repeat_search Model_effect.Forward ->
      move_search session input 1
  | Model_effect.Repeat_search Model_effect.Backward ->
      move_search session input (-1)
  | Model_effect.Request_macro Model_effect.Reserve_macro_input ->
      { session with macro_control = true; quit_armed = false }
  | Model_effect.Request_macro (Model_effect.Toggle_macro_recording register) ->
      toggle_macro_recording ~register session
  | Model_effect.Request_macro (Model_effect.Replay_macro { register; count })
    ->
      request_macro_replay ~register ~count session
  | Model_effect.Request_location (Model_effect.Set_location name) ->
      set_location session name
  | Model_effect.Request_location (Model_effect.Jump_location name) ->
      jump_to_location session input name
  | Model_effect.Request_jump Model_effect.Push_current_jump ->
      push_current_jump session
  | Model_effect.Request_jump (Model_effect.Traverse_jump { direction; count })
    ->
      traverse_jumps session input ~direction ~count
  | Model_effect.Request_workspace request -> request_workspace session request
  | Model_effect.Request_viewport request -> request_viewport session request
  | Model_effect.Request_external_filter request ->
      apply_external_filter session input request
  | Model_effect.Request_background_process request ->
      start_background_process session request
  | Model_effect.Request_save -> request_save session
  | _ -> session

let save = request_save

let required_text_argument arguments name =
  match
    List.find_opt
      (fun argument -> Command_argument.name argument = name)
      arguments
  with
  | Some argument -> (
      match Command_argument.value argument with
      | Command_argument.Text value -> Ok value
      | Command_argument.Selector _ | Command_argument.Transformation _ ->
          Error
            (Error.Invalid_command_arguments ("expected text argument " ^ name))
      )
  | None -> Error (Error.Invalid_command_arguments ("missing argument " ^ name))

let required_positive_int_argument arguments name =
  Result.bind (required_text_argument arguments name) (fun value ->
      match int_of_string_opt value with
      | Some value when value > 0 -> Ok value
      | Some _ ->
          Error
            (Error.Invalid_command_arguments
               ("expected positive integer argument " ^ name))
      | None ->
          Error
            (Error.Invalid_command_arguments
               ("expected integer argument " ^ name)))

let required_nonnegative_int_argument arguments name =
  Result.bind (required_text_argument arguments name) (fun value ->
      match int_of_string_opt value with
      | Some value when value >= 0 -> Ok value
      | Some _ ->
          Error
            (Error.Invalid_command_arguments
               ("expected nonnegative integer argument " ^ name))
      | None ->
          Error
            (Error.Invalid_command_arguments
               ("expected integer argument " ^ name)))

let set_presentation session ~profile =
  let selected =
    match Zenbu_view.Presentation.find_builtin profile with
    | Some presentation -> Ok presentation
    | None ->
        Zenbu_view.Presentation.load profile
        |> Result.map_error (fun reason ->
            Error.Invalid_command_arguments reason)
  in
  match selected with
  | Error error ->
      {
        session with
        message = Some (Error.to_string error);
        inspector = None;
        quit_armed = false;
      }
  | Ok presentation ->
      {
        session with
        presentation;
        message =
          Some ("presentation: " ^ Zenbu_view.Presentation.name presentation);
        inspector = None;
        quit_armed = false;
      }

let set_theme session ~theme =
  let selected =
    match Zenbu_view.Theme.find_builtin theme with
    | Some theme -> Ok theme
    | None ->
        Zenbu_view.Theme.load theme
        |> Result.map_error (fun reason ->
            Error.Invalid_command_arguments reason)
  in
  match selected with
  | Error error ->
      {
        session with
        message = Some (Error.to_string error);
        inspector = None;
        quit_armed = false;
      }
  | Ok theme ->
      {
        session with
        theme;
        message = Some ("theme: " ^ Zenbu_view.Theme.name theme);
        inspector = None;
        quit_armed = false;
      }

let cancel_background_job session ~job_id =
  match session.jobs with
  | None ->
      {
        session with
        message = Some "background job cancellation rejected: no jobs";
        inspector = None;
        quit_armed = false;
      }
  | Some jobs -> (
      match Background_job.cancel jobs ~id:job_id with
      | Error error ->
          {
            session with
            message = Some (Error.to_string error);
            inspector = None;
            quit_armed = false;
          }
      | Ok () ->
          {
            session with
            message = Some (Printf.sprintf "background job %d cancelled" job_id);
            inspector = None;
            quit_armed = false;
          })

let open_background_job_output session ~job_id =
  match session.jobs with
  | None ->
      {
        session with
        message = Some "background job output rejected: no jobs";
        inspector = None;
        quit_armed = false;
      }
  | Some jobs -> (
      match Background_job.output jobs ~id:job_id with
      | Error error ->
          {
            session with
            message = Some (Error.to_string error);
            inspector = None;
            quit_armed = false;
          }
      | Ok contents ->
          new_buffer_with_contents
            ~buffer_name:(Printf.sprintf "*job %d output*" job_id)
            session ~contents
            ~message:
              (Printf.sprintf
                 "background job %d output opened in buffer *job %d output*"
                 job_id job_id))

let release_session session =
  Option.iter Background_job.close session.jobs;
  current_buffer session :: session.inactive_buffers
  |> List.iter (fun (buffer : buffer) ->
      Option.iter Lsp.close buffer.language_client)

let layout_error message =
  Error.Invalid_command_arguments ("workspace layout: " ^ message)

let layout_model_of_model = function
  | Vim -> Ok Session_layout.Vim
  | Selection -> Ok Session_layout.Selection
  | Direct -> Ok Session_layout.Direct
  | Structural -> Ok Session_layout.Structural
  | Script ->
      Error
        (layout_error
           "script editing models are not persisted because their internals \
            are host-external")

let model_of_layout_model = function
  | Session_layout.Vim -> Vim
  | Session_layout.Selection -> Selection
  | Session_layout.Direct -> Direct
  | Session_layout.Structural -> Structural

let layout_position_of_view_position pane buffer (position : view_position) =
  if position.stale then
    Error
      (layout_error
         (Printf.sprintf
            "view state for pane %d and buffer %d is stale and cannot be saved"
            pane buffer))
  else
    Ok
      Session_layout.
        {
          pane;
          buffer;
          selections =
            List.map
              (fun (anchor, head) -> { anchor; head })
              position.selections;
          primary = position.primary;
        }

let save_layout session ~path =
  let session = capture_focused_view_position session in
  let buffers = current_buffer session :: session.inactive_buffers in
  let rec serialize_buffers values = function
    | [] -> Ok (List.rev values)
    | (buffer : buffer) :: rest -> (
        match (buffer.file_path, buffer.saved_snapshot) with
        | None, _ ->
            Error
              (layout_error
                 (Printf.sprintf
                    "buffer %d is unnamed; save it before saving the workspace \
                     layout"
                    buffer.id))
        | Some _, None ->
            Error
              (layout_error
                 (Printf.sprintf "buffer %d has no verified file baseline"
                    buffer.id))
        | Some file_path, Some saved_snapshot -> (
            if buffer_dirty buffer then
              Error
                (layout_error
                   (Printf.sprintf
                      "buffer %d has unsaved changes; save it before saving \
                       the workspace layout"
                      buffer.id))
            else
              match File_io.check_snapshot saved_snapshot ~path:file_path with
              | Error error -> Error (layout_error (File_io.to_string error))
              | Ok () -> (
                  match
                    layout_model_of_model (model_of_active buffer.active)
                  with
                  | Error _ as error -> error
                  | Ok model ->
                      serialize_buffers
                        (Session_layout.
                           {
                             id = buffer.id;
                             path = file_path;
                             name = buffer.buffer_name;
                             language = buffer.language_override;
                             model;
                           }
                        :: values)
                        rest)))
  in
  match serialize_buffers [] buffers with
  | Error _ as error -> error
  | Ok buffers -> (
      let rec serialize_positions values = function
        | [] -> Ok (List.rev values)
        | ((pane, buffer), position) :: rest -> (
            match layout_position_of_view_position pane buffer position with
            | Error _ as error -> error
            | Ok position -> serialize_positions (position :: values) rest)
      in
      match serialize_positions [] session.pane_view_positions with
      | Error _ as error -> error
      | Ok view_positions ->
          let layout =
            Session_layout.
              {
                schema_version = Session_layout.current_schema_version;
                buffers;
                layout = Layout.to_persisted session.layout;
                focused_pane = session.focused_pane;
                pane_buffers =
                  List.map
                    (fun (pane, buffer) -> Session_layout.{ pane; buffer })
                    session.pane_buffers;
                viewports =
                  List.map
                    (fun (pane, (viewport : Zenbu_view.Viewport.t)) ->
                      Session_layout.
                        {
                          pane;
                          top_line = viewport.top_line;
                          left_column = viewport.left_column;
                          follow_cursor = viewport.follow_cursor;
                        })
                    session.pane_viewports;
                view_positions;
              }
          in
          File_io.save_atomic ~path ~contents:(Session_layout.encode layout)
          |> Result.map_error (fun error ->
              layout_error (File_io.to_string error)))

type restored_layout_buffer = {
  specification : Session_layout.buffer;
  contents : string;
  snapshot : File_io.snapshot;
}

let validate_layout_position ~contents (position : Session_layout.view_position)
    =
  match Text_buffer.of_utf8 contents with
  | Error error -> Error (layout_error (Error.to_string error))
  | Ok buffer ->
      let byte_length = Text_buffer.byte_length buffer in
      let validate_offset kind offset =
        if offset > byte_length then
          Error
            (layout_error
               (Printf.sprintf
                  "invalid %s offset %d for pane %d and buffer %d: outside \
                   %d-byte file"
                  kind offset position.pane position.buffer byte_length))
        else if not (Text_buffer.is_code_point_boundary buffer offset) then
          Error
            (layout_error
               (Printf.sprintf
                  "invalid %s offset %d for pane %d and buffer %d: splits a \
                   UTF-8 code point"
                  kind offset position.pane position.buffer))
        else Ok ()
      in
      let rec validate = function
        | [] -> Ok ()
        | (selection : Session_layout.selection) :: rest -> (
            match validate_offset "anchor" selection.anchor with
            | Error _ as error -> error
            | Ok () -> (
                match validate_offset "head" selection.head with
                | Error _ as error -> error
                | Ok () -> validate rest))
      in
      validate position.selections

let preflight_layout (layout : Session_layout.t) =
  let rec read_buffers values = function
    | [] -> Ok (List.rev values)
    | (specification : Session_layout.buffer) :: rest -> (
        match specification.name with
        | Some name -> (
            match valid_buffer_name name with
            | Error error -> Error (layout_error (Error.to_string error))
            | Ok _ -> read_buffer values specification rest)
        | None -> read_buffer values specification rest)
  and read_buffer values specification rest =
    match
      syntax_service ?language:specification.language (Some specification.path)
    with
    | Error error -> Error (layout_error (Error.to_string error))
    | Ok _ -> (
        match File_io.read_snapshot specification.path with
        | Error error -> Error (layout_error (File_io.to_string error))
        | Ok (contents, snapshot) ->
            read_buffers ({ specification; contents; snapshot } :: values) rest)
  in
  match read_buffers [] layout.buffers with
  | Error _ as error -> error
  | Ok buffers ->
      let buffer_for_id buffer_id =
        List.find_opt
          (fun (buffer : restored_layout_buffer) ->
            buffer.specification.id = buffer_id)
          buffers
      in
      let rec validate_positions = function
        | [] -> Ok buffers
        | (position : Session_layout.view_position) :: rest -> (
            match buffer_for_id position.buffer with
            | None ->
                Error
                  (layout_error
                     (Printf.sprintf "selection references unknown buffer %d"
                        position.buffer))
            | Some buffer -> (
                match
                  validate_layout_position ~contents:buffer.contents position
                with
                | Error _ as error -> error
                | Ok () -> validate_positions rest))
      in
      validate_positions layout.view_positions

let restore_layout session ~path =
  match File_io.read path with
  | Error error -> Error (layout_error (File_io.to_string error))
  | Ok contents -> (
      match Session_layout.decode contents with
      | Error error -> Error (layout_error error)
      | Ok layout -> (
          match preflight_layout layout with
          | Error _ as error -> error
          | Ok buffers -> (
              match buffers with
              | [] -> Error (layout_error "layout has no buffers")
              | initial :: remaining -> (
                  let create_initial () =
                    create
                      ~model:(model_of_layout_model initial.specification.model)
                      ?language:initial.specification.language
                      ~file_path:initial.specification.path
                      ~contents:initial.contents
                      ~trace:(trace_of_active session.active)
                      ~profiler:(profiler_of_active session.active)
                      ~presentation:session.presentation ~theme:session.theme
                      ~system_clipboard:session.system_clipboard
                      ~config:session.config ~plugins:session.plugins_config
                      ~language_registry:session.language_registry
                      ~dimensions:session.dimensions ()
                    |> Result.map_error (fun error ->
                        layout_error (Error.to_string error))
                  in
                  match create_initial () with
                  | Error _ as error -> error
                  | Ok initial_session -> (
                      let initial_session =
                        {
                          initial_session with
                          current_buffer_id = initial.specification.id;
                          buffer_name = initial.specification.name;
                          language_override = initial.specification.language;
                          saved_contents = initial.contents;
                          saved_snapshot = Some initial.snapshot;
                        }
                      in
                      let rec add_buffers restored = function
                        | [] -> Ok restored
                        | buffer :: rest -> (
                            match
                              create_buffer restored ~id:buffer.specification.id
                                ~editing_model:
                                  (model_of_layout_model
                                     buffer.specification.model)
                                ~file_path:buffer.specification.path
                                ?buffer_name:buffer.specification.name
                                ~language:buffer.specification.language
                                ~saved_snapshot:buffer.snapshot
                                ~contents:buffer.contents ()
                            with
                            | Error error ->
                                Error (layout_error (Error.to_string error))
                            | Ok created ->
                                add_buffers
                                  {
                                    restored with
                                    inactive_buffers =
                                      created :: restored.inactive_buffers;
                                  }
                                  rest)
                      in
                      match add_buffers initial_session remaining with
                      | Error _ as error -> error
                      | Ok restored -> (
                          match Layout.of_persisted layout.layout with
                          | Error error -> Error (layout_error error)
                          | Ok restored_layout ->
                              let pane_ids = Layout.panes restored_layout in
                              let next_pane_id =
                                List.fold_left max 0 pane_ids + 1
                              in
                              let next_buffer_id =
                                List.fold_left
                                  (fun maximum (buffer : restored_layout_buffer)
                                     -> max maximum buffer.specification.id)
                                  0 buffers
                                + 1
                              in
                              let restored =
                                {
                                  restored with
                                  layout = restored_layout;
                                  focused_pane = layout.focused_pane;
                                  pane_viewports =
                                    List.map
                                      (fun (viewport : Session_layout.viewport)
                                         ->
                                        ( viewport.pane,
                                          Zenbu_view.Viewport.
                                            {
                                              top_line = viewport.top_line;
                                              left_column = viewport.left_column;
                                              follow_cursor =
                                                viewport.follow_cursor;
                                            } ))
                                      layout.viewports;
                                  pane_view_positions =
                                    List.map
                                      (fun (position :
                                             Session_layout.view_position) ->
                                        ( (position.pane, position.buffer),
                                          {
                                            buffer_id = position.buffer;
                                            document_version = 0;
                                            selections =
                                              List.map
                                                (fun (selection :
                                                       Session_layout.selection)
                                                   ->
                                                  ( selection.anchor,
                                                    selection.head ))
                                                position.selections;
                                            primary = position.primary;
                                            stale = false;
                                          } ))
                                      layout.view_positions;
                                  next_pane_id;
                                  next_buffer_id;
                                  pane_buffers =
                                    List.map
                                      (fun (mapping :
                                             Session_layout.pane_buffer) ->
                                        (mapping.pane, mapping.buffer))
                                      layout.pane_buffers;
                                  interaction = Idle;
                                  inspector = None;
                                  message = None;
                                  mouse_drag = None;
                                  pending_binding = [];
                                  locations = [];
                                  backward_jumps = [];
                                  forward_jumps = [];
                                }
                                |> synchronize_workspace_documents
                              in
                              let target_buffer =
                                List.assoc layout.focused_pane
                                  restored.pane_buffers
                              in
                              let restored =
                                activate_buffer restored target_buffer
                              in
                              let restored =
                                restore_pane_view_position restored
                                  layout.focused_pane
                              in
                              release_session session;
                              Ok
                                {
                                  restored with
                                  message =
                                    Some
                                      ("workspace layout restored from " ^ path);
                                  interaction = Idle;
                                  inspector = None;
                                  quit_armed = false;
                                }))))))

let project_root_error message =
  Error.Invalid_command_arguments ("project root: " ^ message)

let project_root session = Option.map Project_root.path session.project_root

let project_root_lines session =
  match project_root session with
  | None -> [ "Project"; "root: none" ]
  | Some path ->
      [
        "Project";
        "root: " ^ path;
        "validation: canonical root; relative regular files without traversal";
        "picker: hidden, binary, unreadable, and symlink entries are excluded";
      ]

let project_search_lines session =
  match session.project_search with
  | None -> [ "Project search"; "query: none" ]
  | Some snapshot ->
      [
        "Project search";
        "query: " ^ snapshot.Project_search.query;
        "results: " ^ string_of_int (List.length snapshot.results);
        "scanned-files: " ^ string_of_int snapshot.scanned_files;
        "scanned-bytes: " ^ string_of_int snapshot.scanned_bytes;
        "truncated: " ^ string_of_bool snapshot.truncated;
        "limits: files="
        ^ string_of_int Project_search.default_limits.maximum_files
        ^ " results="
        ^ string_of_int Project_search.default_limits.maximum_results
        ^ " bytes-per-file="
        ^ string_of_int Project_search.default_limits.maximum_bytes_per_file
        ^ " total-bytes="
        ^ string_of_int Project_search.default_limits.maximum_total_bytes;
      ]
      @ List.map
          (fun (result : Project_search.result) ->
            Printf.sprintf "%s:%d byte=%d" result.relative_path result.line
              result.byte_offset)
          snapshot.results

let set_project_root session ~path =
  Project_root.select path
  |> Result.map_error project_root_error
  |> Result.map (fun root ->
      {
        session with
        project_root = Some root;
        interaction = Idle;
        inspector = None;
        message = Some ("project root selected: " ^ Project_root.path root);
        quit_armed = false;
      })

let project_file_entries session ~query =
  match session.project_root with
  | None ->
      Error (project_root_error "select a root before opening the file picker")
  | Some root ->
      Project_root.filter root ~query |> Result.map_error project_root_error

let begin_file_picker session =
  match project_file_entries session ~query:"" with
  | Error error ->
      {
        session with
        message = Some (Error.to_string error);
        inspector = None;
        quit_armed = false;
      }
  | Ok _ ->
      {
        session with
        interaction = File_picker { query = ""; selected = 0 };
        message =
          Some "project files: type to filter, Enter opens, Escape cancels";
        inspector = None;
        quit_armed = false;
      }

let file_picker_with_query session ~query ~selected =
  match project_file_entries session ~query with
  | Error error ->
      {
        session with
        interaction = File_picker { query; selected = 0 };
        message = Some (Error.to_string error);
        inspector = None;
        quit_armed = false;
      }
  | Ok entries ->
      {
        session with
        interaction =
          File_picker
            {
              query;
              selected =
                (if entries = [] then 0
                 else min (List.length entries - 1) (max 0 selected));
            };
        quit_armed = false;
      }

let open_picked_file session (entry : Project_root.entry) =
  match session.project_root with
  | None ->
      {
        session with
        interaction = Idle;
        message = Some "project root: selection was cleared";
        inspector = None;
        quit_armed = false;
      }
  | Some root -> (
      match Project_root.resolve root ~relative_path:entry.relative_path with
      | Error error ->
          {
            session with
            message = Some (Error.to_string (project_root_error error));
            inspector = None;
            quit_armed = false;
          }
      | Ok path -> open_buffer session path)

let handle_file_picker_input session query selected input =
  if event_is_named input Input_event.Escape then
    {
      session with
      interaction = Idle;
      message = Some "project file picker cancelled";
      inspector = None;
      quit_armed = false;
    }
  else if event_is_named input Input_event.Arrow_up then
    file_picker_with_query session ~query ~selected:(selected - 1)
  else if event_is_named input Input_event.Arrow_down then
    file_picker_with_query session ~query ~selected:(selected + 1)
  else if event_is_named input Input_event.Backspace then
    file_picker_with_query session ~query:(drop_last_utf8 query) ~selected:0
  else if event_is_named input Input_event.Enter then
    match project_file_entries session ~query with
    | Error error ->
        {
          session with
          message = Some (Error.to_string error);
          inspector = None;
          quit_armed = false;
        }
    | Ok entries -> (
        match List.nth_opt entries selected with
        | None ->
            {
              session with
              message = Some "project file picker has no matching file";
              inspector = None;
              quit_armed = false;
            }
        | Some entry -> open_picked_file session entry)
  else
    match event_text input with
    | None -> session
    | Some text ->
        file_picker_with_query session ~query:(query ^ text) ~selected:0

let project_search_error message =
  Error.Invalid_command_arguments ("project search: " ^ message)

let search_project session ~query =
  match session.project_root with
  | None ->
      {
        session with
        message = Some "project search: select a root before searching";
        inspector = None;
        quit_armed = false;
      }
  | Some root -> (
      match
        Project_search.search root ~limits:Project_search.default_limits ~query
      with
      | Error error ->
          {
            session with
            message = Some error;
            inspector = None;
            quit_armed = false;
          }
      | Ok snapshot ->
          {
            session with
            project_search = Some snapshot;
            interaction = Project_search_view { snapshot; selected = 0 };
            message =
              Some
                (Printf.sprintf "project search: %d result%s"
                   (List.length snapshot.results)
                   (if List.length snapshot.results = 1 then "" else "s"));
            inspector = None;
            quit_armed = false;
          })

let project_search_with_selection session snapshot selected =
  let maximum = List.length snapshot.Project_search.results - 1 in
  let selected = if maximum < 0 then 0 else min (max 0 selected) maximum in
  {
    session with
    interaction = Project_search_view { snapshot; selected };
    quit_armed = false;
  }

let activate_project_search_result session snapshot selected input =
  match
    (session.project_root, List.nth_opt snapshot.Project_search.results selected)
  with
  | None, _ ->
      {
        session with
        interaction = Idle;
        message = Some "project search: selection was cleared";
        inspector = None;
        quit_armed = false;
      }
  | _, None ->
      {
        session with
        message = Some "project search has no matching result";
        inspector = None;
        quit_armed = false;
      }
  | Some root, Some result -> (
      match
        Project_search.validate_result root ~query:snapshot.query result
      with
      | Error error ->
          {
            session with
            message = Some (Error.to_string (project_search_error error));
            inspector = None;
            quit_armed = false;
          }
      | Ok path -> (
          let opened = open_buffer session path in
          if opened.file_path <> Some path then
            {
              opened with
              interaction = Idle;
              inspector = None;
              quit_armed = false;
            }
          else
            match
              Model_intent.set_selections
                ~selections:[ (result.byte_offset, result.byte_offset) ]
                ~primary:0
            with
            | Error error ->
                {
                  opened with
                  interaction = Idle;
                  message = Some (Error.to_string error);
                  inspector = None;
                  quit_armed = false;
                }
            | Ok intent ->
                let selected, _ =
                  execute_active_effects
                    ~augment_provenance:(fun provenance ->
                      Provenance.add provenance
                        (Provenance.Effect "host.project-search.open"))
                    opened input
                    [ Model_effect.Execute_intent intent ]
                in
                {
                  selected with
                  interaction = Idle;
                  message =
                    Some
                      (Printf.sprintf "project search: opened %s:%d"
                         result.relative_path result.line);
                  inspector = None;
                  quit_armed = false;
                }))

let handle_project_search_input session snapshot selected input =
  if event_is_named input Input_event.Escape then
    {
      session with
      interaction = Idle;
      message = Some "project search cancelled";
      inspector = None;
      quit_armed = false;
    }
  else if event_is_named input Input_event.Arrow_up then
    project_search_with_selection session snapshot (selected - 1)
  else if event_is_named input Input_event.Arrow_down then
    project_search_with_selection session snapshot (selected + 1)
  else if event_is_named input Input_event.Enter then
    activate_project_search_result session snapshot selected input
  else session

let invoke_host_palette_command ?(arguments = []) session input = function
  | Save -> save session
  | Save_as -> (
      if arguments = [] then
        {
          session with
          interaction = Save_as_prompt "";
          message = Some "save-as: enter a destination path";
          quit_armed = false;
          inspector = None;
        }
      else
        match required_text_argument arguments "path" with
        | Ok path -> save_to ~overwrite:true session path
        | Error error -> { session with message = Some (Error.to_string error) }
      )
  | Save_layout -> (
      match required_text_argument arguments "path" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok path -> (
          match save_layout session ~path with
          | Error error ->
              { session with message = Some (Error.to_string error) }
          | Ok () ->
              {
                session with
                interaction = Idle;
                inspector = None;
                message = Some ("workspace layout saved to " ^ path);
                quit_armed = false;
              }))
  | Restore_layout -> (
      match required_text_argument arguments "path" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok path -> (
          match restore_layout session ~path with
          | Error error ->
              { session with message = Some (Error.to_string error) }
          | Ok restored -> restored))
  | Set_project_root -> (
      match required_text_argument arguments "path" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok path -> (
          match set_project_root session ~path with
          | Error error ->
              { session with message = Some (Error.to_string error) }
          | Ok session -> session))
  | Open_file_picker -> begin_file_picker session
  | Search_project -> (
      match required_text_argument arguments "query" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok query -> search_project session ~query)
  | Reload_config -> { (reload_config session) with interaction = Idle }
  | Start_search -> begin_search session
  | Start_regexp_search -> begin_search ~kind:Regexp session
  | Replace_all_literal -> (
      match
        ( required_text_argument arguments "query",
          required_text_argument arguments "replacement" )
      with
      | Ok query, Ok replacement ->
          replace_all session input ~kind:Literal ~query ~replacement
      | Error error, _ | _, Error error ->
          { session with message = Some (Error.to_string error) })
  | Replace_all_regexp -> (
      match
        ( required_text_argument arguments "query",
          required_text_argument arguments "replacement" )
      with
      | Ok query, Ok replacement ->
          replace_all session input ~kind:Regexp ~query ~replacement
      | Error error, _ | _, Error error ->
          { session with message = Some (Error.to_string error) })
  | Start_query_replace_literal -> (
      match
        ( required_text_argument arguments "query",
          required_text_argument arguments "replacement" )
      with
      | Ok query, Ok replacement ->
          begin_query_replace session ~kind:Literal ~query ~replacement
      | Error error, _ | _, Error error ->
          { session with message = Some (Error.to_string error) })
  | Start_query_replace_regexp -> (
      match
        ( required_text_argument arguments "query",
          required_text_argument arguments "replacement" )
      with
      | Ok query, Ok replacement ->
          begin_query_replace session ~kind:Regexp ~query ~replacement
      | Error error, _ | _, Error error ->
          { session with message = Some (Error.to_string error) })
  | Search_next ->
      {
        (move_search session input 1) with
        interaction = Idle;
        inspector = None;
      }
  | Search_previous ->
      {
        (move_search session input (-1)) with
        interaction = Idle;
        inspector = None;
      }
  | Toggle_macro_recording -> (
      match optional_text_argument arguments "register" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok register ->
          {
            (toggle_macro_recording ?register session) with
            interaction = Idle;
            inspector = None;
          })
  | Replay_macro -> (
      match
        ( optional_text_argument arguments "register",
          optional_positive_int_argument arguments "count" )
      with
      | Error error, _ | _, Error error ->
          { session with message = Some (Error.to_string error) }
      | Ok register, Ok count ->
          {
            (request_macro_replay ?register ?count session) with
            interaction = Idle;
            inspector = None;
          })
  | Kill_ring_cut -> cut_to_kill_ring session input
  | Kill_ring_yank -> yank_latest_kill session input
  | System_clipboard_copy -> copy_to_system_clipboard session input
  | System_clipboard_paste -> paste_from_system_clipboard session input
  | Set_location -> (
      match required_text_argument arguments "name" with
      | Ok name ->
          {
            (set_location session name) with
            interaction = Idle;
            inspector = None;
          }
      | Error error -> { session with message = Some (Error.to_string error) })
  | Jump_location -> (
      match required_text_argument arguments "name" with
      | Ok name -> jump_to_location session input name
      | Error error -> { session with message = Some (Error.to_string error) })
  | Push_jump ->
      { (push_current_jump session) with interaction = Idle; inspector = None }
  | Jump_backward ->
      traverse_jumps session input ~direction:Model_effect.Backward ~count:1
  | Jump_forward ->
      traverse_jumps session input ~direction:Model_effect.Forward ~count:1
  | Open_palette ->
      {
        session with
        interaction = Palette { query = ""; selected = 0 };
        message = Some "command palette: filter active commands";
        quit_armed = false;
        inspector = None;
      }
  | Open_command_line ->
      {
        session with
        interaction = Command_line ":";
        message = Some "command line: enter one registered command ID";
        quit_armed = false;
        inspector = None;
      }
  | Enable_binding_layer -> (
      match required_text_argument arguments "layer" with
      | Ok id -> { (enable_binding_layer session ~id) with interaction = Idle }
      | Error error -> { session with message = Some (Error.to_string error) })
  | Disable_binding_layer -> (
      match required_text_argument arguments "layer" with
      | Ok id -> { (disable_binding_layer session ~id) with interaction = Idle }
      | Error error -> { session with message = Some (Error.to_string error) })
  | Split_vertical ->
      { (split_pane session Layout.Vertical) with interaction = Idle }
  | Split_horizontal ->
      { (split_pane session Layout.Horizontal) with interaction = Idle }
  | Focus_next_pane -> { (focus_next_pane session) with interaction = Idle }
  | Close_pane -> { (close_pane session) with interaction = Idle }
  | Only_pane -> { (only_pane session) with interaction = Idle }
  | Grow_pane_width ->
      {
        (resize_focused_pane session ~dimension:Layout.Width ~delta:1) with
        interaction = Idle;
      }
  | Shrink_pane_width ->
      {
        (resize_focused_pane session ~dimension:Layout.Width ~delta:(-1)) with
        interaction = Idle;
      }
  | Grow_pane_height ->
      {
        (resize_focused_pane session ~dimension:Layout.Height ~delta:1) with
        interaction = Idle;
      }
  | Shrink_pane_height ->
      {
        (resize_focused_pane session ~dimension:Layout.Height ~delta:(-1)) with
        interaction = Idle;
      }
  | Balance_panes -> { (balance_panes session) with interaction = Idle }
  | New_buffer -> { (new_buffer session) with interaction = Idle }
  | Open_buffer -> (
      if arguments = [] then
        {
          session with
          interaction = Open_buffer_prompt "";
          message = Some "workspace: enter a file path";
          inspector = None;
        }
      else
        match required_text_argument arguments "path" with
        | Ok path -> open_buffer session path
        | Error error -> { session with message = Some (Error.to_string error) }
      )
  | List_buffers ->
      {
        session with
        inspector = Some (buffer_lines session);
        interaction = Idle;
        message = None;
        quit_armed = false;
      }
  | Switch_buffer -> (
      match required_nonnegative_int_argument arguments "buffer-id" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok buffer_id -> switch_buffer session ~buffer_id)
  | Rename_buffer -> (
      match required_text_argument arguments "name" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok name -> rename_buffer session ~name)
  | Close_buffer -> close_buffer session
  | Force_close_buffer -> close_buffer ~force:true session
  | Next_buffer -> { (cycle_buffer session 1) with interaction = Idle }
  | Previous_buffer -> { (cycle_buffer session (-1)) with interaction = Idle }
  | View_scroll_up -> scroll_pane session session.focused_pane ~lines:(-1)
  | View_scroll_down -> scroll_pane session session.focused_pane ~lines:1
  | View_page_up -> scroll_pane_pages session session.focused_pane ~pages:(-1)
  | View_page_down -> scroll_pane_pages session session.focused_pane ~pages:1
  | View_center -> center_pane_viewport session session.focused_pane
  | Switch_model ->
      let current =
        model_choices session
        |> List.find_index (fun candidate -> candidate = model session)
        |> Option.value ~default:0
      in
      {
        session with
        interaction = Model_picker current;
        message = Some "model switch: choose 1, 2, or 3";
        quit_armed = false;
        inspector = None;
      }
  | Help ->
      {
        session with
        interaction = Help_view;
        message = None;
        quit_armed = false;
        inspector = None;
      }
  | Switch_presentation -> (
      match required_text_argument arguments "profile" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok profile -> set_presentation session ~profile)
  | Switch_theme -> (
      match required_text_argument arguments "theme" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok theme -> set_theme session ~theme)
  | Background_jobs ->
      {
        session with
        inspector = Some (background_job_lines session);
        interaction = Idle;
        message = None;
        quit_armed = false;
      }
  | Cancel_background_job -> (
      match required_positive_int_argument arguments "job-id" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok job_id -> cancel_background_job session ~job_id)
  | Open_background_job_output -> (
      match required_positive_int_argument arguments "job-id" with
      | Error error -> { session with message = Some (Error.to_string error) }
      | Ok job_id -> open_background_job_output session ~job_id)
  | Language_status ->
      {
        session with
        inspector = Some (language_status_lines session);
        interaction = Idle;
        message = None;
      }
  | Language_restart -> (
      match session.language_client with
      | None -> language_unavailable session
      | Some client ->
          Lsp.set_execution_id client
            ~execution_id:
              (Option.value ~default:0
                 (last_execution_of_active session.active));
          Lsp.restart client;
          {
            session with
            diagnostics = [];
            message = Some "language server restart requested";
          })
  | Language_hover -> begin_hover session
  | Language_definition -> begin_definition session
  | Language_complete -> begin_completion session
  | Language_rename -> begin_rename session
  | Language_diagnostic_next -> move_to_diagnostic session input 1
  | Language_diagnostic_previous -> move_to_diagnostic session input (-1)
  | Language_diagnostic_describe_current -> describe_diagnostic session
  | Quit | Force_quit ->
      {
        session with
        interaction = Idle;
        message =
          Some
            "quit commands are intentionally available through Ctrl-Q so the \
             terminal loop can exit safely";
      }

let invoke_palette_item_with_arguments session input (item : palette_item)
    arguments =
  match item.action with
  | Invoke_command id ->
      let invocation =
        Command_invocation.create ~id ~arguments |> Result.get_ok
      in
      let next, _ =
        execute_active_effects
          ~augment_provenance:(fun provenance ->
            Provenance.add provenance (Provenance.Effect "host.command-palette"))
          session input
          [ Model_effect.Invoke_command invocation ]
      in
      { next with interaction = Idle; inspector = None }
  | Invoke_host_command command ->
      invoke_host_palette_command ~arguments session input command

let invoke_palette_item session input (item : palette_item) =
  match Command_descriptor.parameters item.descriptor with
  | [] -> invoke_palette_item_with_arguments session input item []
  | _ ->
      begin_command_prompt session ~action:(Palette_item item)
        ~descriptor:item.descriptor

let maximum_command_line_bytes = 4096

let command_line_tokens line =
  if String.length line = 0 || line.[0] <> ':' then
    Error
      (Error.Invalid_command_arguments
         "command line must begin with a colon and an exact command ID")
  else
    String.sub line 1 (String.length line - 1)
    |> String.split_on_char ' '
    |> List.filter (fun value -> String.length value > 0)
    |> function
    | [] ->
        Error
          (Error.Invalid_command_arguments
             "command line requires an exact registered command ID")
    | command :: arguments -> Ok (command, arguments)

let command_line_arguments descriptor values =
  let rec collect arguments parameters values =
    match (parameters, values) with
    | [], [] -> Ok (List.rev arguments)
    | [], _ :: _ ->
        Error
          (Error.Invalid_command_arguments
             "command line has more arguments than the descriptor declares")
    | parameter :: _, [] when parameter.Command_descriptor.required ->
        Error
          (Error.Invalid_command_arguments
             ("command line requires argument " ^ parameter.name))
    | _ :: rest, [] -> collect arguments rest []
    | parameter :: rest, value :: values ->
        Result.bind (command_argument_of_text parameter value) (fun argument ->
            collect (argument :: arguments) rest values)
  in
  collect [] (Command_descriptor.parameters descriptor) values

let run_command_line session input line =
  Result.bind (command_line_tokens line) (fun (id, values) ->
      let items =
        palette_items session
        |> List.filter (fun (item : palette_item) -> String.equal item.id id)
        |> List.filter (fun (item : palette_item) ->
            not (String.equal item.id "editor.command-line"))
      in
      match items with
      | [] ->
          Error
            (Error.Invalid_command_arguments
               ("command line does not name an active command: " ^ id))
      | [ item ] ->
          command_line_arguments item.descriptor values
          |> Result.map (fun arguments -> (item, arguments))
      | _ :: _ :: _ ->
          Error
            (Error.Invalid_command_arguments
               ("command line is ambiguous for command ID: " ^ id)))
  |> function
  | Ok (item, arguments) ->
      invoke_palette_item_with_arguments session input item arguments
  | Error error ->
      {
        session with
        interaction = Command_line line;
        message = Some ("command line: " ^ Error.to_string error);
        inspector = None;
        quit_armed = false;
      }

let input_for_interaction session input =
  match session.interaction with
  | Idle -> (
      match matching_binding session input with
      | Binding_prefix sequence ->
          {
            session with
            pending_binding = sequence;
            message =
              Some
                ("binding prefix: "
                ^ Input_event.binding_sequence_to_string sequence);
            quit_armed = false;
          }
      | Binding_cancelled ->
          let session =
            { session with pending_binding = []; quit_armed = false }
          in
          if session.active_modes <> [] then pop_active_mode session
          else { session with message = Some "binding prefix cancelled" }
      | Binding_rejected sequence ->
          {
            session with
            pending_binding = [];
            message =
              Some
                ("unbound binding sequence: "
                ^ Input_event.binding_sequence_to_string sequence);
            quit_armed = false;
          }
      | Binding_resolved (binding, text) ->
          let arguments =
            match (Scripting.binding_text_argument binding, text) with
            | Some name, Some text ->
                [
                  Command_argument.make ~name
                    ~value:(Command_argument.Text text)
                  |> Result.get_ok;
                ]
            | None, None -> []
            | Some _, None | None, Some _ -> []
          in
          fst
            (invoke_bound_command ~arguments
               (transition_binding_mode
                  { session with pending_binding = [] }
                  binding)
               input binding)
      | No_binding ->
          if
            is_shortcut input ~text:"f" ~modifiers:[ Input_event.Control ]
            && model session <> Direct
          then begin_search session
          else if
            is_shortcut input ~text:"g"
              ~modifiers:[ Input_event.Shift; Input_event.Control ]
            && model session <> Direct
          then move_search session input (-1)
          else if
            is_shortcut input ~text:"g" ~modifiers:[ Input_event.Control ]
            && model session <> Direct
          then move_search session input 1
          else if
            is_shortcut input ~text:" " ~modifiers:[ Input_event.Control ]
            || is_shortcut input ~text:"\000" ~modifiers:[ Input_event.Control ]
          then begin_completion session
          else if
            is_shortcut input ~text:"p" ~modifiers:[ Input_event.Control ]
            && model session <> Direct
          then
            {
              session with
              interaction = Palette { query = ""; selected = 0 };
              message = Some "command palette: filter active commands";
              inspector = None;
            }
          else if
            is_shortcut input ~text:"s"
              ~modifiers:[ Input_event.Shift; Input_event.Control ]
          then
            {
              session with
              interaction = Save_as_prompt "";
              message = Some "save-as: enter a destination path";
              inspector = None;
            }
          else if
            is_shortcut input ~text:"m" ~modifiers:[ Input_event.Alt ]
            || is_shortcut input ~text:"m" ~modifiers:[ Input_event.Meta ]
          then
            let selected =
              model_choices session
              |> List.find_index (fun candidate -> candidate = model session)
              |> Option.value ~default:0
            in
            {
              session with
              interaction = Model_picker selected;
              inspector = None;
            }
          else if
            is_shortcut input ~text:"h" ~modifiers:[ Input_event.Alt ]
            || is_shortcut input ~text:"h" ~modifiers:[ Input_event.Meta ]
          then { session with interaction = Help_view; inspector = None }
          else
            let session, effects = handle_model_input session input in
            List.fold_left
              (fun session request ->
                handle_model_host_request session input request)
              session effects)
  | Search_prompt { kind; query; origin; direction } -> (
      if
        is_shortcut input ~text:"g"
          ~modifiers:[ Input_event.Shift; Input_event.Control ]
      then move_search session input (-1)
      else if is_shortcut input ~text:"g" ~modifiers:[ Input_event.Control ]
      then move_search session input 1
      else if event_is_named input Input_event.Escape then
        restore_search_origin session input origin
      else if event_is_named input Input_event.Enter then
        { session with interaction = Idle; message = Some "search complete" }
      else if event_is_named input Input_event.Backspace then
        let next_query = drop_last_utf8 query in
        let updated =
          update_search session input ~kind ~origin ~direction next_query
        in
        {
          updated with
          interaction =
            Search_prompt { kind; query = next_query; origin; direction };
        }
      else
        match event_text input with
        | None -> session
        | Some text ->
            let next_query = query ^ text in
            let updated =
              update_search session input ~kind ~origin ~direction next_query
            in
            {
              updated with
              interaction =
                Search_prompt { kind; query = next_query; origin; direction };
            })
  | Palette { query; selected } -> (
      let items = matching_palette_items session query in
      if event_is_named input Input_event.Escape then
        {
          session with
          interaction = Idle;
          message = Some "command palette cancelled";
        }
      else if event_is_named input Input_event.Arrow_up then
        let selected = if selected <= 0 then 0 else selected - 1 in
        { session with interaction = Palette { query; selected } }
      else if event_is_named input Input_event.Arrow_down then
        let selected = min (max 0 (List.length items - 1)) (selected + 1) in
        { session with interaction = Palette { query; selected } }
      else if event_is_named input Input_event.Enter then
        match List.nth_opt items selected with
        | None ->
            {
              session with
              message = Some "command palette: no matching command";
            }
        | Some item -> invoke_palette_item session input item
      else if event_is_named input Input_event.Backspace then
        let query = drop_last_utf8 query in
        { session with interaction = Palette { query; selected = 0 } }
      else
        match event_text input with
        | None -> session
        | Some text ->
            {
              session with
              interaction = Palette { query = query ^ text; selected = 0 };
            })
  | Command_line line -> (
      if event_is_named input Input_event.Escape then
        {
          session with
          interaction = Idle;
          message = Some "command line cancelled";
          inspector = None;
          quit_armed = false;
        }
      else if event_is_named input Input_event.Enter then
        run_command_line session input line
      else if event_is_named input Input_event.Backspace then
        {
          session with
          interaction = Command_line (drop_last_utf8 line);
          quit_armed = false;
        }
      else
        match event_text input with
        | None -> session
        | Some value ->
            let next = line ^ value in
            if String.length next > maximum_command_line_bytes then
              {
                session with
                message =
                  Some
                    (Printf.sprintf "command line is limited to %d bytes"
                       maximum_command_line_bytes);
                quit_armed = false;
              }
            else
              {
                session with
                interaction = Command_line next;
                quit_armed = false;
              })
  | Command_prompt { action; descriptor; remaining; arguments_rev; text } -> (
      match remaining with
      | [] -> { session with interaction = Idle }
      | parameter :: rest -> (
          if event_is_named input Input_event.Escape then
            {
              session with
              interaction = Idle;
              message = Some "command argument prompt cancelled";
            }
          else if event_is_named input Input_event.Enter then
            if String.length text = 0 && parameter.required then
              {
                session with
                message =
                  Some ("command argument " ^ parameter.name ^ " is required");
              }
            else
              let argument =
                if String.length text = 0 then Ok None
                else
                  command_argument_of_text parameter text
                  |> Result.map Option.some
              in
              match argument with
              | Error error ->
                  { session with message = Some (Error.to_string error) }
              | Ok argument -> (
                  let arguments_rev =
                    match argument with
                    | None -> arguments_rev
                    | Some argument -> argument :: arguments_rev
                  in
                  match rest with
                  | next :: _ ->
                      {
                        session with
                        interaction =
                          Command_prompt
                            {
                              action;
                              descriptor;
                              remaining = rest;
                              arguments_rev;
                              text = "";
                            };
                        message = Some (command_prompt_message descriptor next);
                      }
                  | [] -> (
                      let arguments = List.rev arguments_rev in
                      match action with
                      | Palette_item item ->
                          invoke_palette_item_with_arguments session input item
                            arguments
                      | Bound_command binding ->
                          fst
                            (invoke_bound_command ~arguments
                               { session with interaction = Idle }
                               input binding)))
          else if event_is_named input Input_event.Backspace then
            {
              session with
              interaction =
                Command_prompt
                  {
                    action;
                    descriptor;
                    remaining;
                    arguments_rev;
                    text = drop_last_utf8 text;
                  };
            }
          else
            match event_text input with
            | None -> session
            | Some value ->
                {
                  session with
                  interaction =
                    Command_prompt
                      {
                        action;
                        descriptor;
                        remaining;
                        arguments_rev;
                        text = text ^ value;
                      };
                }))
  | Save_as_prompt path -> (
      if event_is_named input Input_event.Escape then
        { session with interaction = Idle; message = Some "save-as cancelled" }
      else if event_is_named input Input_event.Enter then
        if String.length path = 0 then
          { session with message = Some "save-as: destination path is empty" }
        else save_to ~overwrite:true session path
      else if event_is_named input Input_event.Backspace then
        { session with interaction = Save_as_prompt (drop_last_utf8 path) }
      else
        match event_text input with
        | None -> session
        | Some text ->
            { session with interaction = Save_as_prompt (path ^ text) })
  | Open_buffer_prompt path -> (
      if event_is_named input Input_event.Escape then
        {
          session with
          interaction = Idle;
          message = Some "workspace: open cancelled";
        }
      else if event_is_named input Input_event.Enter then
        open_buffer session path
      else if event_is_named input Input_event.Backspace then
        { session with interaction = Open_buffer_prompt (drop_last_utf8 path) }
      else
        match event_text input with
        | None -> session
        | Some text ->
            { session with interaction = Open_buffer_prompt (path ^ text) })
  | File_picker { query; selected } ->
      handle_file_picker_input session query selected input
  | Project_search_view { snapshot; selected } ->
      handle_project_search_input session snapshot selected input
  | Query_replace state -> handle_query_replace_input session state input
  | Model_picker selected -> (
      if event_is_named input Input_event.Escape then
        {
          session with
          interaction = Idle;
          message = Some "model switch cancelled";
        }
      else if event_is_named input Input_event.Arrow_up then
        { session with interaction = Model_picker (max 0 (selected - 1)) }
      else if event_is_named input Input_event.Arrow_down then
        {
          session with
          interaction =
            Model_picker
              (min (List.length (model_choices session) - 1) (selected + 1));
        }
      else if event_is_named input Input_event.Enter then
        switch_to_model session (List.nth (model_choices session) selected)
      else
        match event_text input with
        | Some "1" -> switch_to_model session Vim
        | Some "2" -> switch_to_model session Selection
        | Some "3" -> switch_to_model session Structural
        | Some "4" -> switch_to_model session Direct
        | Some "5" when List.mem Script (model_choices session) ->
            switch_to_model session Script
        | Some _ | None -> session)
  | Help_view ->
      if
        event_is_named input Input_event.Escape
        || event_is_named input Input_event.Enter
      then { session with interaction = Idle }
      else session
  | Hover_view _ ->
      if
        event_is_named input Input_event.Escape
        || event_is_named input Input_event.Enter
      then { session with interaction = Idle; message = None }
      else session
  | Completion_view { items; selected; query } -> (
      let visible = matching_completion_items items query in
      if event_is_named input Input_event.Escape then
        {
          session with
          interaction = Idle;
          message = Some "completion cancelled";
        }
      else if event_is_named input Input_event.Arrow_up then
        {
          session with
          interaction =
            Completion_view { items; selected = max 0 (selected - 1); query };
        }
      else if event_is_named input Input_event.Arrow_down then
        {
          session with
          interaction =
            Completion_view
              {
                items;
                selected =
                  (if visible = [] then 0
                   else min (List.length visible - 1) (selected + 1));
                query;
              };
        }
      else if event_is_named input Input_event.Enter then
        match List.nth_opt visible selected with
        | None ->
            {
              session with
              interaction = Idle;
              message = Some "completion filter has no matching item";
            }
        | Some item -> accept_completion session input item
      else if event_is_named input Input_event.Backspace then
        {
          session with
          interaction =
            Completion_view
              { items; selected = 0; query = drop_last_utf8 query };
        }
      else
        match event_text input with
        | None -> session
        | Some text ->
            {
              session with
              interaction =
                Completion_view { items; selected = 0; query = query ^ text };
            })
  | Rename_prompt name -> (
      if event_is_named input Input_event.Escape then
        { session with interaction = Idle; message = Some "rename cancelled" }
      else if event_is_named input Input_event.Enter then
        if String.length name = 0 then
          { session with message = Some "rename: new name is empty" }
        else
          let next =
            request_language session (fun client ->
                Lsp.request_rename client ~byte_offset:(primary_offset session)
                  ~new_name:name)
          in
          { next with interaction = Idle }
      else if event_is_named input Input_event.Backspace then
        { session with interaction = Rename_prompt (drop_last_utf8 name) }
      else
        match event_text input with
        | None -> session
        | Some text ->
            { session with interaction = Rename_prompt (name ^ text) })

let pane_at session ~column ~row =
  let row = row - buffer_line_rows session in
  if row < 0 then None
  else
    layout_bounds session
    |> List.find_map (fun (pane, rectangle) ->
        if
          column >= rectangle.Layout.x
          && column < rectangle.x + rectangle.width
          && row >= rectangle.y
          && row < rectangle.y + rectangle.height
        then Some (pane, rectangle)
        else None)

let divider_target session ~column ~row =
  let row = row - buffer_line_rows session in
  let on_status_row =
    match Zenbu_view.Presentation.status_line session.presentation with
    | Zenbu_view.Presentation.Hidden_status -> false
    | Zenbu_view.Presentation.Detailed | Zenbu_view.Presentation.Minimal ->
        layout_bounds session
        |> List.exists (fun (_, rectangle) ->
            rectangle.Layout.height > 0
            && row = rectangle.y + rectangle.height - 1)
  in
  if row < 0 || on_status_row then None
  else
    Layout.divider_at session.layout ~column ~row
      ~width:session.dimensions.columns ~height:(workspace_height session)

let drag_divider session divider ~column ~row =
  let row = row - buffer_line_rows session in
  if
    column < 0
    || column >= session.dimensions.columns
    || row < 0
    || row >= workspace_height session
  then session
  else
    match
      Layout.drag_divider session.layout divider ~column ~row
        ~width:session.dimensions.columns ~height:(workspace_height session)
    with
    | None -> { session with mouse_drag = None }
    | Some layout ->
        {
          session with
          layout;
          message = None;
          inspector = None;
          quit_armed = false;
        }

let pointer_target session ~column ~row =
  match pane_at session ~column ~row with
  | None -> None
  | Some (pane, rectangle) -> (
      let local_row = row - buffer_line_rows session - rectangle.Layout.y in
      let status_rows =
        match Zenbu_view.Presentation.status_line session.presentation with
        | Zenbu_view.Presentation.Hidden_status -> 0
        | Zenbu_view.Presentation.Detailed | Zenbu_view.Presentation.Minimal ->
            1
      in
      if local_row < 0 || local_row >= rectangle.height - status_rows then None
      else
        match buffer_for_id session (buffer_id_for_pane session pane) with
        | None -> None
        | Some buffer ->
            let contents =
              Editor_context.contents (context_of_active buffer.active)
            in
            let source_lines = Zenbu_view.Display.source_lines contents in
            let source_line =
              List.nth_opt source_lines
                ((pane_viewport session pane).top_line + local_row)
              |> Option.value ~default:(List.hd (List.rev source_lines))
            in
            let line = Zenbu_view.Display.layout contents source_line in
            let column =
              (pane_viewport session pane).left_column + column - rectangle.x
              - Zenbu_view.Renderer.gutter_width session.presentation
                  source_lines rectangle.width
            in
            Some (pane, Zenbu_view.Display.offset_at_column line column))

let trace_pointer session =
  let execution_id =
    Option.value ~default:0 (last_execution_of_active session.active)
  in
  trace_runtime_events
    (trace_of_active session.active)
    (profiler_of_active session.active)
    ~execution_id session.plugins;
  session

let cancel_language_for_pointer session =
  Option.iter
    (fun client ->
      Lsp.set_execution_id client
        ~execution_id:
          (Option.value ~default:0 (last_execution_of_active session.active));
      List.iter (Lsp.cancel client)
        [ Lsp.Hover; Lsp.Completion; Lsp.Definition ])
    session.language_client;
  session

let apply_pointer_selection session input ~pane ~anchor_offset ~head_offset
    ~mouse_drag =
  let session = focus_pane session pane in
  match
    Model_intent.set_selections
      ~selections:[ (anchor_offset, head_offset) ]
      ~primary:0
  with
  | Error error ->
      {
        session with
        mouse_drag = None;
        message = Some ("mouse selection rejected: " ^ Error.to_string error);
      }
  | Ok intent ->
      let next, _ =
        execute_active_effects
          ~augment_provenance:(fun provenance ->
            Provenance.add provenance (Provenance.Effect "host.mouse.select"))
          session input
          [ Model_effect.Execute_intent intent ]
      in
      {
        next with
        mouse_drag;
        interaction = Idle;
        inspector = None;
        message = None;
        quit_armed = false;
      }
      |> fun session ->
      set_pane_viewport session pane
        (Zenbu_view.Viewport.follow (pane_viewport session pane))
      |> cancel_language_for_pointer |> trace_pointer

let scroll_pointer_pane session pane delta =
  scroll_pane session pane ~lines:delta

let handle_pointer session input =
  let session = { session with pending_binding = [] } in
  match
    (session.inspector, session.interaction, Input_event.mouse_action input)
  with
  | Some _, _, _
  | ( None,
      ( Search_prompt _ | Palette _ | Command_line _ | Command_prompt _
      | Save_as_prompt _ | Open_buffer_prompt _ | File_picker _
      | Project_search_view _ | Query_replace _ | Model_picker _ | Help_view
      | Hover_view _ | Completion_view _ | Rename_prompt _ ),
      _ ) ->
      { session with mouse_drag = None }
  | None, Idle, None -> session
  | None, Idle, Some (Input_event.Press Input_event.Wheel_up) -> (
      match Input_event.mouse_position input with
      | Some (column, row) -> (
          match pane_at session ~column ~row with
          | Some (pane, _) -> scroll_pointer_pane session pane (-3)
          | None -> session)
      | None -> session)
  | None, Idle, Some (Input_event.Press Input_event.Wheel_down) -> (
      match Input_event.mouse_position input with
      | Some (column, row) -> (
          match pane_at session ~column ~row with
          | Some (pane, _) -> scroll_pointer_pane session pane 3
          | None -> session)
      | None -> session)
  | None, Idle, Some (Input_event.Press Input_event.Primary) -> (
      match Input_event.mouse_position input with
      | None -> session
      | Some (column, row) -> (
          match divider_target session ~column ~row with
          | Some divider ->
              {
                session with
                mouse_drag = Some (Divider_drag divider);
                interaction = Idle;
                inspector = None;
                message = None;
                quit_armed = false;
              }
          | None -> (
              match pointer_target session ~column ~row with
              | None -> { session with mouse_drag = None }
              | Some (pane, offset) ->
                  let focused = focus_pane session pane in
                  let selections =
                    Editor_context.selections (context focused)
                  in
                  let primary =
                    List.nth selections.selections selections.primary_index
                  in
                  let anchor_offset =
                    if List.mem Input_event.Shift (Input_event.modifiers input)
                    then primary.anchor_offset
                    else offset
                  in
                  apply_pointer_selection focused input ~pane ~anchor_offset
                    ~head_offset:offset
                    ~mouse_drag:(Some (Selection_drag { pane; anchor_offset })))
          ))
  | None, Idle, Some Input_event.Drag -> (
      match (session.mouse_drag, Input_event.mouse_position input) with
      | Some (Divider_drag divider), Some (column, row) ->
          drag_divider session divider ~column ~row
      | Some (Selection_drag { pane; anchor_offset }), Some (column, row) -> (
          match pointer_target session ~column ~row with
          | Some (target_pane, offset) when target_pane = pane ->
              apply_pointer_selection session input ~pane ~anchor_offset
                ~head_offset:offset
                ~mouse_drag:(Some (Selection_drag { pane; anchor_offset }))
          | Some _ | None -> session)
      | None, Some _ | _, None -> session)
  | None, Idle, Some Input_event.Release -> (
      match (session.mouse_drag, Input_event.mouse_position input) with
      | Some (Divider_drag _), _ -> { session with mouse_drag = None }
      | Some (Selection_drag { pane; anchor_offset }), Some (column, row) -> (
          match pointer_target session ~column ~row with
          | Some (target_pane, offset) when target_pane = pane ->
              apply_pointer_selection session input ~pane ~anchor_offset
                ~head_offset:offset ~mouse_drag:None
          | Some _ | None -> { session with mouse_drag = None })
      | None, Some _ | _, None -> { session with mouse_drag = None })
  | ( None,
      Idle,
      Some
        ( Input_event.Press Input_event.Middle
        | Input_event.Press Input_event.Secondary ) ) ->
      { session with mouse_drag = None }

let rec handle_input session input =
  let was_recording = Option.is_some session.macro_recording in
  let session = { session with macro_control = false } in
  let contents_before = Editor_context.contents (context session) in
  let caret_before = primary_offset session in
  let next = input_for_interaction session input in
  let completed =
    if String.equal contents_before (Editor_context.contents (context next))
    then next
    else run_event_hooks next Scripting.Document_changed input
  in
  let completed =
    if
      String.equal contents_before (Editor_context.contents (context completed))
    then completed
    else refresh_search_after_document_change completed
  in
  let completed =
    if
      String.equal contents_before (Editor_context.contents (context completed))
    then completed
    else
      synchronize_language_after_change completed
        ~fallback_contents:contents_before
  in
  let completed =
    if primary_offset completed = caret_before then completed
    else (
      Option.iter
        (fun client ->
          Lsp.set_execution_id client
            ~execution_id:
              (Option.value ~default:0
                 (last_execution_of_active completed.active));
          List.iter (Lsp.cancel client)
            [ Lsp.Hover; Lsp.Completion; Lsp.Definition ])
        completed.language_client;
      completed)
  in
  let completed =
    completed |> observe_language_document_version
    |> synchronize_workspace_documents |> synchronize_kill_ring_from_active
    |> refresh_locations |> capture_focused_view_position
    |> refresh_pane_view_positions
  in
  let completed =
    set_pane_viewport completed completed.focused_pane
      (Zenbu_view.Viewport.follow
         (pane_viewport completed completed.focused_pane))
  in
  let completed =
    if
      was_recording
      && (not session.macro_replaying)
      && session.interaction = Idle
      && completed.interaction = Idle
      && Option.is_some completed.macro_recording
      && not completed.macro_control
    then record_macro_input completed input
    else completed
  in
  let completed =
    if Option.is_some completed.macro_replay_pending then
      replay_requested_macro completed
    else completed
  in
  let execution_id =
    Option.value ~default:0 (last_execution_of_active completed.active)
  in
  trace_runtime_events
    (trace_of_active completed.active)
    (profiler_of_active completed.active)
    ~execution_id completed.plugins;
  completed

and replay_requested_macro session =
  match session.macro_replay_pending with
  | None ->
      {
        session with
        message = Some "macro replay rejected: no requested register";
      }
  | Some (register, count) -> replay_macro session register count

and replay_macro session register count =
  match find_macro session register with
  | None ->
      {
        session with
        macro_replay_pending = None;
        message =
          Some ("macro replay rejected: register " ^ register ^ " is empty");
      }
  | Some _ when session.macro_replaying ->
      {
        session with
        macro_replay_pending = None;
        message = Some "macro replay rejected: recursive replay is disabled";
      }
  | Some inputs ->
      let replaying =
        {
          session with
          macro_replay_pending = None;
          macro_replaying = true;
          macro_control = false;
          pending_binding = [];
        }
      in
      let rec repeat remaining current =
        if remaining = 0 then current
        else repeat (remaining - 1) (List.fold_left handle_input current inputs)
      in
      let completed = repeat count replaying in
      {
        completed with
        macro_replay_pending = None;
        macro_replaying = false;
        macro_control = false;
        message =
          Some
            (Printf.sprintf
               "macro replayed from %s: %d iteration(s), %d keyboard inputs"
               register count
               (count * List.length inputs));
      }

let handle_host session = function
  | Save -> Continue (save session)
  | Save_as ->
      Continue
        {
          session with
          interaction = Save_as_prompt "";
          message = Some "save-as: enter a destination path";
          quit_armed = false;
          inspector = None;
        }
  | Save_layout | Restore_layout ->
      Continue
        {
          session with
          interaction = Idle;
          message =
            Some
              "workspace layout commands require a path through the command \
               palette";
          quit_armed = false;
          inspector = None;
        }
  | Set_project_root ->
      Continue
        {
          session with
          interaction = Idle;
          message =
            Some
              "project-root selection requires a path through the command \
               palette";
          quit_armed = false;
          inspector = None;
        }
  | Open_file_picker -> Continue (begin_file_picker session)
  | Search_project ->
      Continue
        {
          session with
          interaction = Idle;
          message =
            Some "project search requires a query through the command palette";
          quit_armed = false;
          inspector = None;
        }
  | Reload_config -> Continue (reload_config session)
  | Start_search -> Continue { (begin_search session) with quit_armed = false }
  | Start_regexp_search ->
      Continue { (begin_search ~kind:Regexp session) with quit_armed = false }
  | Replace_all_literal | Replace_all_regexp | Start_query_replace_literal
  | Start_query_replace_regexp ->
      Continue
        {
          session with
          message =
            Some
              "replacement commands require query and replacement through the \
               command palette";
          quit_armed = false;
          inspector = None;
        }
  | Search_next ->
      Continue
        (move_search session
           (Input_event.key_press (Input_event.named_key Input_event.Enter))
           1)
  | Search_previous ->
      Continue
        (move_search session
           (Input_event.key_press (Input_event.named_key Input_event.Enter))
           (-1))
  | Toggle_macro_recording -> Continue (toggle_macro_recording session)
  | Replay_macro -> Continue (replay_macro session default_macro_register 1)
  | Kill_ring_cut ->
      Continue
        (cut_to_kill_ring session
           (Input_event.key_press (Input_event.named_key Input_event.Enter)))
  | Kill_ring_yank ->
      Continue
        (yank_latest_kill session
           (Input_event.key_press (Input_event.named_key Input_event.Enter)))
  | System_clipboard_copy ->
      Continue
        (copy_to_system_clipboard session
           (Input_event.key_press (Input_event.named_key Input_event.Enter)))
  | System_clipboard_paste ->
      Continue
        (paste_from_system_clipboard session
           (Input_event.key_press (Input_event.named_key Input_event.Enter)))
  | Set_location | Jump_location ->
      Continue
        {
          session with
          message =
            Some "location commands require a name through the command palette";
          quit_armed = false;
          inspector = None;
        }
  | Push_jump -> Continue (push_current_jump session)
  | Jump_backward ->
      Continue
        (traverse_jumps session
           (Input_event.key_press (Input_event.named_key Input_event.Enter))
           ~direction:Model_effect.Backward ~count:1)
  | Jump_forward ->
      Continue
        (traverse_jumps session
           (Input_event.key_press (Input_event.named_key Input_event.Enter))
           ~direction:Model_effect.Forward ~count:1)
  | Open_palette ->
      Continue
        {
          session with
          interaction = Palette { query = ""; selected = 0 };
          message = Some "command palette: filter active commands";
          quit_armed = false;
          inspector = None;
        }
  | Open_command_line ->
      Continue
        {
          session with
          interaction = Command_line ":";
          message = Some "command line: enter one registered command ID";
          quit_armed = false;
          inspector = None;
        }
  | Enable_binding_layer | Disable_binding_layer ->
      Continue
        {
          session with
          interaction = Idle;
          message =
            Some
              "binding-layer commands require a layer through the command \
               palette";
          quit_armed = false;
          inspector = None;
        }
  | Split_vertical -> Continue (split_pane session Layout.Vertical)
  | Split_horizontal -> Continue (split_pane session Layout.Horizontal)
  | Focus_next_pane -> Continue (focus_next_pane session)
  | Close_pane -> Continue (close_pane session)
  | Only_pane -> Continue (only_pane session)
  | Grow_pane_width ->
      Continue (resize_focused_pane session ~dimension:Layout.Width ~delta:1)
  | Shrink_pane_width ->
      Continue (resize_focused_pane session ~dimension:Layout.Width ~delta:(-1))
  | Grow_pane_height ->
      Continue (resize_focused_pane session ~dimension:Layout.Height ~delta:1)
  | Shrink_pane_height ->
      Continue
        (resize_focused_pane session ~dimension:Layout.Height ~delta:(-1))
  | Balance_panes -> Continue (balance_panes session)
  | New_buffer -> Continue (new_buffer session)
  | Open_buffer ->
      Continue
        {
          session with
          interaction = Open_buffer_prompt "";
          message = Some "workspace: enter a file path";
          inspector = None;
        }
  | List_buffers ->
      Continue
        {
          session with
          inspector = Some (buffer_lines session);
          interaction = Idle;
          message = None;
          quit_armed = false;
        }
  | Switch_buffer ->
      Continue
        {
          session with
          message =
            Some
              "switching buffers requires a buffer id through the command \
               palette";
          quit_armed = false;
          inspector = None;
        }
  | Rename_buffer ->
      Continue
        {
          session with
          message =
            Some "renaming a buffer requires a name through the command palette";
          quit_armed = false;
          inspector = None;
        }
  | Close_buffer -> Continue (close_buffer session)
  | Force_close_buffer -> Continue (close_buffer ~force:true session)
  | Next_buffer -> Continue (cycle_buffer session 1)
  | Previous_buffer -> Continue (cycle_buffer session (-1))
  | View_scroll_up ->
      Continue (scroll_pane session session.focused_pane ~lines:(-1))
  | View_scroll_down ->
      Continue (scroll_pane session session.focused_pane ~lines:1)
  | View_page_up ->
      Continue (scroll_pane_pages session session.focused_pane ~pages:(-1))
  | View_page_down ->
      Continue (scroll_pane_pages session session.focused_pane ~pages:1)
  | View_center -> Continue (center_pane_viewport session session.focused_pane)
  | Switch_model ->
      let current =
        model_choices session
        |> List.find_index (fun candidate -> candidate = model session)
        |> Option.value ~default:0
      in
      Continue
        {
          session with
          interaction = Model_picker current;
          message = Some "model switch: choose 1, 2, or 3";
          quit_armed = false;
          inspector = None;
        }
  | Help ->
      Continue
        {
          session with
          interaction = Help_view;
          message = None;
          quit_armed = false;
          inspector = None;
        }
  | Switch_presentation ->
      Continue
        {
          session with
          message =
            Some
              "presentation switching requires a profile through the command \
               palette";
          quit_armed = false;
          inspector = None;
        }
  | Switch_theme ->
      Continue
        {
          session with
          message =
            Some "theme switching requires a theme through the command palette";
          quit_armed = false;
          inspector = None;
        }
  | Background_jobs ->
      Continue
        {
          session with
          inspector = Some (background_job_lines session);
          interaction = Idle;
          message = None;
          quit_armed = false;
        }
  | Cancel_background_job ->
      Continue
        {
          session with
          message =
            Some
              "background job cancellation requires a job id through the \
               command palette";
          quit_armed = false;
          inspector = None;
        }
  | Open_background_job_output ->
      Continue
        {
          session with
          message =
            Some
              "opening background-job output requires a job id through the \
               command palette";
          quit_armed = false;
          inspector = None;
        }
  | Language_status ->
      Continue
        {
          session with
          inspector = Some (language_status_lines session);
          interaction = Idle;
          message = None;
        }
  | Language_restart -> (
      match session.language_client with
      | None -> Continue (language_unavailable session)
      | Some client ->
          Lsp.set_execution_id client
            ~execution_id:
              (Option.value ~default:0
                 (last_execution_of_active session.active));
          Lsp.restart client;
          Continue
            {
              session with
              diagnostics = [];
              message = Some "language server restart requested";
            })
  | Language_hover -> Continue (begin_hover session)
  | Language_definition -> Continue (begin_definition session)
  | Language_complete -> Continue (begin_completion session)
  | Language_rename -> Continue (begin_rename session)
  | Language_diagnostic_next ->
      Continue
        (move_to_diagnostic session
           (Input_event.key_press (Input_event.named_key Input_event.Enter))
           1)
  | Language_diagnostic_previous ->
      Continue
        (move_to_diagnostic session
           (Input_event.key_press (Input_event.named_key Input_event.Enter))
           (-1))
  | Language_diagnostic_describe_current ->
      Continue (describe_diagnostic session)
  | Force_quit -> Exit session
  | Quit when not (dirty session) -> Exit session
  | Quit when session.quit_armed -> Exit session
  | Quit ->
      Continue
        {
          session with
          message = Some "unsaved changes: press Ctrl-Q again to force quit";
          quit_armed = true;
        }

let resize session ~columns ~rows =
  {
    session with
    dimensions =
      { Zenbu_view.Renderer.columns = max 0 columns; rows = max 0 rows };
  }

let syntax_spans session =
  match Editor_context.syntax (context session) with
  | None -> []
  | Some snapshot ->
      Syntax.Highlight.spans snapshot
      |> List.map (fun span ->
          {
            Zenbu_view.Renderer.start_offset =
              Syntax.Highlight.start_offset span;
            stop_offset = Syntax.Highlight.stop_offset span;
            class_ =
              (match Syntax.Highlight.class_ span with
              | Syntax.Highlight.Keyword -> Zenbu_view.Renderer.Keyword
              | Syntax.Highlight.String -> Zenbu_view.Renderer.String
              | Syntax.Highlight.Number -> Zenbu_view.Renderer.Number
              | Syntax.Highlight.Comment -> Zenbu_view.Renderer.Comment
              | Syntax.Highlight.Type -> Zenbu_view.Renderer.Type
              | Syntax.Highlight.Constructor -> Zenbu_view.Renderer.Constructor);
          })

let presentation_cache session =
  let contents = Editor_context.contents (context session) in
  match session.presentation_cache with
  | Some cache when String.equal cache.contents contents -> cache
  | None | Some _ ->
      {
        contents;
        source_lines = Zenbu_view.Display.source_lines contents;
        syntax_spans = syntax_spans session;
      }

let search_ranges session =
  Option.map (fun search -> search.matches) session.search
  |> Option.value ~default:[]

let diagnostic_ranges session =
  session.diagnostics
  |> List.map (fun (diagnostic : Language.diagnostic) ->
      {
        Zenbu_view.Renderer.start_offset = diagnostic.start_offset;
        stop_offset = diagnostic.stop_offset;
        kind =
          (match diagnostic.severity with
          | Language.Error -> Zenbu_view.Renderer.Error
          | Warning -> Zenbu_view.Renderer.Warning
          | Information -> Zenbu_view.Renderer.Information
          | Hint -> Zenbu_view.Renderer.Hint);
      })

let diagnostic_summary session =
  let count severity =
    List.length
      (List.filter
         (fun (diagnostic : Language.diagnostic) ->
           diagnostic.severity = severity)
         session.diagnostics)
  in
  let errors = count Language.Error in
  let warnings = count Language.Warning in
  let values =
    [] |> fun values ->
    if errors = 0 then values
    else
      ("E" ^ string_of_int errors) :: values |> fun values ->
      if warnings = 0 then values
      else ("W" ^ string_of_int warnings) :: values |> List.rev
  in
  if values = [] then None else Some (String.concat " " values)

let active_input_rules = function
  | Vim_runtime runtime -> Vim_runtime.input_rules runtime
  | Selection_runtime runtime -> Selection_runtime.input_rules runtime
  | Direct_runtime runtime -> Direct_runtime.input_rules runtime
  | Structural_runtime runtime -> Structural_runtime.input_rules runtime
  | Script_runtime runtime -> Script_runtime.input_rules runtime

let help_lines session =
  let model_status = active_status session.active in
  let model_rules =
    active_input_rules session.active
    |> List.map (fun rule ->
        Printf.sprintf "  %s — %s"
          (Input_rule.pattern rule |> Input_rule.pattern_to_string)
          (Input_rule.summary rule))
  in
  [
    "Zenbu getting started";
    "";
    "Host controls";
    "  Ctrl-F search    Ctrl-G / Ctrl-Shift-G next / previous";
    "  Ctrl-P command palette    Ctrl-Shift-S save as";
    "  Alt-M switch model    Alt-H help    Ctrl-O inspector";
    "  Ctrl-S save    Ctrl-Q quit    Alt-R reload configuration";
    "  command palette: editor.macro.record / editor.macro.replay";
    "";
    "Active model metadata";
    "  status: " ^ Model_status.label model_status;
    "  "
    ^ Option.value ~default:"no description"
        (Model_status.description model_status);
    "";
    "Current input rules";
  ]
  @ model_rules
  @ [ ""; "Escape or Enter closes help." ]

let model_choice_name = function
  | Vim -> "Vim-style"
  | Selection -> "Selection-first"
  | Direct -> "Direct"
  | Structural -> "Structural"
  | Script -> "Script"

let interaction_overlay session =
  match session.interaction with
  | Idle | Search_prompt _ | Command_line _ | Command_prompt _
  | Save_as_prompt _ | Open_buffer_prompt _ | Rename_prompt _ ->
      None
  | Help_view -> Some (help_lines session)
  | Model_picker selected ->
      Some
        ("Switch editing model (semantic history, selections, clipboard, and \
          extensions stay active)" :: ""
         :: List.mapi
              (fun index choice ->
                Printf.sprintf "%s%d. %s"
                  (if index = selected then "> " else "  ")
                  (index + 1) (model_choice_name choice))
              (model_choices session)
        @ [ ""; "Arrow keys or number select; Enter confirms; Escape cancels." ]
        )
  | Palette { query; selected } ->
      let items = matching_palette_items session query in
      let maximum_visible = 16 in
      let rec take remaining = function
        | _ when remaining <= 0 -> []
        | [] -> []
        | value :: rest -> value :: take (remaining - 1) rest
      in
      let rec drop remaining values =
        match (remaining, values) with
        | remaining, _ when remaining <= 0 -> values
        | _, [] -> []
        | remaining, _ :: rest -> drop (remaining - 1) rest
      in
      let visible =
        items
        |> List.mapi (fun index (item : palette_item) ->
            Printf.sprintf "%s%s — %s [%s]"
              (if index = selected then "> " else "  ")
              item.id item.title
              (Provider.id item.provider))
        |> fun values ->
        let first =
          min
            (max 0 (List.length values - maximum_visible))
            (max 0 (selected - maximum_visible + 1))
        in
        values |> drop first |> take maximum_visible
      in
      Some
        ([ "Command palette"; "filter: " ^ query; "" ]
        @ (if visible = [] then [ "  no matching commands" ] else visible)
        @ [
            "";
            "All active builtin, script, and plugin commands are searchable.";
          ])
  | File_picker { query; selected } -> (
      match project_file_entries session ~query with
      | Error error ->
          Some
            [
              "Project files";
              "root: " ^ Option.value ~default:"none" (project_root session);
              "filter: " ^ query;
              "";
              "  " ^ Error.to_string error;
            ]
      | Ok entries ->
          let rec take remaining values =
            match (remaining, values) with
            | remaining, _ when remaining <= 0 -> []
            | _, [] -> []
            | remaining, value :: rest -> value :: take (remaining - 1) rest
          in
          let visible =
            entries
            |> List.mapi (fun index (entry : Project_root.entry) ->
                Printf.sprintf "%s%s"
                  (if index = selected then "> " else "  ")
                  entry.relative_path)
            |> take 16
          in
          Some
            ([
               "Project files";
               "root: " ^ Option.value ~default:"none" (project_root session);
               "filter: " ^ query;
               "";
             ]
            @ (if visible = [] then [ "  no matching readable text files" ]
               else visible)
            @ [ ""; "Type to filter; Enter opens; Escape cancels." ]))
  | Project_search_view { snapshot; selected } ->
      let visible =
        snapshot.Project_search.results
        |> List.mapi (fun index (result : Project_search.result) ->
            Printf.sprintf "%s%s:%d"
              (if index = selected then "> " else "  ")
              result.relative_path result.line)
        |> fun results ->
        let rec take remaining = function
          | _ when remaining <= 0 -> []
          | [] -> []
          | result :: rest -> result :: take (remaining - 1) rest
        in
        take 16 results
      in
      Some
        ([
           "Project search";
           "query: " ^ snapshot.query;
           Printf.sprintf "results: %d  scanned: %d files, %d bytes%s"
             (List.length snapshot.results)
             snapshot.scanned_files snapshot.scanned_bytes
             (if snapshot.truncated then " (truncated)" else "");
           "";
         ]
        @ (if visible = [] then [ "  no matching readable text files" ]
           else visible)
        @ [ ""; "Arrow keys select; Enter opens; Escape cancels." ])
  | Query_replace state -> (
      match state.pending with
      | [] -> None
      | current :: _ ->
          Some
            [
              "Query replace";
              "kind: " ^ search_kind_name state.kind;
              "query: " ^ state.query;
              "replacement: " ^ state.replacement;
              Printf.sprintf "match: byte %d..%d  remaining: %d"
                current.start_offset current.stop_offset
                (List.length state.pending);
              Printf.sprintf "replaced: %d  skipped: %d" state.replaced
                state.skipped;
              "";
              "s skip   r replace   a replace remaining   q quit";
            ])
  | Hover_view hover ->
      Some
        ([ "Language hover"; "" ]
        @ String.split_on_char '\n' hover.Language.text
        @ [ ""; "Escape or Enter closes hover." ])
  | Completion_view { items; selected; query } ->
      let visible =
        matching_completion_items items query
        |> List.mapi (fun index item ->
            Printf.sprintf "%s%s%s"
              (if index = selected then "> " else "  ")
              item.Language.label
              (Option.map (fun detail -> " — " ^ detail) item.detail
              |> Option.value ~default:""))
        |> fun values ->
        let rec take remaining = function
          | _ when remaining <= 0 -> []
          | [] -> []
          | value :: rest -> value :: take (remaining - 1) rest
        in
        take 16 values
      in
      Some
        ([ "Language completion"; "filter: " ^ query; "" ]
        @ (if visible = [] then [ "  no matching completions" ] else visible)
        @ [
            "";
            "Type to filter; Arrow keys select; Enter accepts; Escape cancels.";
          ])

let interaction_message session =
  match session.interaction with
  | Search_prompt { kind; query; _ } ->
      let count = List.length (search_ranges session) in
      Some
        (Printf.sprintf "%s%s  %d match%s"
           (match kind with Literal -> "/" | Regexp -> "/~")
           query count
           (if count = 1 then "" else "es"))
  | Command_prompt { descriptor; remaining = parameter :: _; text; _ } ->
      Some
        (Printf.sprintf "%s %s: %s"
           (Command_descriptor.id descriptor |> Command_id.to_string)
           parameter.name text)
  | Command_prompt { remaining = []; _ } -> session.message
  | Command_line line -> Some line
  | Save_as_prompt path -> Some ("destination: " ^ path)
  | Open_buffer_prompt path -> Some ("open: " ^ path)
  | File_picker { query; _ } -> Some ("files: " ^ query)
  | Project_search_view { snapshot; _ } ->
      Some ("project search: " ^ snapshot.query)
  | Query_replace state ->
      Some
        (Printf.sprintf "query-replace: %d remaining"
           (List.length state.pending))
  | Rename_prompt name -> Some ("rename: " ^ name)
  | Completion_view { query; _ } -> Some ("completion: " ^ query)
  | Idle | Palette _ | Model_picker _ | Help_view | Hover_view _ ->
      session.message

let blank_frame ~width ~height =
  Zenbu_view.Frame.create ~width ~height
    ~rows:
      (List.init height (fun _ ->
           [ Zenbu_view.Frame.cell ~width (String.make width ' ') ]))
    ~cursor:None

let clip_terminal_text ~width text =
  if width <= 0 then ""
  else
    Zenbu_view.Display.lines text |> List.hd |> fun line ->
    List.fold_left
      (fun (parts, used) (grapheme : Zenbu_view.Display.grapheme) ->
        if used + grapheme.width > width then (parts, used)
        else (grapheme.text :: parts, used + grapheme.width))
      ([], 0) line.graphemes
    |> fun (parts, _) -> String.concat "" (List.rev parts)

let buffer_line_row session =
  let width = session.dimensions.columns in
  let buffers =
    buffer_ids session |> List.sort_uniq Int.compare
    |> List.filter_map (buffer_for_id session)
  in
  let cells, remaining =
    List.fold_left
      (fun (cells, remaining) (buffer : buffer) ->
        if remaining <= 0 then (cells, remaining)
        else
          let active = buffer.id = session.current_buffer_id in
          let label =
            Printf.sprintf "%s%d:%s%s%s"
              (if active then "[" else " ")
              buffer.id
              (buffer_label ~buffer_name:buffer.buffer_name
                 ~file_path:buffer.file_path)
              (if buffer_dirty buffer then "*" else "")
              (if active then "]" else " ")
          in
          let text = clip_terminal_text ~width:remaining label in
          let text_width = Zenbu_view.Display.text_width text in
          if text_width = 0 then (cells, remaining)
          else
            ( Zenbu_view.Frame.cell
                ~style:
                  (if active then Zenbu_view.Frame.Status
                   else Zenbu_view.Frame.Dim)
                ~width:text_width text
              :: cells,
              remaining - text_width ))
      ([], width) buffers
  in
  List.rev cells
  @
  if remaining = 0 then []
  else
    [
      Zenbu_view.Frame.cell ~style:Zenbu_view.Frame.Status ~width:remaining
        (String.make remaining ' ');
    ]

let with_buffer_line session frame =
  if buffer_line_rows session = 0 then frame
  else
    let cursor =
      Zenbu_view.Frame.cursor frame
      |> Option.map (fun (cursor : Zenbu_view.Frame.cursor) ->
          { Zenbu_view.Frame.column = cursor.column; row = cursor.row + 1 })
    in
    Zenbu_view.Frame.create ~width:session.dimensions.columns
      ~height:session.dimensions.rows
      ~rows:(buffer_line_row session :: Zenbu_view.Frame.rows frame)
      ~cursor

let session_for_buffer session (buffer : buffer) =
  if buffer.id = session.current_buffer_id then session
  else
    {
      session with
      active = buffer.active;
      file_path = buffer.file_path;
      buffer_name = buffer.buffer_name;
      language_override = buffer.language_override;
      saved_version = buffer.saved_version;
      saved_contents = buffer.saved_contents;
      saved_snapshot = buffer.saved_snapshot;
      language_client = buffer.language_client;
      diagnostics = buffer.diagnostics;
      presentation_cache = buffer.presentation_cache;
      search = buffer.search;
      active_modes = buffer.active_modes;
      active_binding_layers = buffer.active_binding_layers;
      inactive_buffers = [];
      interaction = Idle;
      inspector = None;
      message = None;
      quit_armed = false;
    }

let render_context_for_pane session pane display =
  let context = context display in
  let buffer_id = buffer_id_for_pane session pane in
  match pane_view_position session ~pane ~buffer_id with
  | Some { stale = false; document_version; selections; primary; _ }
    when document_version = Editor_context.document_version context ->
      Editor_context.with_selections context
        {
          Editor_context.selections =
            List.map
              (fun (anchor_offset, head_offset) ->
                Editor_context.{ anchor_offset; head_offset })
              selections;
          primary_index = primary;
        }
  | Some _ | None -> context

let render_pane session pane rectangle =
  if rectangle.Layout.width = 0 || rectangle.height = 0 then
    (session, blank_frame ~width:rectangle.width ~height:rectangle.height)
  else
    let focused = pane = session.focused_pane in
    let display =
      match buffer_for_id session (buffer_id_for_pane session pane) with
      | Some buffer -> session_for_buffer session buffer
      | None -> session
    in
    let presentation = presentation_cache display in
    let context = render_context_for_pane session pane display in
    let dimensions =
      Zenbu_view.Renderer.{ columns = rectangle.width; rows = rectangle.height }
    in
    let rendered =
      Zenbu_view.Renderer.render_with_inspector ~context
        ~presentation:session.presentation
        ~status:
          (if focused then status session else active_status display.active)
        ~filename:(filename display) ~dirty:(current_dirty display)
        ~message:(if focused then interaction_message session else None)
        ~viewport:(pane_viewport session pane)
        ~dimensions
        ~inspector:(if focused then session.inspector else None)
        ?overlay:(if focused then interaction_overlay session else None)
        ~source_lines:presentation.source_lines
        ~syntax_spans:presentation.syntax_spans
        ~search_ranges:(search_ranges display)
        ~diagnostic_ranges:(diagnostic_ranges display)
        ?diagnostic_summary:(diagnostic_summary display)
        ()
    in
    (set_pane_viewport session pane rendered.viewport, rendered.frame)

let render session =
  let presentation = presentation_cache session in
  let session, frames =
    layout_bounds session
    |> List.fold_left
         (fun (session, frames) (pane, rectangle) ->
           let session, frame = render_pane session pane rectangle in
           (session, (pane, frame) :: frames))
         (session, [])
  in
  let frame =
    match
      Layout.compose session.layout ~width:session.dimensions.columns
        ~height:(workspace_height session) ~focused_pane:session.focused_pane
        ~frames
    with
    | Ok frame -> frame
    | Error _ ->
        blank_frame ~width:session.dimensions.columns
          ~height:(workspace_height session)
  in
  ( { session with presentation_cache = Some presentation },
    with_buffer_line session frame )

let contents session = Editor_context.contents (context session)
let file_path session = session.file_path
let dimensions session = session.dimensions
let viewport session = pane_viewport session session.focused_pane

let notice session message =
  { session with message = Some message; quit_armed = false; inspector = None }

let all_models =
  [
    Vim_model.descriptor;
    Selection_model.descriptor;
    Direct_model.descriptor;
    Structural_model.descriptor;
  ]

let scope_to_string = function
  | Scripting.Global -> "global"
  | Scripting.Model model -> "model:" ^ model
  | Scripting.Model_status { model; status } -> "model:" ^ model ^ ":" ^ status
  | Scripting.Mode mode -> "mode:" ^ mode

let script_binding_lines session =
  let layers = binding_layer_catalog session.generation session.plugins in
  let bindings = active_bindings session in
  let layer_lines =
    layers
    |> List.map (fun layer ->
        Printf.sprintf "binding layer: %s (priority %d; %s; provider %s)"
          (Scripting.binding_layer_id layer)
          (Scripting.binding_layer_priority layer)
          (if
             List.exists
               (fun active -> same_binding_layer active layer)
               session.active_binding_layers
           then "enabled"
           else "disabled")
          (Provider.id (Scripting.binding_layer_provider layer)))
  in
  let binding_lines =
    bindings
    |> List.map (fun binding ->
        let layer =
          match Scripting.binding_layer binding with
          | None -> "base"
          | Some id ->
              let enabled =
                List.exists
                  (fun layer -> binding_belongs_to_layer layer binding)
                  session.active_binding_layers
              in
              Printf.sprintf "layer %s (%s)" id
                (if enabled then "enabled" else "disabled")
        in
        Printf.sprintf "binding: %s -> %s (%s; %s; provider %s)"
          (Input_event.binding_pattern_sequence_to_string
             (Scripting.binding_inputs binding))
          (Scripting.binding_command binding)
          (scope_to_string (Scripting.binding_scope binding))
          layer
          (Provider.id (Scripting.binding_provider binding)))
  in
  if layer_lines = [] && binding_lines = [] then [ "extension overlays: none" ]
  else layer_lines @ binding_lines

let plugin_lines session =
  match Plugins.views session.plugins with
  | [] -> [ "plugins: none" ]
  | views ->
      List.concat_map
        (fun view ->
          let id =
            Plugins.view_id view
            |> Option.map Zenbu_extension.Plugin_id.to_string
            |> Option.value ~default:"<invalid-manifest>"
          in
          let version =
            Plugins.view_version view
            |> Option.map Zenbu_extension.Plugin_version.to_string
            |> Option.value ~default:"-"
          in
          let runtime = Option.value ~default:"-" (Plugins.view_runtime view) in
          let capabilities =
            Plugins.view_granted_capabilities view
            |> List.map Zenbu_extension.Capability.id
            |> String.concat ", "
          in
          let contributions =
            Plugins.view_contributions view
            |> List.map Zenbu_extension.Contribution.id
            |> String.concat ", "
          in
          [
            Printf.sprintf "%s %s %s %s" id version
              (Plugins.state_name (Plugins.view_state view))
              runtime;
            "  health: " ^ Plugins.health_name (Plugins.view_health view);
            "  manifest: " ^ Plugins.view_manifest_path view;
            ("  capabilities: "
            ^ if String.length capabilities = 0 then "none" else capabilities);
            ("  contributions: "
            ^ if String.length contributions = 0 then "none" else contributions
            );
            (match Plugins.view_runtime_limits view with
            | None -> "  limits: none"
            | Some (fuel, memory_bytes) ->
                Printf.sprintf "  limits: fuel=%d memory-bytes=%d" fuel
                  memory_bytes);
            (match Plugins.view_error view with
            | None -> "  last-error: none"
            | Some error -> "  last-error: " ^ Error.to_string error);
          ])
        views

let macro_lines session =
  let preview inputs =
    let rec take remaining = function
      | _ when remaining <= 0 -> []
      | [] -> []
      | input :: rest ->
          Input_event.to_string input :: take (remaining - 1) rest
    in
    let values = take 12 inputs in
    if List.length inputs > List.length values then values @ [ "…" ] else values
  in
  let recorded =
    match session.last_macro_register with
    | None ->
        [
          "last-recorded-register: none";
          "recorded-inputs: none";
          "preview: none";
        ]
    | Some register -> (
        match find_macro session register with
        | None ->
            [
              "last-recorded-register: " ^ register;
              "recorded-inputs: none";
              "preview: none";
            ]
        | Some inputs ->
            [
              "last-recorded-register: " ^ register;
              "recorded-inputs: " ^ string_of_int (List.length inputs);
              "preview: " ^ String.concat " " (preview inputs);
            ])
  in
  let register_catalog =
    session.macros
    |> List.sort (fun (left, _) (right, _) -> String.compare left right)
    |> List.map (fun (register, inputs) ->
        Printf.sprintf "%s (%d)" register (List.length inputs))
    |> String.concat ", "
  in
  [
    "Keyboard macros";
    (match session.macro_recording with
    | None -> "recording: no"
    | Some { register; inputs_rev } ->
        Printf.sprintf "recording: yes (register %s, %d inputs)" register
          (List.length inputs_rev));
    "replaying: " ^ string_of_bool session.macro_replaying;
    "maximum-recorded-inputs: " ^ string_of_int maximum_macro_events;
    "maximum-registers: " ^ string_of_int maximum_macro_registers;
    "maximum-replay-count: " ^ string_of_int maximum_macro_replay_count;
    "maximum-replay-events: " ^ string_of_int maximum_macro_replay_events;
    "register-count: " ^ string_of_int (List.length session.macros);
    ("registers: "
    ^ if String.length register_catalog = 0 then "none" else register_catalog);
  ]
  @ recorded

let location_lines session =
  let location_line (location : location) =
    let selections =
      location.selections
      |> List.map (fun (anchor_offset, head_offset) ->
          string_of_int anchor_offset ^ ":" ^ string_of_int head_offset)
      |> String.concat ", "
    in
    Printf.sprintf "%s buffer=%d version=%d primary=%d selections=%s state=%s"
      location.name location.buffer_id location.document_version
      location.primary selections
      (if location.stale then "stale" else "active")
  in
  [
    "Locations";
    "maximum-locations: " ^ string_of_int maximum_locations;
    "maximum-name-bytes: " ^ string_of_int maximum_location_name_bytes;
    "count: " ^ string_of_int (List.length session.locations);
  ]
  @ (session.locations
    |> List.sort (fun left right -> String.compare left.name right.name)
    |> List.map location_line)

let jump_lines session =
  let location_line direction index (location : location) =
    let selections =
      location.selections
      |> List.map (fun (anchor_offset, head_offset) ->
          string_of_int anchor_offset ^ ":" ^ string_of_int head_offset)
      |> String.concat ", "
    in
    Printf.sprintf
      "%s %d buffer=%d version=%d primary=%d selections=%s state=%s" direction
      index location.buffer_id location.document_version location.primary
      selections
      (if location.stale then "stale" else "active")
  in
  [
    "Jump history";
    "maximum-entries: " ^ string_of_int maximum_jump_entries;
    "backward-count: " ^ string_of_int (List.length session.backward_jumps);
    "forward-count: " ^ string_of_int (List.length session.forward_jumps);
  ]
  @ List.mapi (location_line "backward") session.backward_jumps
  @ List.mapi (location_line "forward") session.forward_jumps

let inspect session inspection =
  let format ~last_execution ~trace ~model_descriptor ~model_status ~rules
      ~command_registry ~semantic_behaviors ~runtime_history ~runtime_context
      ~profiler =
    match inspection with
    | Why -> (
        match last_execution with
        | None -> [ "Why"; "no completed input execution" ]
        | Some execution_id -> (
            match Inspector.why trace ~execution_id with
            | None -> [ "Why"; "trace is disabled; restart with --trace" ]
            | Some why -> "Why" :: Inspector.format_why why))
    | Bindings ->
        "Bindings"
        :: (Inspector.format_bindings model_descriptor model_status rules
           @ host_binding_lines session
           @ script_binding_lines session)
    | Commands ->
        "Commands"
        :: Inspector.format_commands
             (Inspector.commands command_registry
             @ (host_command_descriptors ()
               |> List.map Inspector.describe_command))
    | History ->
        "History"
        :: Inspector.format_history
             (Inspector.history ~saved_version:session.saved_version
                runtime_history)
    | Selection_view ->
        "Selection"
        :: Inspector.format_selection (Inspector.selections runtime_context)
    | Syntax -> (
        match Inspector.syntax runtime_context with
        | None -> [ "Syntax"; "syntax is unavailable" ]
        | Some syntax -> "Syntax" :: Inspector.format_syntax syntax)
    | Profile -> "Profile" :: Inspector.format_profile profiler
    | Search -> (
        match session.search with
        | None -> [ "Search"; "active-query: none" ]
        | Some search ->
            [
              "Search";
              "kind: " ^ search_kind_name search.kind;
              "query: " ^ search.query;
              "matches: " ^ string_of_int (List.length search.matches);
              (match search.current with
              | None -> "current-match: none"
              | Some index -> "current-match: " ^ string_of_int (index + 1));
              (match session.interaction with
              | Search_prompt _ -> "prompt: open"
              | Idle | Palette _ | Command_line _ | Command_prompt _
              | Save_as_prompt _ | Open_buffer_prompt _ | File_picker _
              | Project_search_view _ | Query_replace _ | Model_picker _
              | Help_view | Hover_view _ | Completion_view _ | Rename_prompt _
                ->
                  "prompt: closed");
            ])
    | Macros -> macro_lines session
    | Locations -> location_lines session
    | Jumps -> jump_lines session
    | Jobs -> background_job_lines session
    | Buffers -> buffer_lines session
    | Project -> project_root_lines session
    | Project_search -> project_search_lines session
    | File_watches -> file_watch_lines session
    | Api ->
        "API"
        :: Inspector.format_api
             (Inspector.api ~models:all_models ~commands:command_registry
                ~semantic_behaviors ())
    | Plugins -> "Plugins" :: plugin_lines session
    | Language -> language_status_lines session
    | Scripts -> (
        match session.generation with
        | None ->
            [
              "Scripts";
              "active-generation: none";
              (match session.message with
              | None -> "message: none"
              | Some message -> "message: " ^ message);
              (match session.last_reload_error with
              | None -> "last-reload: none"
              | Some error -> "last-reload-error: " ^ Error.to_string error);
            ]
        | Some generation ->
            let commands, selectors, transformations, bindings, hooks =
              Scripting.counts generation
            in
            [
              "Scripts";
              "generation: "
              ^ string_of_int (Scripting.generation_id generation);
              "source: " ^ Scripting.source generation;
              "provider: " ^ Provider.id (Scripting.provider generation);
              (match Scripting.model generation with
              | None -> "model: none"
              | Some model ->
                  "model: "
                  ^ Editing_model.id (Scripting.model_descriptor model));
              Printf.sprintf
                "registrations: %d commands, %d selectors, %d transformations, \
                 %d bindings, %d hooks, %d modes"
                commands selectors transformations bindings hooks
                (List.length (Scripting.modes generation));
              (match session.message with
              | None -> "message: none"
              | Some message -> "message: " ^ message);
              (match session.last_reload_error with
              | None -> "last-reload: success"
              | Some error -> "last-reload-error: " ^ Error.to_string error);
            ])
  in
  match session.active with
  | Vim_runtime runtime ->
      format
        ~last_execution:(Vim_runtime.last_execution runtime)
        ~trace:(Vim_runtime.trace runtime)
        ~model_descriptor:(Vim_runtime.model_descriptor runtime)
        ~model_status:(Vim_runtime.status runtime)
        ~rules:(Vim_runtime.input_rules runtime)
        ~command_registry:(Vim_runtime.commands runtime)
        ~semantic_behaviors:(Vim_runtime.semantic_behaviors runtime)
        ~runtime_history:(Vim_runtime.history runtime)
        ~runtime_context:(Vim_runtime.context runtime)
        ~profiler:(Vim_runtime.profiler runtime)
  | Selection_runtime runtime ->
      format
        ~last_execution:(Selection_runtime.last_execution runtime)
        ~trace:(Selection_runtime.trace runtime)
        ~model_descriptor:(Selection_runtime.model_descriptor runtime)
        ~model_status:(Selection_runtime.status runtime)
        ~rules:(Selection_runtime.input_rules runtime)
        ~command_registry:(Selection_runtime.commands runtime)
        ~semantic_behaviors:(Selection_runtime.semantic_behaviors runtime)
        ~runtime_history:(Selection_runtime.history runtime)
        ~runtime_context:(Selection_runtime.context runtime)
        ~profiler:(Selection_runtime.profiler runtime)
  | Direct_runtime runtime ->
      format
        ~last_execution:(Direct_runtime.last_execution runtime)
        ~trace:(Direct_runtime.trace runtime)
        ~model_descriptor:(Direct_runtime.model_descriptor runtime)
        ~model_status:(Direct_runtime.status runtime)
        ~rules:(Direct_runtime.input_rules runtime)
        ~command_registry:(Direct_runtime.commands runtime)
        ~semantic_behaviors:(Direct_runtime.semantic_behaviors runtime)
        ~runtime_history:(Direct_runtime.history runtime)
        ~runtime_context:(Direct_runtime.context runtime)
        ~profiler:(Direct_runtime.profiler runtime)
  | Structural_runtime runtime ->
      format
        ~last_execution:(Structural_runtime.last_execution runtime)
        ~trace:(Structural_runtime.trace runtime)
        ~model_descriptor:(Structural_runtime.model_descriptor runtime)
        ~model_status:(Structural_runtime.status runtime)
        ~rules:(Structural_runtime.input_rules runtime)
        ~command_registry:(Structural_runtime.commands runtime)
        ~semantic_behaviors:(Structural_runtime.semantic_behaviors runtime)
        ~runtime_history:(Structural_runtime.history runtime)
        ~runtime_context:(Structural_runtime.context runtime)
        ~profiler:(Structural_runtime.profiler runtime)
  | Script_runtime runtime ->
      format
        ~last_execution:(Script_runtime.last_execution runtime)
        ~trace:(Script_runtime.trace runtime)
        ~model_descriptor:(Script_runtime.model_descriptor runtime)
        ~model_status:(Script_runtime.status runtime)
        ~rules:(Script_runtime.input_rules runtime)
        ~command_registry:(Script_runtime.commands runtime)
        ~semantic_behaviors:(Script_runtime.semantic_behaviors runtime)
        ~runtime_history:(Script_runtime.history runtime)
        ~runtime_context:(Script_runtime.context runtime)
        ~profiler:(Script_runtime.profiler runtime)

let toggle_inspector session =
  match session.inspector with
  | Some _ -> { session with inspector = None }
  | None -> { session with inspector = Some (inspect session Why) }

let inspector_open session = Option.is_some session.inspector
let theme session = session.theme

let language_wakeup_fd session =
  Option.map Lsp.wakeup_fd session.language_client

let language_wakeup_fds session =
  current_buffer session :: session.inactive_buffers
  |> List.filter_map (fun (buffer : buffer) ->
      Option.map Lsp.wakeup_fd buffer.language_client)
  |> List.sort_uniq compare

let background_job_wakeup_fd session =
  Option.map Background_job.wakeup_fd session.jobs

let wakeup_fds session =
  language_wakeup_fds session
  @ Option.to_list (background_job_wakeup_fd session)
  @ [ File_watcher.wakeup_fd session.file_watcher ]
  |> List.sort_uniq compare

let poll_background session =
  let session = poll_language session |> poll_file_watcher in
  match session.jobs with
  | None -> session
  | Some jobs -> (
      match Background_job.drain jobs with
      | [] -> session
      | completions ->
          let message =
            completions |> List.rev |> List.hd
            |> Background_job.completion_message
          in
          { session with message = Some message; quit_armed = false })

let close session =
  File_watcher.close session.file_watcher;
  Option.iter Background_job.close session.jobs;
  current_buffer session :: session.inactive_buffers
  |> List.iter (fun (buffer : buffer) ->
      Option.iter Lsp.close buffer.language_client)
