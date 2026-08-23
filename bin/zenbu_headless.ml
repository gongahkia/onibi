open Zenbu_kernel
open Zenbu_model_api
open Zenbu_syntax
open Zenbu_proof_models
open Zenbu_structural_model
module Scripting = Zenbu_scripting.Scripting
module Plugins = Zenbu_extension.Plugin_host
module Language = Zenbu_language.Language

let fail error =
  prerr_endline (Error.to_string error);
  exit 1

let document_for id contents =
  match Document_id.of_string id with
  | Error error -> fail error
  | Ok id -> (
      match Document.create ~id ~contents () with
      | Error error -> fail error
      | Ok document -> document)

let logical_key text =
  match Input_event.logical_text text with
  | Ok key -> Input_event.key_press key
  | Error error -> fail error

let named_key value = Input_event.key_press (Input_event.named_key value)

let control_key text =
  match Input_event.logical_text (String.lowercase_ascii text) with
  | Ok key -> Input_event.key_press ~modifiers:[ Input_event.Control ] key
  | Error error -> fail error

let shortcut_key modifiers text =
  match Input_event.logical_text (String.lowercase_ascii text) with
  | Ok key -> Input_event.key_press ~modifiers key
  | Error error -> fail error

module Vim_runtime = Model_runtime.Make (Vim_model)
module Selection_runtime = Model_runtime.Make (Selection_model)
module Structural_runtime = Model_runtime.Make (Structural_model)

let semantic_registry () =
  match
    Command_registry.register Command_registry.empty
      Semantic_commands.apply_command
  with
  | Error error -> fail error
  | Ok registry -> (
      List.fold_left
        (fun registry command ->
          match registry with
          | Error error -> fail error
          | Ok registry -> (
              match Command_registry.register registry command with
              | Ok registry -> Ok registry
              | Error error -> Error error))
        (Ok registry)
        (Syntax_commands.commands ())
      |> function
      | Ok registry -> registry
      | Error error -> fail error)

let read_file path =
  let channel = open_in_bin path in
  Fun.protect
    ~finally:(fun () -> close_in_noerr channel)
    (fun () -> really_input_string channel (in_channel_length channel))

let write_file path contents =
  let channel = open_out_bin path in
  Fun.protect
    ~finally:(fun () -> close_out_noerr channel)
    (fun () -> output_string channel contents)

let base64_digit = function
  | 'A' .. 'Z' as value -> Char.code value - Char.code 'A'
  | 'a' .. 'z' as value -> Char.code value - Char.code 'a' + 26
  | '0' .. '9' as value -> Char.code value - Char.code '0' + 52
  | '+' -> 62
  | '/' -> 63
  | value ->
      fail
        (Error.Invalid_command_arguments
           ("invalid base64 character " ^ String.make 1 value))

let decode_base64 text =
  let input =
    text |> String.to_seq |> List.of_seq
    |> List.filter (fun value -> value <> '\n' && value <> '\r' && value <> ' ')
  in
  let output = Buffer.create (String.length text * 3 / 4) in
  let rec groups = function
    | [] -> Buffer.contents output
    | first :: second :: third :: fourth :: rest ->
        let first = base64_digit first in
        let second = base64_digit second in
        let has_third = third <> '=' in
        let has_fourth = fourth <> '=' in
        let third = if has_third then base64_digit third else 0 in
        let fourth = if has_fourth then base64_digit fourth else 0 in
        Buffer.add_char output (Char.chr ((first lsl 2) lor (second lsr 4)));
        if has_third then
          Buffer.add_char output
            (Char.chr (((second land 0x0f) lsl 4) lor (third lsr 2)));
        if has_fourth then
          Buffer.add_char output
            (Char.chr (((third land 0x03) lsl 6) lor fourth));
        groups rest
    | _ -> fail (Error.Invalid_command_arguments "incomplete base64 fixture")
  in
  groups input

let print_history history =
  List.iter
    (fun change ->
      let metadata = Transaction.metadata_of (History.transaction change) in
      let intent = Option.value ~default:"-" (Transaction.intent metadata) in
      let description =
        Option.value ~default:"-" (Transaction.description metadata)
      in
      Printf.printf "change %d: %s %s (%s), v%d -> v%d\n"
        (History.change_id change)
        (Transaction.source_to_string (Transaction.source metadata))
        intent description
        (Document_version.to_int (Document.version (History.before change)))
        (Document_version.to_int (Document.version (History.after change))))
    (History.lineage history)

let print_result history =
  let snapshot = Document.snapshot (History.current history) in
  Printf.printf "text: %S\n" (Document_snapshot.contents snapshot);
  Printf.printf "version: %d\n"
    (Document_version.to_int (Document_snapshot.version snapshot));
  Printf.printf "selections:";
  List.iter
    (fun selection ->
      Printf.printf " %d:%d"
        (Anchor.byte_offset (Selection.anchor selection))
        (Anchor.byte_offset (Selection.head selection)))
    (Selection_set.to_list (Document_snapshot.selections snapshot));
  print_newline ();
  print_history history

let print_effects = function
  | [] -> Printf.printf "  effects: none\n"
  | effects ->
      List.iter
        (fun model_effect ->
          Printf.printf "  effect: %s\n" (Model_effect.describe model_effect))
        effects

let print_intents intents =
  List.iter
    (fun intent ->
      Printf.printf "  semantic intent: %s\n" (Model_intent.identity intent))
    intents

