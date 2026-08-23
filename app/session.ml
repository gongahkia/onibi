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
module Vim_runtime = Model_runtime.Make (Vim_model)
module Selection_runtime = Model_runtime.Make (Selection_model)
module Structural_runtime = Model_runtime.Make (Structural_model)

type model = Vim | Selection | Structural

type host_command =
  | Save
  | Save_as
  | Quit
  | Force_quit
  | Reload_config
  | Start_search
  | Search_next
  | Search_previous
  | Open_palette
  | Switch_model
  | Help
  | Language_status
  | Language_restart
  | Language_hover
  | Language_definition
  | Language_complete
  | Language_rename
  | Language_diagnostic_next
  | Language_diagnostic_previous
  | Language_diagnostic_describe_current

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
  | Language

type search = {
  query : string;
  matches : Zenbu_view.Renderer.search_range list;
  current : int option;
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
}

type presentation_cache = {
  contents : string;
  source_lines : Zenbu_view.Display.source_line list;
  syntax_spans : Zenbu_view.Renderer.syntax_span list;
}

type interaction =
  | Idle
  | Search_prompt of {
      query : string;
      origin : Editor_context.selection_set;
      direction : Model_effect.search_direction;
    }
  | Palette of { query : string; selected : int }
  | Save_as_prompt of string
  | Model_picker of int
  | Help_view
  | Hover_view of Language.hover
  | Completion_view of {
      items : Language.completion list;
      selected : int;
      query : string;
    }
  | Rename_prompt of string

type outcome = Continue of t | Exit of t

and active =
  | Vim_runtime of Vim_runtime.t
  | Selection_runtime of Selection_runtime.t
  | Structural_runtime of Structural_runtime.t

and t = {
  active : active;
  base_commands : Command_registry.t;
  base_semantics : Semantic_descriptor.t list;
  config : Scripting.config;
  generation : Scripting.t option;
  plugins : Plugins.t;
  next_generation_id : int;
  last_reload_error : Error.t option;
  delivering_events : Scripting.event list;
  file_path : string option;
  language_override : string option;
  saved_version : int;
  saved_contents : string;
  viewport : Zenbu_view.Viewport.t;
  dimensions : Zenbu_view.Renderer.dimensions;
  message : string option;
  quit_armed : bool;
  inspector : string list option;
  presentation_cache : presentation_cache option;
  search : search option;
  interaction : interaction;
  language_client : Lsp.t option;
  diagnostics : Language.diagnostic list;
  language_registry : Language.Registry.t;
}

let static = function
  | Ok value -> value
  | Error error -> failwith (Error.to_string error)

type host_command_entry = {
  command : host_command;
  descriptor : Command_descriptor.t;
  palette : bool;
}

let host_provider =
  Provider.create ~id:"zenbu.app" ~kind:Provider.Application |> static

