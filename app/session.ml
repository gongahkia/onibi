open Zenbu_kernel
open Zenbu_model_api
open Zenbu_syntax
open Zenbu_proof_models
open Zenbu_structural_model
module Scripting = Zenbu_scripting.Scripting
module Plugins = Zenbu_extension.Plugin_host
module Vim_runtime = Model_runtime.Make (Vim_model)
module Selection_runtime = Model_runtime.Make (Selection_model)
module Structural_runtime = Model_runtime.Make (Structural_model)

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
  | Plugins

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
  last_reload_error : string option;
  delivering_events : Scripting.event list;
  file_path : string option;
  saved_version : int;
  saved_contents : string;
  viewport : Zenbu_view.Viewport.t;
  dimensions : Zenbu_view.Renderer.dimensions;
  message : string option;
  quit_armed : bool;
  inspector : string list option;
}

let static = function
  | Ok value -> value
  | Error error -> failwith (Error.to_string error)

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
  Inspector.semantic_registry () |> Semantic_registry.descriptors

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
  Semantic_behavior_registry.merge
    (semantic_behaviors generation)
    (Plugins.semantic_behaviors plugins)
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
    ?(config = Scripting.Default) ?(plugins = Plugins.Disabled) ~dimensions () =
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
              let generation, config_message =
                match
                  Profiler.measure profiler Profiler.Script_load (fun () ->
                      Scripting.load ~generation_id:1 ~base_commands
                        ~base_semantics config)
                with
                | Ok generation -> (generation, None)
                | Error error ->
                    ( None,
                      Some ("configuration not loaded: " ^ Error.to_string error)
                    )
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
                  {
                    active;
                    base_commands;
                    base_semantics;
                    config;
                    generation;
                    plugins = plugin_host;
                    next_generation_id = 2;
                    last_reload_error = config_message;
                    delivering_events = [];
                    file_path;
                    saved_version = 0;
                    saved_contents = contents;
                    viewport = Zenbu_view.Viewport.origin;
                    dimensions;
                    message = config_message;
                    quit_armed = false;
                    inspector = None;
                  })))

let context = function
  | { active = Vim_runtime runtime; _ } -> Vim_runtime.context runtime
  | { active = Selection_runtime runtime; _ } ->
      Selection_runtime.context runtime
  | { active = Structural_runtime runtime; _ } ->
      Structural_runtime.context runtime

let status = function
  | { active = Vim_runtime runtime; _ } -> Vim_runtime.status runtime
  | { active = Selection_runtime runtime; _ } ->
      Selection_runtime.status runtime
  | { active = Structural_runtime runtime; _ } ->
      Structural_runtime.status runtime

let filename session =
  match session.file_path with
  | None -> "[No Name]"
  | Some path -> Filename.basename path

let dirty session =
  let context = context session in
  Editor_context.document_version context <> session.saved_version
  && not (String.equal (Editor_context.contents context) session.saved_contents)

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
            {
              session with
              message = Some (Error.to_string error);
              quit_armed = false;
              inspector = None;
            }
        | Ok (runtime, step) ->
            {
              session with
              active = Vim_runtime runtime;
              message = last_message (Vim_runtime.messages step);
              quit_armed = false;
              inspector = None;
            })
    | Selection_runtime runtime -> (
        match Selection_runtime.handle_input runtime input with
        | Error error ->
            {
              session with
              message = Some (Error.to_string error);
              quit_armed = false;
              inspector = None;
            }
        | Ok (runtime, step) ->
            {
              session with
              active = Selection_runtime runtime;
              message = last_message (Selection_runtime.messages step);
              quit_armed = false;
              inspector = None;
            })
    | Structural_runtime runtime -> (
        match Structural_runtime.handle_input runtime input with
        | Error error ->
            {
              session with
              message = Some (Error.to_string error);
              quit_armed = false;
              inspector = None;
            }
        | Ok (runtime, step) ->
            {
              session with
              active = Structural_runtime runtime;
              message = last_message (Structural_runtime.messages step);
              quit_armed = false;
              inspector = None;
            })
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
        last_reload_error = Some (Error.to_string error);
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
            last_reload_error = Some (Error.to_string error);
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

let execute_active_effects ?augment_provenance session input effects =
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

let handle_input session input =
  let contents_before = Editor_context.contents (context session) in
  let next =
    match matching_binding session input with
    | Some binding -> fst (invoke_bound_command session input binding)
    | None -> handle_model_input session input
  in
  let completed =
    if String.equal contents_before (Editor_context.contents (context next))
    then next
    else run_event_hooks next Scripting.Document_changed input
  in
  let execution_id =
    Option.value ~default:0 (last_execution_of_active completed.active)
  in
  trace_runtime_events
    (trace_of_active completed.active)
    (profiler_of_active completed.active)
    ~execution_id completed.plugins;
  completed

let save session =
  let completed =
    match session.file_path with
    | None ->
        {
          session with
          message = Some "save-as is not implemented for unnamed buffers";
          quit_armed = false;
        }
    | Some path -> (
        match
          File_io.save_atomic ~path
            ~contents:(Editor_context.contents (context session))
        with
        | Ok () ->
            let saved =
              {
                session with
                saved_version =
                  Editor_context.document_version (context session);
                saved_contents = Editor_context.contents (context session);
                message = Some ("saved " ^ path);
                quit_armed = false;
              }
            in
            let input =
              Input_event.logical_text "s"
              |> Result.get_ok
              |> Input_event.key_press ~modifiers:[ Input_event.Control ]
            in
            run_event_hooks saved Scripting.After_save input
        | Error error ->
            {
              session with
              message = Some (File_io.to_string error);
              quit_armed = false;
            })
  in
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
  | Reload_config -> Continue (reload_config session)
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

let render session =
  let rendered =
    Zenbu_view.Renderer.render_with_inspector ~context:(context session)
      ~status:(status session) ~filename:(filename session)
      ~dirty:(dirty session) ~message:session.message ~viewport:session.viewport
      ~dimensions:session.dimensions ~inspector:session.inspector
  in
  ({ session with viewport = rendered.viewport }, rendered.frame)

let contents session = Editor_context.contents (context session)
let file_path session = session.file_path
let dimensions session = session.dimensions

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
           @ script_binding_lines session)
    | Commands ->
        "Commands"
        :: Inspector.format_commands (Inspector.commands command_registry)
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
    | Api ->
        "API"
        :: Inspector.format_api
             (Inspector.api ~models:all_models ~commands:command_registry
                ~semantic_behaviors ())
    | Plugins -> "Plugins" :: plugin_lines session
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
              | Some error -> "last-reload-error: " ^ error);
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
              | Some error -> "last-reload-error: " ^ error);
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