let print_step index input effects intents status history =
  Printf.printf "step %d: %s -> %s\n" index
    (Input_event.to_string input)
    (Model_status.label status);
  print_effects effects;
  print_intents intents;
  let snapshot = Document.snapshot (History.current history) in
  Printf.printf "  document: %S\n" (Document_snapshot.contents snapshot)

let run_vim_session contents inputs =
  let runtime =
    match
      Vim_runtime.create ~commands:(semantic_registry ())
        ~document:(document_for "vim-session" contents)
        ()
    with
    | Ok runtime -> runtime
    | Error error -> fail error
  in
  let runtime =
    List.fold_left
      (fun runtime input ->
        match Vim_runtime.handle_input runtime input with
        | Error error -> fail error
        | Ok (runtime, step) ->
            print_step
              (List.length (Vim_runtime.input_trace runtime))
              input (Vim_runtime.effects step) (Vim_runtime.intents step)
              (Vim_runtime.status_after step)
              (Vim_runtime.history runtime);
            runtime)
      runtime inputs
  in
  print_result (Vim_runtime.history runtime)

let run_selection_session contents inputs =
  let runtime =
    match
      Selection_runtime.create ~commands:(semantic_registry ())
        ~document:(document_for "selection-session" contents)
        ()
    with
    | Ok runtime -> runtime
    | Error error -> fail error
  in
  let runtime =
    List.fold_left
      (fun runtime input ->
        match Selection_runtime.handle_input runtime input with
        | Error error -> fail error
        | Ok (runtime, step) ->
            print_step
              (List.length (Selection_runtime.input_trace runtime))
              input
              (Selection_runtime.effects step)
              (Selection_runtime.intents step)
              (Selection_runtime.status_after step)
              (Selection_runtime.history runtime);
            runtime)
      runtime inputs
  in
  print_result (Selection_runtime.history runtime)

let run_structural_session ?(language = "ocaml") contents inputs =
  let syntax_service =
    match Syntax.Language.find language with
    | Some language -> Syntax.Service.create language
    | None ->
        fail (Error.Invalid_command_arguments ("unknown language: " ^ language))
  in
  let runtime =
    match
      Structural_runtime.create ~commands:(semantic_registry ()) ~syntax_service
        ~document:(document_for "structural-session" contents)
        ()
    with
    | Ok runtime -> runtime
    | Error error -> fail error
  in
  let runtime =
    List.fold_left
      (fun runtime input ->
        match Structural_runtime.handle_input runtime input with
        | Error error -> fail error
        | Ok (runtime, step) ->
            print_step
              (List.length (Structural_runtime.input_trace runtime))
              input
              (Structural_runtime.effects step)
              (Structural_runtime.intents step)
              (Structural_runtime.status_after step)
              (Structural_runtime.history runtime);
            runtime)
      runtime inputs
  in
  print_result (Structural_runtime.history runtime)

type session_model = Vim | Selection_first | Structural

type session = {
  model : session_model;
  language : string option;
  contents : string;
  inputs : Input_event.t list;
}

let unescape value =
  try Ok (Scanf.unescaped value)
  with Failure _ | Invalid_argument _ ->
    Error (Error.Malformed_replay "invalid session escape")

let input_of_session_value = function
  | "Escape" -> Ok (named_key Input_event.Escape)
  | "Backspace" -> Ok (named_key Input_event.Backspace)
  | "Enter" -> Ok (named_key Input_event.Enter)
  | "ArrowUp" -> Ok (named_key Input_event.Arrow_up)
  | "ArrowDown" -> Ok (named_key Input_event.Arrow_down)
  | "ArrowLeft" -> Ok (named_key Input_event.Arrow_left)
  | "ArrowRight" -> Ok (named_key Input_event.Arrow_right)
  | value when String.starts_with ~prefix:"Ctrl-Shift-" value ->
      let text =
        String.sub value 11 (String.length value - String.length "Ctrl-Shift-")
      in
      Ok (shortcut_key [ Input_event.Shift; Input_event.Control ] text)
  | value when String.starts_with ~prefix:"Ctrl-" value ->
      let text =
        String.sub value 5 (String.length value - String.length "Ctrl-")
      in
      Ok (control_key text)
  | value when String.starts_with ~prefix:"Alt-" value ->
      let text =
        String.sub value 4 (String.length value - String.length "Alt-")
      in
      Ok (shortcut_key [ Input_event.Alt ] text)
  | value -> Ok (logical_key value)

let session_of_string text =
  let parse_line session line =
    if String.length line = 0 || line.[0] = '#' then Ok session
    else
      match String.split_on_char '=' line with
      | [ "model"; "vim" ] -> Ok { session with model = Vim }
      | [ "model"; "selection-first" ] ->
          Ok { session with model = Selection_first }
      | [ "model"; "structural" ] -> Ok { session with model = Structural }
      | "language" :: value ->
          Ok { session with language = Some (String.concat "=" value) }
      | "text" :: value -> (
          match unescape (String.concat "=" value) with
          | Error _ as error -> error
          | Ok contents -> Ok { session with contents })
      | "input" :: value -> (
          match input_of_session_value (String.concat "=" value) with
          | Error _ as error -> error
          | Ok input -> Ok { session with inputs = session.inputs @ [ input ] })
      | "text-input" :: value -> (
          match unescape (String.concat "=" value) with
          | Error _ as error -> error
          | Ok value -> (
              match Input_event.text_input value with
              | Error _ as error -> error
              | Ok input ->
                  Ok { session with inputs = session.inputs @ [ input ] }))
      | _ -> Error (Error.Malformed_replay ("invalid session line " ^ line))
  in
  let initial = { model = Vim; language = None; contents = ""; inputs = [] } in
  let rec loop session = function
    | [] -> Ok session
    | line :: rest -> (
        match parse_line session line with
        | Error _ as error -> error
        | Ok session -> loop session rest)
  in
  loop initial (String.split_on_char '\n' text)