let host_descriptor id title description =
  Command_descriptor.create
    ~id:(Command_id.of_string id |> static)
    ~title ~description ~category:"host" ~provider:host_provider ()
  |> static

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
          host_descriptor "editor.save-as" "Save buffer as"
            "Prompt for a path and atomically replace that destination.";
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
        command = Search_next;
        descriptor =
          host_descriptor "search.next" "Next search match"
            "Select the next literal-search match, wrapping at the end.";
        palette = true;
      };
      {
        command = Search_previous;
        descriptor =
          host_descriptor "search.previous" "Previous search match"
            "Select the previous literal-search match, wrapping at the \
             beginning.";
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
        command = Switch_model;
        descriptor =
          host_descriptor "editor.model.switch" "Switch editing model"
            "Choose Vim-style, selection-first, or structural editing without \
             replacing semantic state.";
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

let host_binding_lines () =
  [
    "host reserved: Ctrl-S -> editor.save (zenbu.app)";
    "host reserved: Ctrl-Shift-S -> editor.save-as (zenbu.app)";
    "host reserved: Ctrl-Q -> editor.quit (zenbu.app; press again when dirty)";
    "host reserved: Alt-R / Ctrl-Alt-R -> config.reload (zenbu.app)";
    "host reserved: Ctrl-F -> search.start (zenbu.app)";
    "host reserved: Ctrl-G -> search.next (zenbu.app)";
    "host reserved: Ctrl-Shift-G -> search.previous (zenbu.app)";
    "host reserved: Ctrl-P -> editor.command-palette (zenbu.app)";
    "host reserved: Alt-M -> editor.model.switch (zenbu.app)";
    "host reserved: Alt-H -> editor.help (zenbu.app)";
    "host reserved: Ctrl-O -> why inspector (zenbu.app)";
    "host reserved: Ctrl-Space -> language.complete (zenbu.language)";
  ]

let commands () =
  match
    Command_registry.register Command_registry.empty
      Semantic_commands.apply_command
  with
  | Error _ as error -> error
  | Ok registry ->
      List.fold_left
        (fun registry command ->
          match registry with
          | Error _ -> registry
          | Ok registry -> Command_registry.register registry command)
        (Ok registry)
        (Syntax_commands.commands ())

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
  | Structural_runtime runtime -> Structural_runtime.trace runtime

let profiler_of_active = function
  | Vim_runtime runtime -> Vim_runtime.profiler runtime
  | Selection_runtime runtime -> Selection_runtime.profiler runtime
  | Structural_runtime runtime -> Structural_runtime.profiler runtime

let active_with_syntax_service active syntax_service =
  match active with
  | Vim_runtime runtime ->
      Vim_runtime.with_syntax_service runtime ~syntax_service
      |> Result.map (fun runtime -> Vim_runtime runtime)
  | Selection_runtime runtime ->
      Selection_runtime.with_syntax_service runtime ~syntax_service
      |> Result.map (fun runtime -> Selection_runtime runtime)
  | Structural_runtime runtime ->
      Structural_runtime.with_syntax_service runtime ~syntax_service
      |> Result.map (fun runtime -> Structural_runtime runtime)

let last_execution_of_active = function
  | Vim_runtime runtime -> Vim_runtime.last_execution runtime
  | Selection_runtime runtime -> Selection_runtime.last_execution runtime
  | Structural_runtime runtime -> Structural_runtime.last_execution runtime

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
    ?(config = Scripting.Default) ?(plugins = Plugins.Disabled)
    ?(language_registry = Language.Registry.default ()) ~dimensions () =
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
                | Structural ->
                    Structural_runtime.create ~commands ~semantic_behaviors
                      ?syntax_service ~trace ~profiler ~document ()
                    |> Result.map (fun runtime -> Structural_runtime runtime)
              in
              runtime
              |> Result.map (fun active ->
                  let language_client =
                    Option.bind file_path (fun path ->
                        Language.Registry.find_for_path language_registry
                          ~language_id:language path
                        |> Option.map (fun server ->
                            Lsp.start ~config:server
                              ~document_id:"terminal-buffer" ~document_version:0
                              ~file_path:path ~contents ~trace ~profiler))
                  in
                  {
                    active;
                    base_commands;
                    base_semantics;
                    config;
                    generation;
                    plugins = plugin_host;
                    next_generation_id = 2;
                    last_reload_error = config_error;
                    delivering_events = [];
                    file_path;
                    language_override = language;
                    saved_version = 0;
                    saved_contents = contents;
                    viewport = Zenbu_view.Viewport.origin;
                    dimensions;
                    message = config_message;
                    quit_armed = false;
                    inspector = None;
                    presentation_cache = None;
                    search = None;
                    interaction = Idle;
                    language_client;
                    diagnostics = [];
                    language_registry;
                  })))

let context = function
  | { active = Vim_runtime runtime; _ } -> Vim_runtime.context runtime
  | { active = Selection_runtime runtime; _ } ->
      Selection_runtime.context runtime
  | { active = Structural_runtime runtime; _ } ->
      Structural_runtime.context runtime

let active_status = function
  | Vim_runtime runtime -> Vim_runtime.status runtime
  | Selection_runtime runtime -> Selection_runtime.status runtime
  | Structural_runtime runtime -> Structural_runtime.status runtime