let run_session path =
  match session_of_string (read_file path) with
  | Error error -> fail error
  | Ok { model = Vim; contents; inputs; _ } -> run_vim_session contents inputs
  | Ok { model = Selection_first; contents; inputs; _ } ->
      run_selection_session contents inputs
  | Ok { model = Structural; language; contents; inputs } ->
      run_structural_session ?language contents inputs

let print_lines lines = List.iter print_endline lines

let app_model = function
  | Vim -> Zenbu_app.Session.Vim
  | Selection_first -> Zenbu_app.Session.Selection
  | Structural -> Zenbu_app.Session.Structural

let observed_session ?(config = Scripting.Disabled) inspection path =
  match session_of_string (read_file path) with
  | Error error -> fail error
  | Ok session -> (
      let trace =
        Zenbu_model_api.Trace.enabled ~capacity:1024 |> Result.get_ok
      in
      let profiler =
        Zenbu_model_api.Profiler.enabled ~capacity:1024 |> Result.get_ok
      in
      let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 } in
      match
        Zenbu_app.Session.create ~model:(app_model session.model)
          ?language:session.language ~contents:session.contents ~trace ~profiler
          ~config ~dimensions ()
      with
      | Error error -> fail error
      | Ok initial ->
          let current =
            List.fold_left Zenbu_app.Session.handle_input initial session.inputs
          in
          Zenbu_app.Session.inspect current inspection |> print_lines)

let initial_bindings model =
  let session =
    {
      model;
      language = (match model with Structural -> Some "ocaml" | _ -> None);
      contents = "let alpha = 1\n";
      inputs = [];
    }
  in
  let trace = Zenbu_model_api.Trace.disabled () in
  let profiler = Zenbu_model_api.Profiler.disabled () in
  let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 } in
  match
    Zenbu_app.Session.create ~model:(app_model session.model)
      ?language:session.language ~contents:session.contents ~trace ~profiler
      ~config:Scripting.Disabled ~dimensions ()
  with
  | Error error -> fail error
  | Ok session ->
      Zenbu_app.Session.inspect session Zenbu_app.Session.Bindings
      |> print_lines

let all_models =
  [
    Vim_model.descriptor;
    Selection_model.descriptor;
    Structural_model.descriptor;
  ]

let inspect_api () =
  Inspector.api ~models:all_models ~commands:(semantic_registry ()) ()
  |> Inspector.format_api |> print_lines

let inspect_commands () =
  Inspector.commands (semantic_registry ())
  @ (Zenbu_app.Session.host_command_descriptors ()
    |> List.map Inspector.describe_command)
  |> Inspector.format_commands |> print_lines

let inspect_description kind id =
  let print = function
    | None ->
        fail (Error.Invalid_command_arguments ("unknown " ^ kind ^ ": " ^ id))
    | Some description ->
        Inspector.format_description description |> print_lines
  in
  match kind with
  | "command" ->
      let host =
        Zenbu_app.Session.host_command_descriptors ()
        |> List.find_opt (fun descriptor ->
            Command_id.to_string (Command_descriptor.id descriptor) = id)
        |> Option.map Inspector.describe_command
      in
      print
        (match Inspector.find_command (semantic_registry ()) id with
        | Some description -> Some description
        | None -> host)
  | "model" ->
      all_models
      |> List.find_opt (fun descriptor -> Editing_model.id descriptor = id)
      |> Option.map Inspector.describe_model
      |> print
  | "selector" | "transformation" ->
      Inspector.semantic_registry () |> fun registry ->
      (match Inspector.find_semantic registry id with
        | Some description when Inspector.description_kind description = kind ->
            Some description
        | Some _ | None -> None)
      |> print
  | _ ->
      fail
        (Error.Invalid_command_arguments
           "describe expects command, model, selector, or transformation")

let script_base_semantics () =
  Inspector.semantic_registry () |> Semantic_registry.descriptors

let print_plugin_view view =
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
  Printf.printf "%s %s %s %s\n" id version
    (Plugins.state_name (Plugins.view_state view))
    runtime;
  Printf.printf "  health: %s\n"
    (Plugins.health_name (Plugins.view_health view));
  Printf.printf "  manifest: %s\n" (Plugins.view_manifest_path view);
  Printf.printf "  capabilities: %s\n"
    (Plugins.view_granted_capabilities view
    |> List.map Zenbu_extension.Capability.id
    |> String.concat ", ");
  Printf.printf "  contributions: %s\n"
    (Plugins.view_contributions view
    |> List.map Zenbu_extension.Contribution.id
    |> String.concat ", ");
  (match Plugins.view_runtime_limits view with
  | None -> Printf.printf "  limits: none\n"
  | Some (fuel, memory_bytes) ->
      Printf.printf "  limits: fuel=%d memory-bytes=%d\n" fuel memory_bytes);
  Printf.printf "  registrations: %s\n"
    (Plugins.view_registered_ids view |> String.concat ", ");
  Option.iter
    (fun error -> Printf.printf "  error: %s\n" (Error.to_string error))
    (Plugins.view_error view)

let plugin_host config =
  Plugins.load ~config ~base_commands:(semantic_registry ())
    ~base_semantics:(script_base_semantics ()) ()

let plugins config =
  let host = plugin_host config in
  Fun.protect
    ~finally:(fun () -> Plugins.dispose host)
    (fun () ->
      match Plugins.views host with
      | [] -> print_endline "plugins: none"
      | views -> List.iter print_plugin_view views)

let plugin_check path =
  let package_dir =
    if String.equal (Filename.basename path) Zenbu_extension.Manifest.filename
    then Filename.dirname path
    else path
  in
  let host = plugin_host (Plugins.Directories [ package_dir ]) in
  Fun.protect
    ~finally:(fun () -> Plugins.dispose host)
    (fun () ->
      let manifest =
        Filename.concat package_dir Zenbu_extension.Manifest.filename
      in
      match
        Plugins.views host
        |> List.find_opt (fun view ->
            String.equal (Plugins.view_manifest_path view) manifest)
      with
      | None ->
          fail
            (Error.Extension_error
               {
                 code = Error.Invalid_plugin_package;
                 plugin_id = None;
                 provider = None;
                 operation = Some "plugin-check";
                 required = None;
                 granted = [];
                 message = "plugin package was not discovered";
               })
      | Some view -> (
          print_plugin_view view;
          Printf.printf "  bindings: %d\n" (List.length (Plugins.bindings host));
          match Plugins.view_error view with
          | None -> ()
          | Some error -> fail error))

let extension_api () = print_string (Zenbu_extension.Contract.markdown ())
let extension_sdk () = print_string (Zenbu_extension.Contract.lua_stub ())
let extension_wit () = print_string (Zenbu_extension.Contract.wit ())

let check_config path =
  match
    Scripting.check_file ~base_commands:(semantic_registry ())
      ~base_semantics:(script_base_semantics ()) path
  with
  | Error error -> fail error
  | Ok (commands, selectors, transformations, bindings, hooks) ->
      Printf.printf
        "config ok: %d commands, %d selectors, %d transformations, %d \
         bindings, %d hooks\n"
        commands selectors transformations bindings hooks

let describe_config path =
  match
    Scripting.load ~generation_id:1 ~base_commands:(semantic_registry ())
      ~base_semantics:(script_base_semantics ()) (Scripting.Explicit path)
  with
  | Error error -> fail error
  | Ok None -> print_endline "no script generation"
  | Ok (Some generation) ->
      Printf.printf "generation: %d\nsource: %s\nprovider: %s\n"
        (Scripting.generation_id generation)
        (Scripting.source generation)
        (Zenbu_kernel.Provider.id (Scripting.provider generation));
      List.iter
        (fun command ->
          let descriptor = Command.descriptor command in
          Printf.printf "command: %s\n"
            (Command_descriptor.id descriptor |> Command_id.to_string))
        (Scripting.commands generation);
      List.iter
        (fun descriptor ->
          Printf.printf "%s: %s\n"
            (match Semantic_descriptor.kind descriptor with
            | Semantic_descriptor.Selector -> "selector"
            | Semantic_descriptor.Transformation -> "transformation")
            (Semantic_descriptor.id descriptor))
        (Scripting.descriptors generation);
      List.iter
        (fun binding ->
          Printf.printf "binding: %s -> %s\n"
            (Input_event.binding_pattern_sequence_to_string
               (Scripting.binding_inputs binding))
            (Scripting.binding_command binding))
        (Scripting.bindings generation);
      Scripting.dispose generation

let script_session config_path session_path =
  let config = Scripting.Explicit config_path in
  match session_of_string (read_file session_path) with
  | Error error -> fail error
  | Ok definition -> (
      let trace = Trace.enabled ~capacity:1024 |> Result.get_ok in
      let profiler = Profiler.enabled ~capacity:1024 |> Result.get_ok in
      let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 } in
      match
        Zenbu_app.Session.create
          ~model:(app_model definition.model)
          ?language:definition.language ~contents:definition.contents ~trace
          ~profiler ~config ~dimensions ()
      with
      | Error error -> fail error
      | Ok session ->
          let session =
            List.fold_left Zenbu_app.Session.handle_input session
              definition.inputs
          in
          Printf.printf "text: %S\n" (Zenbu_app.Session.contents session);
          List.iter print_endline
            (Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts);
          List.iter print_endline
            (Zenbu_app.Session.inspect session Zenbu_app.Session.History))

let plugin_session plugin_directory session_path =
  match session_of_string (read_file session_path) with
  | Error error -> fail error
  | Ok definition -> (
      let trace = Trace.enabled ~capacity:1024 |> Result.get_ok in
      let profiler = Profiler.enabled ~capacity:1024 |> Result.get_ok in
      let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 } in
      match
        Zenbu_app.Session.create
          ~model:(app_model definition.model)
          ?language:definition.language ~contents:definition.contents ~trace
          ~profiler ~config:Scripting.Disabled
          ~plugins:(Plugins.Directories [ plugin_directory ]) ~dimensions ()
      with
      | Error error -> fail error
      | Ok session ->
          let session =
            List.fold_left Zenbu_app.Session.handle_input session
              definition.inputs
          in
          Printf.printf "text: %S\n" (Zenbu_app.Session.contents session);
          List.iter print_endline
            (Zenbu_app.Session.inspect session Zenbu_app.Session.Plugins);
          List.iter print_endline
            (Zenbu_app.Session.inspect session Zenbu_app.Session.Bindings);
          List.iter print_endline
            (Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts);
          List.iter print_endline
            (Zenbu_app.Session.inspect session Zenbu_app.Session.Why);
          List.iter print_endline
            (Zenbu_app.Session.inspect session Zenbu_app.Session.History))