let host_status ~id ~label ~description ?(text_entry = false) () =
  Model_status.create ~id ~label ~description
    ~input_mode:
      (if text_entry then Model_status.Text_entry else Model_status.Key_commands)
    ()
  |> Result.get_ok

let status session =
  match session.interaction with
  | Idle -> active_status session.active
  | Search_prompt _ ->
      host_status ~id:"host-search" ~label:"SEARCH"
        ~description:
          "enter a literal Unicode search; Enter confirms and Escape cancels"
        ~text_entry:true ()
  | Palette _ ->
      host_status ~id:"host-palette" ~label:"COMMAND"
        ~description:"filter registered commands from all active providers"
        ~text_entry:true ()
  | Save_as_prompt _ ->
      host_status ~id:"host-save-as" ~label:"SAVE AS"
        ~description:"enter a destination path; Enter saves atomically"
        ~text_entry:true ()
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

let model = function
  | { active = Vim_runtime _; _ } -> Vim
  | { active = Selection_runtime _; _ } -> Selection
  | { active = Structural_runtime _; _ } -> Structural

let filename session =
  match session.file_path with
  | None -> "[No Name]"
  | Some path -> Filename.basename path

let configuration_error session = session.last_reload_error

let plugin_load_errors session =
  Plugins.views session.plugins
  |> List.filter_map (fun view ->
      match (Plugins.view_state view, Plugins.view_error view) with
      | Plugins.Failed, Some error -> Some error
      | Plugins.Active, None | Plugins.Active, Some _ | Plugins.Failed, None ->
          None)

let dirty session =
  let context = context session in
  Editor_context.document_version context <> session.saved_version
  && not (String.equal (Editor_context.contents context) session.saved_contents)

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
  | Structural_runtime runtime ->
      Structural_runtime.with_extensions runtime ~commands ~semantic_behaviors
      |> fun runtime -> Structural_runtime runtime

let active_commands = function
  | Vim_runtime runtime -> Vim_runtime.commands runtime
  | Selection_runtime runtime -> Selection_runtime.commands runtime
  | Structural_runtime runtime -> Structural_runtime.commands runtime

let shared_state = function
  | Vim_runtime runtime -> Vim_runtime.shared_state runtime
  | Selection_runtime runtime -> Selection_runtime.shared_state runtime
  | Structural_runtime runtime -> Structural_runtime.shared_state runtime

let active_from_shared model shared =
  match model with
  | Vim ->
      Vim_runtime.create_from_shared shared
      |> Result.map (fun value -> Vim_runtime value)
  | Selection ->
      Selection_runtime.create_from_shared shared
      |> Result.map (fun value -> Selection_runtime value)
  | Structural ->
      Structural_runtime.create_from_shared shared
      |> Result.map (fun value -> Structural_runtime value)

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
      | Ok configured_commands ->
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
                    | Some generation -> Some (Scripting.bindings generation))
                  ())
          in
          trace_plugins trace ~execution_id ~phase:"reload" plugin_host;
          trace_runtime_events trace profiler ~execution_id plugin_host;
          let commands =
            match commands_with_plugins configured_commands plugin_host with
            | Ok commands -> commands
            | Error error ->
                failwith
                  ("plugin snapshot invariant violated: "
                 ^ Error.to_string error)
          in
          let active =
            active_with_extensions session.active ~commands
              ~semantic_behaviors:
                (semantic_behaviors_with_plugins generation plugin_host)
          in
          Option.iter Scripting.dispose session.generation;
          lifecycle trace ~execution_id ~phase:"reload" ?generation
            ~outcome:"succeeded" ();
          let message =
            let plugin_count = List.length (Plugins.providers plugin_host) in
            match generation with
            | None ->
                Printf.sprintf
                  "configuration reloaded: no active script generation; %d \
                   active plugins"
                  plugin_count
            | Some generation ->
                let commands, selectors, transformations, bindings, hooks =
                  Scripting.counts generation
                in
                Printf.sprintf
                  "configuration reloaded: %d commands, %d selectors, %d \
                   transformations, %d bindings, %d hooks"
                  commands selectors transformations bindings hooks
                ^ Printf.sprintf "; %d active plugins" plugin_count
          in
          {
            session with
            active;
            generation;
            plugins = plugin_host;
            next_generation_id = session.next_generation_id + 1;
            last_reload_error = None;
            message = Some message;
            quit_armed = false;
          })

let model_descriptor = function
  | Vim_runtime runtime -> Vim_runtime.model_descriptor runtime
  | Selection_runtime runtime -> Selection_runtime.model_descriptor runtime
  | Structural_runtime runtime -> Structural_runtime.model_descriptor runtime

let binding_rank session binding =
  let model = model_descriptor session.active |> Editing_model.id in
  let status = status session |> Model_status.id in
  match Scripting.binding_scope binding with
  | Scripting.Global -> Some 0
  | Scripting.Model candidate when String.equal candidate model -> Some 1
  | Scripting.Model_status { model = candidate; status = candidate_status }
    when String.equal candidate model && String.equal candidate_status status ->
      Some 2
  | Scripting.Model _ | Scripting.Model_status _ -> None

let matching_binding session input =
  let bindings =
    (match session.generation with
      | None -> []
      | Some generation -> Scripting.bindings generation)
    @ Plugins.bindings session.plugins
  in
  bindings
  |> List.filter_map (fun binding ->
      if
        String.equal
          (Input_event.to_string (Scripting.binding_input binding))
          (Input_event.to_string input)
      then
        Option.map (fun rank -> (rank, binding)) (binding_rank session binding)
      else None)
  |> List.sort (fun (left, _) (right, _) -> Int.compare right left)
  |> function
  | [] -> None
  | (_, binding) :: _ -> Some binding

let history_of_active = function
  | Vim_runtime runtime -> Vim_runtime.history runtime
  | Selection_runtime runtime -> Selection_runtime.history runtime
  | Structural_runtime runtime -> Structural_runtime.history runtime

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
  in
  let next, changed = result in
  (observe_language_document_version next, changed)

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

let apply_definition session input target =
  match session.file_path with
  | Some path
    when String.equal (Language.Uri.file_of_path path) target.Language.uri -> (
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
                Provenance.add provenance
                  (Provenance.Effect "language.definition"))
              session input
              [ Model_effect.Execute_intent intent ]
          in
          {
            next with
            interaction = Idle;
            message = Some "definition: same document";
          })
  | _ ->
      {
        session with
        message =
          Some
            ("definition is outside the active buffer: " ^ target.Language.uri
           ^ " (cross-file navigation is not supported yet)");
      }

let all_current_document_edits session edits =
  match session.file_path with
  | None -> Error "rename requires a saved file"
  | Some path ->
      let current_uri = Language.Uri.file_of_path path in
      if
        List.for_all
          (fun (edit : Lsp.workspace_edit) -> String.equal edit.uri current_uri)
          edits
      then
        Ok
          (List.concat_map
             (fun (edit : Lsp.workspace_edit) -> edit.edits)
             edits)
      else
        let paths =
          edits
          |> List.filter_map (fun (edit : Lsp.workspace_edit) ->
              if String.equal edit.uri current_uri then None else Some edit.uri)
          |> List.sort_uniq String.compare
          |> fun values ->
          if List.length values > 4 then
            List.filteri (fun index _ -> index < 4) values @ [ "…" ]
          else values
        in
        Error
          ("workspace edit spans unsupported files: " ^ String.concat ", " paths)