let script_demo_config prefix suffix =
  Printf.sprintf
    {|
zenbu.selector {
  id = "demo.document",
  title = "Demo document",
  description = "Select the complete document.",
  run = function(call)
    return { selections = {{ anchor = 0, head = call.context.document.length }}, primary = 1 }
  end,
}
zenbu.transform {
  id = "demo.surround",
  title = "Demo surround",
  description = "Surround selected text.",
  run = function(call)
    local selection = call.arguments.selection_set[1]
    local start = math.min(selection.anchor, selection.head)
    local stop = math.max(selection.anchor, selection.head)
    return { edits = {
      { start = start, stop = start, text = %S },
      { start = stop, stop = stop, text = %S },
    }}
  end,
}
zenbu.command {
  id = "demo.wrap",
  title = "Demo wrap",
  description = "Run the registered selector and transform.",
  run = function(_) return {{ kind = "apply", selector = "demo.document", transformation = "demo.surround" }} end,
}
zenbu.bind { input = "Ctrl-K", command = "demo.wrap" }
|}
    prefix suffix

let run_script_demo () =
  let path = Filename.temp_file "zenbu-m7-demo" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write_file path (script_demo_config "[" "]");
      let trace = Trace.enabled ~capacity:128 |> Result.get_ok in
      let profiler = Profiler.enabled ~capacity:128 |> Result.get_ok in
      let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 } in
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~trace ~profiler ~config:(Scripting.Explicit path) ~dimensions ()
        |> Result.get_ok
      in
      let session = Zenbu_app.Session.handle_input session (control_key "K") in
      Printf.printf "\nSCRIPTING: Ctrl-K with generation 1 -> %S\n"
        (Zenbu_app.Session.contents session);
      write_file path (script_demo_config "(" ")");
      let session = Zenbu_app.Session.reload_config session in
      let session = Zenbu_app.Session.handle_input session (control_key "K") in
      Printf.printf "SCRIPTING: reload + Ctrl-K -> %S\n"
        (Zenbu_app.Session.contents session);
      Zenbu_app.Session.inspect session Zenbu_app.Session.Why |> print_lines)

let print_demo_why model ?language contents inputs =
  let trace = Zenbu_model_api.Trace.enabled ~capacity:128 |> Result.get_ok in
  let profiler = Zenbu_model_api.Profiler.disabled () in
  let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 } in
  match
    Zenbu_app.Session.create ~model ?language ~contents ~trace ~profiler
      ~config:Scripting.Disabled ~dimensions ()
  with
  | Error error -> fail error
  | Ok session ->
      List.fold_left Zenbu_app.Session.handle_input session inputs
      |> fun session ->
      Zenbu_app.Session.inspect session Zenbu_app.Session.Why |> print_lines

let component_demo_manifest =
  {|
manifest_version = 1

[plugin]
id = "com.example.conformance"
name = "Component conformance demo"
version = "1.0.0"
api = 1
runtime = "wasm-component"
entrypoint = "plugin.wasm"
contributions = ["commands", "selectors", "transformations", "bindings", "events"]
capabilities = ["document.read", "document.edit", "selection.read", "selection.write", "ui.message", "event.subscribe"]

[wasm]
fuel = 5000000
memory_bytes = 16777216
|}

let with_component_demo run =
  let root = Filename.temp_file "zenbu-m9-demo" "" in
  Sys.remove root;
  Unix.mkdir root 0o700;
  let package = Filename.concat root "component" in
  Unix.mkdir package 0o700;
  let entrypoint = Filename.concat package "plugin.wasm" in
  let manifest = Filename.concat package "zenbu-plugin.toml" in
  Fun.protect
    ~finally:(fun () ->
      List.iter
        (fun path -> if Sys.file_exists path then Sys.remove path)
        [ entrypoint; manifest ];
      Unix.rmdir package;
      Unix.rmdir root)
    (fun () ->
      write_file entrypoint
        (read_file "test/fixtures/m9_conformance_component.wasm.b64"
        |> decode_base64);
      write_file manifest component_demo_manifest;
      run root)

let run_component_demo () =
  with_component_demo (fun root ->
      let trace = Trace.enabled ~capacity:128 |> Result.get_ok in
      let profiler = Profiler.enabled ~capacity:128 |> Result.get_ok in
      let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 } in
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~trace ~profiler ~config:Scripting.Disabled
          ~plugins:(Plugins.Directories [ root ]) ~dimensions ()
        |> Result.get_ok
      in
      Printf.printf "\nWASM COMPONENT PLUGIN\n";
      Zenbu_app.Session.inspect session Zenbu_app.Session.Plugins |> print_lines;
      let session = Zenbu_app.Session.handle_input session (control_key "K") in
      Printf.printf "input: Ctrl-K\nsemantic result: %S\n"
        (Zenbu_app.Session.contents session);
      Zenbu_app.Session.inspect session Zenbu_app.Session.Why |> print_lines;
      let plugins =
        Plugins.load ~config:(Plugins.Directories [ root ])
          ~base_commands:(semantic_registry ()) ~base_semantics:[] ()
      in
      Fun.protect
        ~finally:(fun () -> Plugins.dispose plugins)
        (fun () ->
          let commands =
            List.fold_left
              (fun registry command ->
                Command_registry.register registry command |> Result.get_ok)
              (semantic_registry ()) (Plugins.commands plugins)
          in
          let runtime =
            Vim_runtime.create ~commands
              ~semantic_behaviors:(Plugins.semantic_behaviors plugins)
              ~document:(document_for "component-demo" "alpha beta")
              ()
            |> Result.get_ok
          in
          let invoke runtime id =
            let invocation =
              Command_invocation.create
                ~id:(Command_id.of_string id |> Result.get_ok)
                ~arguments:[]
              |> Result.get_ok
            in
            Vim_runtime.invoke_command runtime ~input:(control_key "K")
              invocation
          in
          Printf.printf "malicious plugin: infinite loop\n";
          (match invoke runtime "com.example.conformance.loop" with
          | Error
              (Error.Extension_error
                 { code = Error.Extension_fuel_exhausted; _ }) ->
              Printf.printf "result: extension-fuel-exhausted\n"
          | Error error -> Printf.printf "result: %s\n" (Error.to_string error)
          | Ok _ -> Printf.printf "result: unexpected success\n");
          let recovered =
            Vim_runtime.handle_input runtime (logical_key "d")
            |> Result.get_ok |> fst
          in
          match Vim_runtime.handle_input recovered (logical_key "w") with
          | Error error -> fail error
          | Ok (recovered, _) ->
              Printf.printf "editor remains operational: %S\n"
                (Vim_runtime.context recovered |> Editor_context.contents)))

let demo () =
  Printf.printf
    "Zenbu M11: one semantic kernel, practical host and language tools\n\n";
  Printf.printf "Initial: \"alpha beta gamma\"\n\nVIM-STYLE: d w\n";
  run_vim_session "alpha beta gamma" [ logical_key "d"; logical_key "w" ];
  print_demo_why Zenbu_app.Session.Vim "alpha beta gamma"
    [ logical_key "d"; logical_key "w" ];
  Printf.printf "\nSELECTION-FIRST: w d\n";
  run_selection_session "alpha beta gamma" [ logical_key "w"; logical_key "d" ];
  print_demo_why Zenbu_app.Session.Selection "alpha beta gamma"
    [ logical_key "w"; logical_key "d" ];
  Printf.printf "\nSELECTION-FIRST MULTI-SELECTION: W * d\n";
  run_selection_session "foo bar foo baz foo"
    [ logical_key "W"; logical_key "*"; logical_key "d" ];
  print_demo_why Zenbu_app.Session.Selection "foo bar foo baz foo"
    [ logical_key "W"; logical_key "*"; logical_key "d" ];
  Printf.printf "\nSTRUCTURAL: f ArrowUp ArrowDown ArrowRight x\n";
  run_structural_session "let alpha = 1\nlet beta = 2\n"
    [
      logical_key "f";
      named_key Input_event.Arrow_up;
      named_key Input_event.Arrow_down;
      named_key Input_event.Arrow_right;
      logical_key "x";
    ];
  print_demo_why Zenbu_app.Session.Structural ~language:"ocaml"
    "let alpha = 1\nlet beta = 2\n"
    [
      logical_key "f";
      named_key Input_event.Arrow_up;
      named_key Input_event.Arrow_down;
      named_key Input_event.Arrow_right;
      logical_key "x";
    ];
  run_script_demo ();
  run_component_demo ()

let rec print_node indent node =
  Printf.printf "%s%s %d:%d named=%b error=%b missing=%b\n" indent
    (Syntax.Snapshot.Node.kind node |> Syntax.Kind.to_string)
    (Syntax.Snapshot.Node.start_offset node)
    (Syntax.Snapshot.Node.stop_offset node)
    (Syntax.Snapshot.Node.is_named node)
    (Syntax.Snapshot.Node.is_error node)
    (Syntax.Snapshot.Node.is_missing node);
  List.iter
    (print_node (indent ^ "  "))
    (Syntax.Snapshot.Node.named_children node)

let inspect_syntax path =
  match Syntax.Language.detect_path path with
  | None ->
      fail (Error.Invalid_command_arguments ("no syntax language for " ^ path))
  | Some language -> (
      let document = document_for path (read_file path) in
      let service = Syntax.Service.create language in
      match Syntax.Service.refresh service (Document.snapshot document) with
      | Error error ->
          fail (Error.Model_execution_failed (Syntax.Error.to_string error))
      | Ok snapshot ->
          Printf.printf "language: %s\ndocument: %s@%d\nhas-error: %b\n"
            (Syntax.Language.id (Syntax.Snapshot.language snapshot))
            (Syntax.Snapshot.document_id snapshot)
            (Syntax.Snapshot.document_version snapshot)
            (Syntax.Snapshot.has_error snapshot);
          print_node "" (Syntax.Snapshot.root snapshot))

let language_status path =
  let session =
    Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~file_path:path
      ~contents:(read_file path)
      ~dimensions:Zenbu_view.Renderer.{ columns = 80; rows = 24 }
      ()
    |> function
    | Ok session -> session
    | Error error -> fail error
  in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        match Zenbu_app.Session.language_wakeup_fd session with
        | None -> session
        | Some fd ->
            ignore (Unix.select [ fd ] [] [] 0.2);
            Zenbu_app.Session.poll_language session
      in
      Zenbu_app.Session.inspect session Zenbu_app.Session.Language
      |> List.iter print_endline)