let poll_language session =
  let current_version = Editor_context.document_version (context session) in
  let handle session = function
    | Lsp.Initialized -> { session with message = Some "language server ready" }
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
           && byte_offset = primary_offset session -> (
        match hover with
        | None ->
            { session with message = Some "language: no hover information" }
        | Some hover ->
            { session with interaction = Hover_view hover; message = None })
    | Lsp.Hover_result _ ->
        {
          session with
          message = Some "language: stale hover response discarded";
        }
    | Lsp.Definition_result { document_version; byte_offset; targets; _ }
      when document_version = current_version
           && byte_offset = primary_offset session -> (
        match targets with
        | [] -> { session with message = Some "language: no definition found" }
        | target :: _ ->
            apply_definition session
              (Input_event.key_press (Input_event.named_key Input_event.Enter))
              target)
    | Lsp.Definition_result _ -> session
    | Lsp.Completion_result { document_version; byte_offset; items; _ }
      when document_version = current_version
           && byte_offset = primary_offset session ->
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
      when document_version = current_version -> (
        match all_current_document_edits session edits with
        | Error reason ->
            { session with message = Some ("rename rejected: " ^ reason) }
        | Ok edits ->
            let input =
              Input_event.key_press (Input_event.named_key Input_event.Enter)
            in
            let next =
              apply_language_edits session input ~effect_id:"language.rename"
                ~edits
            in
            { next with interaction = Idle; message = Some "rename applied" })
    | Lsp.Rename_result _ -> session
    | Lsp.Apply_edit { request_id; edits } -> (
        match all_current_document_edits session edits with
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
        | Ok edits ->
            let input =
              Input_event.key_press (Input_event.named_key Input_event.Enter)
            in
            let next =
              apply_language_edits session input
                ~effect_id:"language.apply-edit" ~edits
            in
            Option.iter
              (fun client ->
                Lsp.respond_apply_edit client ~request_id ~applied:true
                  ~reason:None)
              next.language_client;
            { next with message = Some "workspace/applyEdit applied" })
    | Lsp.Server_message message ->
        { session with message = Some ("language: " ^ message) }
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
    | Lsp.Server_failed reason | Lsp.Server_exited reason ->
        {
          session with
          message = Some ("language server unavailable: " ^ reason);
        }
  in
  match session.language_client with
  | None -> session
  | Some client -> List.fold_left handle session (Lsp.drain client)

let invoke_bound_command session input binding =
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
    in
    Trace.emit_lazy (trace_of_active next.active) (fun () ->
        Trace_event.Binding_resolved
          {
            execution_id;
            input = Input_event.to_string (Scripting.binding_input binding);
            command_id = command;
            provider = Scripting.binding_provider binding;
            scope;
          });
    next
  in
  if String.equal command "config.reload" then
    let next = reload_config session in
    (trace_binding next, false)
  else
    match Command_id.of_string command with
    | Error error ->
        ({ session with message = Some (Error.to_string error) }, false)
    | Ok id ->
        let invocation =
          Command_invocation.create ~id ~arguments:[] |> Result.get_ok
        in
        let next, changed =
          execute_active_effects
            ~augment_provenance:(fun provenance ->
              Provenance.add provenance
                (Provenance.Binding
                   {
                     input =
                       Input_event.to_string (Scripting.binding_input binding);
                     command;
                     provider = Scripting.binding_provider binding;
                   }))
            session input
            [ Model_effect.Invoke_command invocation ]
        in
        (trace_binding next, changed)

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

let search_with_query session query =
  let matches =
    literal_matches (Editor_context.contents (context session)) query
  in
  { query; matches; current = (if matches = [] then None else Some 0) }

let refresh_search_after_document_change session =
  match session.search with
  | None -> session
  | Some previous ->
      let refreshed = search_with_query session previous.query in
      let selections = Editor_context.selections (context session) in
      let primary = List.nth selections.selections selections.primary_index in
      let current =
        refreshed.matches
        |> List.find_index (fun (range : Zenbu_view.Renderer.search_range) ->
            range.start_offset = primary.anchor_offset
            && range.stop_offset = primary.head_offset)
        |> function
        | Some index -> Some index
        | None -> refreshed.current
      in
      { session with search = Some { refreshed with current } }

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
  | Save | Save_as | Quit | Force_quit | Reload_config | Start_search
  | Search_next | Search_previous | Open_palette | Switch_model | Help ->
      false