let language_fake_session executable path =
  let extension =
    match Filename.extension path with "" -> ".txt" | extension -> extension
  in
  let server =
    Language.Server_config.create ~id:"headless.fake" ~language_ids:[ "fake" ]
      ~extensions:[ extension ] ~executable ~root_markers:[] ()
    |> function
    | Ok server -> server
    | Error reason -> fail (Error.Invalid_command_arguments reason)
  in
  let language_registry =
    Language.Registry.register Language.Registry.empty server |> function
    | Ok registry -> registry
    | Error reason -> fail (Error.Invalid_command_arguments reason)
  in
  let session =
    Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~file_path:path
      ~contents:(read_file path) ~language_registry
      ~dimensions:Zenbu_view.Renderer.{ columns = 80; rows = 24 }
      ()
    |> function
    | Ok session -> session
    | Error error -> fail error
  in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let deadline = Unix.gettimeofday () +. 1.0 in
      let rec poll session =
        if Unix.gettimeofday () >= deadline then session
        else
          match Zenbu_app.Session.language_wakeup_fd session with
          | None -> session
          | Some fd ->
              ignore (Unix.select [ fd ] [] [] 0.05);
              let session = Zenbu_app.Session.poll_language session in
              let ready =
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready")
              in
              if ready then session else poll session
      in
      let session = poll session in
      Zenbu_app.Session.inspect session Zenbu_app.Session.Language
      |> List.iter print_endline)

let lsp_position encoding offset path =
  let encoding =
    match Language.Position.of_name encoding with
    | Some encoding -> encoding
    | None ->
        fail
          (Error.Invalid_command_arguments
             "position encoding must be utf-8, utf-16, or utf-32")
  in
  let contents = read_file path in
  match
    Language.Position.offset_to_position ~contents ~encoding ~byte_offset:offset
  with
  | Error reason -> fail (Error.Invalid_command_arguments reason)
  | Ok position ->
      Printf.printf "encoding: %s\nbyte-offset: %d\nline: %d\ncharacter: %d\n"
        (Language.Position.encoding_name encoding)
        offset position.line position.character

let measure_seconds run =
  let started = Unix.gettimeofday () in
  let value = run () in
  (value, Unix.gettimeofday () -. started)

let report_benchmark name seconds =
  Printf.printf "%-30s %.3f ms\n" name (seconds *. 1000.)

let source_root () =
  Option.value ~default:(Sys.getcwd ()) (Sys.getenv_opt "DUNE_SOURCEROOT")

let source_path path = Filename.concat (source_root ()) path

let generated_ocaml_source ~bytes =
  let line = "let benchmark_value = 12345 (* syntax benchmark *)\n" in
  let contents = Buffer.create bytes in
  while Buffer.length contents < bytes do
    Buffer.add_string contents line
  done;
  Buffer.contents contents

let benchmark_session ?language ?(config = Scripting.Disabled)
    ?(plugins = Plugins.Disabled) contents =
  Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ?language ~contents
    ~config ~plugins
    ~dimensions:Zenbu_view.Renderer.{ columns = 100; rows = 30 }
    ()
  |> function
  | Ok value -> value
  | Error error -> fail error

let temporary_directory prefix =
  let directory = Filename.temp_file prefix "" in
  Sys.remove directory;
  Unix.mkdir directory 0o700;
  directory

let copy_file source destination = write_file destination (read_file source)

let benchmark_component_callback () =
  let root = temporary_directory "zenbu-benchmark-component" in
  let package = Filename.concat root "component" in
  Unix.mkdir package 0o700;
  let source = source_path "examples/wasm-component-conformance" in
  Fun.protect
    ~finally:(fun () ->
      List.iter
        (fun path -> if Sys.file_exists path then Sys.remove path)
        [
          Filename.concat package "zenbu-plugin.toml";
          Filename.concat package "plugin.wasm";
        ];
      Unix.rmdir package;
      Unix.rmdir root)
    (fun () ->
      copy_file
        (Filename.concat source "zenbu-plugin.toml")
        (Filename.concat package "zenbu-plugin.toml");
      copy_file
        (Filename.concat source "plugin.wasm")
        (Filename.concat package "plugin.wasm");
      let session =
        benchmark_session ~plugins:(Plugins.Directories [ root ]) "alpha"
      in
      measure_seconds (fun () ->
          Zenbu_app.Session.handle_input session (control_key "K")))