let palette_items session =
  let from_descriptor action descriptor =
    {
      id = Command_descriptor.id descriptor |> Command_id.to_string;
      title = Command_descriptor.title descriptor;
      description = Command_descriptor.description descriptor;
      provider = Command_descriptor.provider descriptor;
      action;
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
    (fun left right -> String.compare left.id right.id)
    (host @ model_and_extensions)

let matching_palette_items session query =
  palette_items session
  |> List.filter (fun item ->
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
    match active_from_shared target (shared_state session.active) with
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
          | Structural_runtime _ -> "Structural editing model"
        in
        {
          session with
          active;
          interaction = Idle;
          message = Some ("switched editing model to " ^ name);
          inspector = None;
          quit_armed = false;
        }

let save_to session path =
  let completed =
    match
      File_io.save_atomic ~path
        ~contents:(Editor_context.contents (context session))
    with
    | Error error ->
        {
          session with
          interaction = Idle;
          message = Some (File_io.to_string error);
          quit_armed = false;
        }
    | Ok () ->
        let saved =
          {
            session with
            file_path = Some path;
            saved_version = Editor_context.document_version (context session);
            saved_contents = Editor_context.contents (context session);
            interaction = Idle;
            message = Some ("saved " ^ path);
            quit_armed = false;
          }
        in
        let saved =
          match session.file_path with
          | Some previous when String.equal previous path -> saved
          | None | Some _ -> replace_language_client saved path
        in
        Option.iter
          (fun client ->
            Lsp.set_execution_id client
              ~execution_id:
                (Option.value ~default:0
                   (last_execution_of_active saved.active));
            Lsp.notify_save client
              ~contents:(Editor_context.contents (context saved))
              ~document_version:
                (Editor_context.document_version (context saved)))
          saved.language_client;
        let input =
          Input_event.logical_text "s"
          |> Result.get_ok
          |> Input_event.key_press ~modifiers:[ Input_event.Control ]
        in
        run_event_hooks saved Scripting.After_save input
  in
  let execution_id =
    Option.value ~default:0 (last_execution_of_active completed.active)
  in
  trace_runtime_events
    (trace_of_active completed.active)
    (profiler_of_active completed.active)
    ~execution_id completed.plugins;
  completed

let model_choices = [ Vim; Selection; Structural ]

let begin_search ?(direction = Model_effect.Forward) session =
  {
    session with
    interaction =
      Search_prompt
        {
          query = "";
          origin = Editor_context.selections (context session);
          direction;
        };
    search = None;
    message = Some "search: enter a literal Unicode query";
    inspector = None;
  }

let search_index ~origin ~direction matches =
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

let update_search session input ~origin ~direction query =
  if String.length query = 0 then
    { session with search = None; message = Some "search: enter literal text" }
  else
    let search = search_with_query session query in
    match search.matches with
    | [] ->
        {
          session with
          search = Some search;
          message = Some ("search: no matches for " ^ query);
        }
    | matches ->
        move_to_search_match session input search
          (search_index ~origin ~direction matches)

let handle_model_search_request session input = function
  | Model_effect.Request_search direction -> begin_search ~direction session
  | Model_effect.Repeat_search Model_effect.Forward ->
      move_search session input 1
  | Model_effect.Repeat_search Model_effect.Backward ->
      move_search session input (-1)
  | _ -> session

let save session =
  match session.file_path with
  | None ->
      {
        session with
        interaction = Save_as_prompt "";
        message = Some "save-as: enter a destination path";
        quit_armed = false;
      }
  | Some path -> save_to session path

let invoke_host_palette_command session input = function
  | Save -> save session
  | Save_as ->
      {
        session with
        interaction = Save_as_prompt "";
        message = Some "save-as: enter a destination path";
        quit_armed = false;
        inspector = None;
      }
  | Reload_config -> { (reload_config session) with interaction = Idle }
  | Start_search -> begin_search session
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
  | Open_palette ->
      {
        session with
        interaction = Palette { query = ""; selected = 0 };
        message = Some "command palette: filter active commands";
        quit_armed = false;
        inspector = None;
      }
  | Switch_model ->
      let current =
        model_choices
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

let invoke_palette_item session input item =
  match item.action with
  | Invoke_command id ->
      let invocation =
        Command_invocation.create ~id ~arguments:[] |> Result.get_ok
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
      invoke_host_palette_command session input command

let input_for_interaction session input =
  match session.interaction with
  | Idle -> (
      if is_shortcut input ~text:"f" ~modifiers:[ Input_event.Control ] then
        begin_search session
      else if
        is_shortcut input ~text:"g"
          ~modifiers:[ Input_event.Shift; Input_event.Control ]
      then move_search session input (-1)
      else if is_shortcut input ~text:"g" ~modifiers:[ Input_event.Control ]
      then move_search session input 1
      else if
        is_shortcut input ~text:" " ~modifiers:[ Input_event.Control ]
        || is_shortcut input ~text:"\000" ~modifiers:[ Input_event.Control ]
      then begin_completion session
      else if is_shortcut input ~text:"p" ~modifiers:[ Input_event.Control ]
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
          model_choices
          |> List.find_index (fun candidate -> candidate = model session)
          |> Option.value ~default:0
        in
        { session with interaction = Model_picker selected; inspector = None }
      else if
        is_shortcut input ~text:"h" ~modifiers:[ Input_event.Alt ]
        || is_shortcut input ~text:"h" ~modifiers:[ Input_event.Meta ]
      then { session with interaction = Help_view; inspector = None }
      else
        match matching_binding session input with
        | Some binding -> fst (invoke_bound_command session input binding)
        | None ->
            let session, effects = handle_model_input session input in
            List.fold_left
              (fun session request ->
                handle_model_search_request session input request)
              session effects)
  | Search_prompt { query; origin; direction } -> (
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
          update_search session input ~origin ~direction next_query
        in
        {
          updated with
          interaction = Search_prompt { query = next_query; origin; direction };
        }
      else
        match event_text input with
        | None -> session
        | Some text ->
            let next_query = query ^ text in
            let updated =
              update_search session input ~origin ~direction next_query
            in
            {
              updated with
              interaction =
                Search_prompt { query = next_query; origin; direction };
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
  | Save_as_prompt path -> (
      if event_is_named input Input_event.Escape then
        { session with interaction = Idle; message = Some "save-as cancelled" }
      else if event_is_named input Input_event.Enter then
        if String.length path = 0 then
          { session with message = Some "save-as: destination path is empty" }
        else save_to session path
      else if event_is_named input Input_event.Backspace then
        { session with interaction = Save_as_prompt (drop_last_utf8 path) }
      else
        match event_text input with
        | None -> session
        | Some text ->
            { session with interaction = Save_as_prompt (path ^ text) })
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
            Model_picker (min (List.length model_choices - 1) (selected + 1));
        }
      else if event_is_named input Input_event.Enter then
        switch_to_model session (List.nth model_choices selected)
      else
        match event_text input with
        | Some "1" -> switch_to_model session Vim
        | Some "2" -> switch_to_model session Selection
        | Some "3" -> switch_to_model session Structural
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

let handle_input session input =
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
  let completed = observe_language_document_version completed in
  let execution_id =
    Option.value ~default:0 (last_execution_of_active completed.active)
  in
  trace_runtime_events
    (trace_of_active completed.active)
    (profiler_of_active completed.active)
    ~execution_id completed.plugins;
  completed

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
  | Reload_config -> Continue (reload_config session)
  | Start_search -> Continue { (begin_search session) with quit_armed = false }
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
  | Open_palette ->
      Continue
        {
          session with
          interaction = Palette { query = ""; selected = 0 };
          message = Some "command palette: filter active commands";
          quit_armed = false;
          inspector = None;
        }
  | Switch_model ->
      let current =
        model_choices
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
  | Structural_runtime runtime -> Structural_runtime.input_rules runtime

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
  | Structural -> "Structural"

let interaction_overlay session =
  match session.interaction with
  | Idle | Search_prompt _ | Save_as_prompt _ | Rename_prompt _ -> None
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
              model_choices
        @ [ ""; "Arrow keys or 1/2/3 select; Enter confirms; Escape cancels." ]
        )
  | Palette { query; selected } ->
      let items = matching_palette_items session query in
      let visible =
        items
        |> List.mapi (fun index item ->
            Printf.sprintf "%s%s — %s [%s]"
              (if index = selected then "> " else "  ")
              item.id item.title
              (Provider.id item.provider))
        |> fun values ->
        let rec take remaining = function
          | _ when remaining <= 0 -> []
          | [] -> []
          | value :: rest -> value :: take (remaining - 1) rest
        in
        take 16 values
      in
      Some
        ([ "Command palette"; "filter: " ^ query; "" ]
        @ (if visible = [] then [ "  no matching commands" ] else visible)
        @ [ ""; "All active builtin, script, and plugin commands are listed." ]
        )
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
  | Search_prompt { query; _ } ->
      let count = List.length (search_ranges session) in
      Some
        (Printf.sprintf "/%s  %d match%s" query count
           (if count = 1 then "" else "es"))
  | Save_as_prompt path -> Some ("destination: " ^ path)
  | Rename_prompt name -> Some ("rename: " ^ name)
  | Completion_view { query; _ } -> Some ("completion: " ^ query)
  | Idle | Palette _ | Model_picker _ | Help_view | Hover_view _ ->
      session.message

let render session =
  let presentation = presentation_cache session in
  let rendered =
    Zenbu_view.Renderer.render_with_inspector ~context:(context session)
      ~status:(status session) ~filename:(filename session)
      ~dirty:(dirty session)
      ~message:(interaction_message session)
      ~viewport:session.viewport ~dimensions:session.dimensions
      ~inspector:session.inspector
      ?overlay:(interaction_overlay session)
      ~source_lines:presentation.source_lines
      ~syntax_spans:presentation.syntax_spans
      ~search_ranges:(search_ranges session)
      ~diagnostic_ranges:(diagnostic_ranges session)
      ?diagnostic_summary:(diagnostic_summary session)
      ()
  in
  ( {
      session with
      viewport = rendered.viewport;
      presentation_cache = Some presentation;
    },
    rendered.frame )

let contents session = Editor_context.contents (context session)
let file_path session = session.file_path
let dimensions session = session.dimensions
let viewport session = session.viewport

let notice session message =
  { session with message = Some message; quit_armed = false; inspector = None }

let all_models =
  [
    Vim_model.descriptor;
    Selection_model.descriptor;
    Structural_model.descriptor;
  ]

let scope_to_string = function
  | Scripting.Global -> "global"
  | Scripting.Model model -> "model:" ^ model
  | Scripting.Model_status { model; status } -> "model:" ^ model ^ ":" ^ status

let script_binding_lines session =
  let bindings =
    (match session.generation with
      | None -> []
      | Some generation -> Scripting.bindings generation)
    @ Plugins.bindings session.plugins
  in
  match bindings with
  | [] -> [ "extension overlays: none" ]
  | bindings ->
      bindings
      |> List.map (fun binding ->
          Printf.sprintf "script overlay: %s -> %s (%s; provider %s)"
            (Input_event.to_string (Scripting.binding_input binding))
            (Scripting.binding_command binding)
            (scope_to_string (Scripting.binding_scope binding))
            (Provider.id (Scripting.binding_provider binding)))

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
           @ host_binding_lines ()
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
              "query: " ^ search.query;
              "matches: " ^ string_of_int (List.length search.matches);
              (match search.current with
              | None -> "current-match: none"
              | Some index -> "current-match: " ^ string_of_int (index + 1));
              (match session.interaction with
              | Search_prompt _ -> "prompt: open"
              | Idle | Palette _ | Save_as_prompt _ | Model_picker _ | Help_view
              | Hover_view _ | Completion_view _ | Rename_prompt _ ->
                  "prompt: closed");
            ])
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
              Printf.sprintf
                "registrations: %d commands, %d selectors, %d transformations, \
                 %d bindings, %d hooks"
                commands selectors transformations bindings hooks;
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

let toggle_inspector session =
  match session.inspector with
  | Some _ -> { session with inspector = None }
  | None -> { session with inspector = Some (inspect session Why) }

let inspector_open session = Option.is_some session.inspector

let language_wakeup_fd session =
  Option.map Lsp.wakeup_fd session.language_client

let close session = Option.iter Lsp.close session.language_client