let benchmark () =
  let small = "alpha beta gamma\n" in
  let large = generated_ocaml_source ~bytes:(1024 * 1024) in
  print_endline "Zenbu M11 benchmark (single process; lower is better)";
  let _, startup = measure_seconds (fun () -> benchmark_session small) in
  report_benchmark "empty-session initialization" startup;
  let _, lsp_position =
    measure_seconds (fun () ->
        match
          Language.Position.offset_to_position ~contents:large
            ~encoding:Language.Position.Utf16
            ~byte_offset:(String.length large / 2)
        with
        | Ok _ -> ()
        | Error reason -> fail (Error.Invalid_command_arguments reason))
  in
  report_benchmark "LSP UTF-16 position conversion" lsp_position;
  let _, lsp_sync =
    measure_seconds (fun () ->
        match
          Language.Sync.incremental_changes ~contents:"alpha 😀 beta\n"
            ~encoding:Language.Position.Utf16
            ~edits:
              [
                {
                  Language.start_offset = 0;
                  stop_offset = 5;
                  replacement = "ALPHA";
                };
              ]
            ~expected:"ALPHA 😀 beta\n"
        with
        | Ok _ -> ()
        | Error reason -> fail (Error.Invalid_command_arguments reason))
  in
  report_benchmark "LSP incremental sync construction" lsp_sync;
  let syntax_session, open_syntax =
    measure_seconds (fun () -> benchmark_session ~language:"ocaml" large)
  in
  report_benchmark "open + parse 1 MiB OCaml" open_syntax;
  let (syntax_session, _), first_render =
    measure_seconds (fun () -> Zenbu_app.Session.render syntax_session)
  in
  report_benchmark "first 100x30 highlighted frame" first_render;
  let _, steady_render =
    measure_seconds (fun () -> Zenbu_app.Session.render syntax_session)
  in
  report_benchmark "cached 100x30 highlighted frame" steady_render;
  let _, search =
    measure_seconds (fun () ->
        let session =
          Zenbu_app.Session.handle_host syntax_session
            Zenbu_app.Session.Start_search
          |> function
          | Zenbu_app.Session.Continue value -> value
          | Zenbu_app.Session.Exit _ -> assert false
        in
        Zenbu_app.Session.handle_input session
          (Input_event.text_input "benchmark_value" |> Result.get_ok))
  in
  report_benchmark "literal search over 1 MiB" search;
  let _, edit =
    measure_seconds (fun () ->
        let session = benchmark_session small in
        let session =
          Zenbu_app.Session.handle_input session (logical_key "i")
        in
        Zenbu_app.Session.handle_input session
          (Input_event.text_input "!" |> Result.get_ok))
  in
  report_benchmark "Vim committed text edit" edit;
  let lua_config = source_path "examples/m7-init.lua" in
  let lua_session =
    benchmark_session ~config:(Scripting.Explicit lua_config) "alpha"
  in
  let _, lua =
    measure_seconds (fun () ->
        Zenbu_app.Session.handle_input lua_session (control_key "K"))
  in
  report_benchmark "Lua callback + transaction" lua;
  let _, component = benchmark_component_callback () in
  report_benchmark "Component callback + transaction" component

let usage () =
  prerr_endline
    "usage: zenbu-headless version | demo | replay <fixture.replay> | session \
     <fixture.session> | syntax <file> | language-status <file> | \
     language-fake-session <fake-lsp-server> <file> | lsp-position \
     <utf-8|utf-16|utf-32> <byte-offset> <file> | commands | api | describe \
     <command|model|selector|transformation> <id> | bindings \
     <vim|selection|structural> | why <fixture.session> | bindings-session \
     <fixture.session> | history <fixture.session> | selection \
     <fixture.session> | syntax-session <fixture.session> | search-session \
     <fixture.session> | profile <fixture.session> | config-check <init.lua> | \
     config-describe <init.lua> | script-session <init.lua> <fixture.session> \
     | plugin-session <PLUGIN-ROOT> <fixture.session> | plugins [DIR] | \
     plugin-check <PLUGIN-DIR> | plugin-describe <PLUGIN-DIR> | extension-api \
     | extension-sdk | extension-wit | benchmark";
  exit 2

let () =
  match Array.to_list Sys.argv with
  | [ _; "version" ] -> print_endline ("zenbu " ^ Version.current)
  | [ _; "demo" ] -> demo ()
  | [ _; "session"; path ] -> run_session path
  | [ _; "syntax"; path ] -> inspect_syntax path
  | [ _; "language-status"; path ] -> language_status path
  | [ _; "language-fake-session"; executable; path ] ->
      language_fake_session executable path
  | [ _; "lsp-position"; encoding; offset; path ] -> (
      match int_of_string_opt offset with
      | Some offset -> lsp_position encoding offset path
      | None ->
          fail
            (Error.Invalid_command_arguments "byte-offset must be an integer"))
  | [ _; "commands" ] -> inspect_commands ()
  | [ _; "api" ] -> inspect_api ()
  | [ _; "config-check"; path ] -> check_config path
  | [ _; "config-describe"; path ] -> describe_config path
  | [ _; "script-session"; config; session ] -> script_session config session
  | [ _; "plugins" ] -> plugins Plugins.Default
  | [ _; "plugins"; directory ] -> plugins (Plugins.Directories [ directory ])
  | [ _; "plugin-check"; path ] | [ _; "plugin-describe"; path ] ->
      plugin_check path
  | [ _; "plugin-session"; plugins; session ] -> plugin_session plugins session
  | [ _; "extension-api" ] -> extension_api ()
  | [ _; "extension-sdk" ] -> extension_sdk ()
  | [ _; "extension-wit" ] -> extension_wit ()
  | [ _; "benchmark" ] -> benchmark ()
  | [ _; "describe"; kind; id ] -> inspect_description kind id
  | [ _; "bindings"; "vim" ] -> initial_bindings Vim
  | [ _; "bindings"; "selection" ] -> initial_bindings Selection_first
  | [ _; "bindings"; "structural" ] -> initial_bindings Structural
  | [ _; "why"; path ] -> observed_session Zenbu_app.Session.Why path
  | [ _; "bindings-session"; path ] ->
      observed_session Zenbu_app.Session.Bindings path
  | [ _; "history"; path ] -> observed_session Zenbu_app.Session.History path
  | [ _; "selection"; path ] ->
      observed_session Zenbu_app.Session.Selection_view path
  | [ _; "syntax-session"; path ] ->
      observed_session Zenbu_app.Session.Syntax path
  | [ _; "search-session"; path ] ->
      observed_session Zenbu_app.Session.Search path
  | [ _; "profile"; path ] -> observed_session Zenbu_app.Session.Profile path
  | [ _; "replay"; path ] -> (
      match Replay.of_string (read_file path) with
      | Ok replay -> (
          match Replay.run replay with
          | Ok history -> print_result history
          | Error error -> fail error)
      | Error error -> fail error)
  | _ -> usage ()
