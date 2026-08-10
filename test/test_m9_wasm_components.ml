open Zenbu_kernel
open Zenbu_model_api
module Extension = Zenbu_extension
module Plugins = Extension.Plugin_host

exception Test_failure of string

let failf format =
  Printf.ksprintf (fun message -> raise (Test_failure message)) format

let expect condition format =
  Printf.ksprintf
    (fun message -> if not condition then raise (Test_failure message))
    format

let must = function
  | Ok value -> value
  | Error error -> failf "%s" (Error.to_string error)

let contains text fragment =
  let text_length = String.length text in
  let fragment_length = String.length fragment in
  let rec loop offset =
    if offset + fragment_length > text_length then false
    else if String.sub text offset fragment_length = fragment then true
    else loop (offset + 1)
  in
  fragment_length = 0 || loop 0

let read path =
  let channel = open_in_bin path in
  Fun.protect
    ~finally:(fun () -> close_in_noerr channel)
    (fun () -> really_input_string channel (in_channel_length channel))

let write path text =
  let channel = open_out_bin path in
  Fun.protect
    ~finally:(fun () -> close_out_noerr channel)
    (fun () -> output_string channel text)

let fixture path =
  let candidates =
    [
      path;
      Filename.concat ".." path;
      Filename.concat "../.." path;
      Filename.concat "../../.." path;
    ]
    @ Option.to_list
        (Option.map
           (fun root -> Filename.concat root path)
           (Sys.getenv_opt "DUNE_SOURCEROOT"))
  in
  match List.find_opt Sys.file_exists candidates with
  | Some path -> path
  | None -> failf "missing M9 fixture %s" path

let base64_digit = function
  | 'A' .. 'Z' as value -> Char.code value - Char.code 'A'
  | 'a' .. 'z' as value -> Char.code value - Char.code 'a' + 26
  | '0' .. '9' as value -> Char.code value - Char.code '0' + 52
  | '+' -> 62
  | '/' -> 63
  | value -> failf "invalid base64 character %C" value

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
    | _ -> failf "base64 fixture has an incomplete final group"
  in
  groups input

let rec remove path =
  if Sys.file_exists path then
    if Sys.is_directory path then (
      Sys.readdir path
      |> Array.iter (fun entry -> remove (Filename.concat path entry));
      Unix.rmdir path)
    else Sys.remove path

let with_root run =
  let root = Filename.temp_file "zenbu-m9" "" in
  Sys.remove root;
  Unix.mkdir root 0o700;
  Fun.protect ~finally:(fun () -> remove root) (fun () -> run root)

let component_fixture =
  lazy (fixture "test/fixtures/m9_component.wasm.b64" |> read |> decode_base64)

let wasi_fixture =
  lazy
    (fixture "test/fixtures/m9_wasi_component.wasm.b64" |> read |> decode_base64)

let conformance_fixture =
  lazy
    (fixture "test/fixtures/m9_conformance_component.wasm.b64"
    |> read |> decode_base64)

let unauthorized_import_fixture =
  lazy
    (fixture "test/fixtures/m9_unauthorized_import_component.wasm.b64"
    |> read |> decode_base64)

let toml_array values =
  values |> List.map (Printf.sprintf "%S") |> String.concat ", "

let manifest ?(entrypoint = "plugin.wasm") ~id ~version ~contributions
    ~capabilities () =
  Printf.sprintf
    {|
manifest_version = 1

[plugin]
id = %S
name = "M9 Component test plugin"
version = %S
api = 1
runtime = "wasm-component"
entrypoint = %S
contributions = [%s]
capabilities = [%s]
|}
    id version entrypoint (toml_array contributions) (toml_array capabilities)

let create_package root ?(directory = "fixture") ?(entrypoint = "plugin.wasm")
    ?(binary = component_fixture) ~id ~version ~contributions ~capabilities () =
  let package = Filename.concat root directory in
  Unix.mkdir package 0o700;
  write (Filename.concat package entrypoint) (Lazy.force binary);
  write
    (Filename.concat package "zenbu-plugin.toml")
    (manifest ~entrypoint ~id ~version ~contributions ~capabilities ());
  package

let ctrl text =
  Input_event.logical_text (String.lowercase_ascii text)
  |> must
  |> Input_event.key_press ~modifiers:[ Input_event.Control ]

let key text = Input_event.logical_text text |> must |> Input_event.key_press
let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 }

let session root ?(trace = Trace.disabled ()) ?(profiler = Profiler.disabled ())
    () =
  Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha" ~trace
    ~profiler ~config:Zenbu_scripting.Scripting.Disabled
    ~plugins:(Plugins.Directories [ root ]) ~dimensions ()
  |> must

let scripts value =
  Zenbu_app.Session.inspect value Zenbu_app.Session.Scripts
  |> String.concat "\n"

let base_commands () =
  Command_registry.register Command_registry.empty
    Zenbu_proof_models.Semantic_commands.apply_command
  |> must

let error_code = function
  | Error.Extension_error { code; _ } -> Some code
  | _ -> None

let conformance_id = "com.example.conformance"

let lua_manifest ~id ~version ~contributions ~capabilities =
  Printf.sprintf
    {|
manifest_version = 1

[plugin]
id = %S
name = "Lua conformance fixture"
version = %S
api = 1
runtime = "lua-trusted"
entrypoint = "init.lua"
contributions = [%s]
capabilities = [%s]
|}
    id version (toml_array contributions) (toml_array capabilities)

let conformance_lua_source =
  {|
zenbu.selector {
  id = "com.example.conformance.document",
  title = "Document",
  description = "Select the document.",
  run = function(call)
    return {
      selections = {{ anchor = 0, head = call.context.document.length }},
      primary = 1,
    }
  end,
}

zenbu.transform {
  id = "com.example.conformance.done",
  title = "Done",
  description = "Replace the selection.",
  run = function(call)
    local selection = call.arguments.selection_set[1]
    return {
      edits = {{
        start = math.min(selection.anchor, selection.head),
        stop = math.max(selection.anchor, selection.head),
        text = "done",
      }},
    }
  end,
}

zenbu.command {
  id = "com.example.conformance.insert",
  title = "Insert",
  description = "Insert a marker.",
  run = function(_) return {{ kind = "insert", text = "!" }} end,
}

zenbu.command {
  id = "com.example.conformance.apply",
  title = "Apply",
  description = "Compose selector and transformation.",
  run = function(_)
    return {{
      kind = "apply",
      selector = "com.example.conformance.document",
      transformation = "com.example.conformance.done",
    }}
  end,
}

zenbu.bind {
  input = "Ctrl-K",
  command = "com.example.conformance.insert",
  scope = "global",
}
zenbu.bind {
  input = "Ctrl-A",
  command = "com.example.conformance.apply",
  scope = "global",
}
zenbu.on {
  event = "document-changed",
  run = function(_) return {{ kind = "message", text = "event delivered" }} end,
}
|}

let create_lua_package root ~id ~version ~contributions ~capabilities =
  let package = Filename.concat root "fixture" in
  Unix.mkdir package 0o700;
  write
    (Filename.concat package "zenbu-plugin.toml")
    (lua_manifest ~id ~version ~contributions ~capabilities);
  write (Filename.concat package "init.lua") conformance_lua_source;
  package

let create_lua_command_package root ~directory ~id ~input ~text =
  let package = Filename.concat root directory in
  Unix.mkdir package 0o700;
  write
    (Filename.concat package "zenbu-plugin.toml")
    (lua_manifest ~id ~version:"1.0.0" ~contributions:[ "commands"; "bindings" ]
       ~capabilities:[ "document.edit" ]);
  write
    (Filename.concat package "init.lua")
    (Printf.sprintf
       {|
zenbu.command {
  id = %S,
  title = "Insert",
  description = "Insert a test marker.",
  run = function(_) return {{ kind = "insert", text = %S }} end,
}
zenbu.bind { input = %S, command = %S, scope = "global" }
|}
       (id ^ ".insert") text input (id ^ ".insert"));
  package

let all_contributions =
  [ "commands"; "selectors"; "transformations"; "bindings"; "events" ]

let all_capabilities =
  [
    "document.read";
    "document.edit";
    "selection.read";
    "selection.write";
    "ui.message";
    "event.subscribe";
  ]

let assert_runtime_neutral_conformance ~runtime root =
  let trace = Trace.enabled ~capacity:512 |> must in
  let profiler = Profiler.enabled ~capacity:512 |> must in
  let value = session root ~trace ~profiler () in
  let value = Zenbu_app.Session.handle_input value (ctrl "K") in
  expect
    (Zenbu_app.Session.contents value = "!alpha")
    "%s command binding did not use the semantic transaction path" runtime;
  let value = Zenbu_app.Session.handle_input value (ctrl "A") in
  expect
    (Zenbu_app.Session.contents value = "done")
    "%s selector/transformation composition diverged" runtime;
  let history =
    Zenbu_app.Session.inspect value Zenbu_app.Session.History
    |> String.concat "\n"
  in
  expect
    (contains history (conformance_id ^ "@1.0.0 (" ^ runtime ^ ")"))
    "%s provenance/history attribution is missing: %s" runtime history;
  let plugins =
    Zenbu_app.Session.inspect value Zenbu_app.Session.Plugins
    |> String.concat "\n"
  in
  expect
    (contains plugins (conformance_id ^ " 1.0.0 active " ^ runtime))
    "%s plugin inspection is incomplete: %s" runtime plugins;
  expect
    (List.exists
       (function
         | Trace_event.Extension_callback
             { provider; kind = "event"; outcome = "succeeded"; _ } ->
             Provider.runtime provider = Some runtime
         | _ -> false)
       (Trace.events trace))
    "%s event callback did not use the shared extension trace path" runtime;
  expect
    (List.exists
       (fun aggregate ->
         Profiler.aggregate_stage aggregate = Profiler.Extension_command
         || Profiler.aggregate_stage aggregate = Profiler.Extension_selector
         || Profiler.aggregate_stage aggregate
            = Profiler.Extension_transformation
         || Profiler.aggregate_stage aggregate = Profiler.Extension_event)
       (Profiler.aggregates profiler))
    "%s did not produce generic extension profile samples" runtime;
  let undone = Zenbu_app.Session.handle_input value (key "u") in
  expect
    (Zenbu_app.Session.contents undone = "!alpha")
    "%s command did not remain undoable through ordinary history" runtime;
  let redone = Zenbu_app.Session.handle_input undone (ctrl "R") in
  expect
    (Zenbu_app.Session.contents redone = "done")
    "%s command did not remain redoable through ordinary history" runtime

let test_same_runtime_neutral_conformance_suite_runs_for_lua_and_component () =
  with_root (fun lua_root ->
      ignore
        (create_lua_package lua_root ~id:conformance_id ~version:"1.0.0"
           ~contributions:all_contributions ~capabilities:all_capabilities);
      assert_runtime_neutral_conformance ~runtime:"lua-trusted" lua_root);
  with_root (fun component_root ->
      ignore
        (create_package component_root ~binary:conformance_fixture
           ~id:conformance_id ~version:"1.0.0" ~contributions:all_contributions
           ~capabilities:all_capabilities ());
      assert_runtime_neutral_conformance ~runtime:"wasm-component"
        component_root)

module Vim_runtime = Model_runtime.Make (Zenbu_proof_models.Vim_model)

let runtime_for_plugins plugins =
  let commands =
    List.fold_left
      (fun registry command ->
        Command_registry.register registry command |> must)
      (base_commands ()) (Plugins.commands plugins)
  in
  let document =
    Document.create
      ~id:(Document_id.of_string "component-runtime" |> must)
      ~contents:"alpha" ()
    |> must
  in
  Vim_runtime.create ~commands
    ~semantic_behaviors:(Plugins.semantic_behaviors plugins)
    ~document ()
  |> must

let invoke_component_command runtime id =
  let invocation =
    Command_invocation.create
      ~id:(Command_id.of_string id |> must)
      ~arguments:[]
    |> must
  in
  Vim_runtime.invoke_command runtime ~input:(ctrl "K") invocation

let runtime_contents runtime =
  Vim_runtime.context runtime |> Editor_context.contents

let selection_state_of_set selections =
  Replay.
    {
      selections =
        Selection_set.to_list selections
        |> List.map (fun selection ->
            Selection_spec.make
              ~anchor_offset:(Selection.anchor selection |> Anchor.byte_offset)
              ~head_offset:(Selection.head selection |> Anchor.byte_offset)
            |> must);
      primary = Selection_set.primary_index selections;
    }

let selection_state document =
  let snapshot = Document.snapshot document in
  Document_snapshot.selections snapshot |> selection_state_of_set

let replay_from_single_change change =
  let before = History.before change in
  let transaction = History.transaction change in
  let metadata = Transaction.metadata_of transaction in
  let edits =
    Transaction.edits transaction
    |> List.map (fun edit ->
        let range = Edit.range edit in
        Replay.
          {
            start_offset = Range.start range |> Anchor.byte_offset;
            stop_offset = Range.stop range |> Anchor.byte_offset;
            text = Edit.text edit;
          })
  in
  let selection_change =
    Transaction.selection_change transaction
    |> Option.map selection_state_of_set
  in
  Replay.create
    ~document_id:(Document.id before |> Document_id.to_string)
    ~contents:(Document.snapshot before |> Document_snapshot.contents)
    ~initial_selections:(selection_state before)
    ~actions:
      [
        Replay.Transaction
          {
            source = Transaction.source metadata;
            intent = Transaction.intent metadata;
            description = Transaction.description metadata;
            edits;
            selection_change;
          };
      ]
  |> must

let test_component_output_limits_atomicity_and_replay_without_runtime () =
  with_root (fun root ->
      let package =
        create_package root ~binary:conformance_fixture ~id:conformance_id
          ~version:"1.0.0" ~contributions:all_contributions
          ~capabilities:all_capabilities ()
      in
      write
        (Filename.concat package "zenbu-plugin.toml")
        (manifest ~id:conformance_id ~version:"1.0.0"
           ~contributions:all_contributions ~capabilities:all_capabilities ()
        ^ "\n[wasm]\nfuel = 100000000\nmemory_bytes = 16777216\n");
      let plugins =
        Plugins.load ~config:(Plugins.Directories [ root ])
          ~base_commands:(base_commands ()) ~base_semantics:[] ()
      in
      let initial = runtime_for_plugins plugins in
      let assert_unchanged result description =
        match result with
        | Ok _ -> failf "%s unexpectedly succeeded" description
        | Error _ ->
            expect
              (runtime_contents initial = "alpha")
              "%s changed the document before rejection" description;
            expect
              (History.lineage (Vim_runtime.history initial) = [])
              "%s changed history before rejection" description
      in
      assert_unchanged
        (invoke_component_command initial "com.example.conformance.invalid")
        "a response containing valid then invalid actions";
      assert_unchanged
        (invoke_component_command initial
           "com.example.conformance.bad-selection")
        "an invalid Component selection";
      let overlarge =
        invoke_component_command initial "com.example.conformance.large"
      in
      (match overlarge with
      | Error error ->
          expect
            (error_code error = Some Error.Extension_response_limit)
            "oversized Component response returned %s" (Error.to_string error)
      | Ok _ -> failf "oversized Component response unexpectedly succeeded");
      expect
        (runtime_contents initial = "alpha")
        "oversized Component response changed the document";
      let committed, _ =
        invoke_component_command initial "com.example.conformance.insert"
        |> must
      in
      expect
        (runtime_contents committed = "!alpha")
        "Component remained unusable after rejected output";
      let history = Vim_runtime.history committed in
      let replay =
        match History.lineage history with
        | [ change ] -> replay_from_single_change change
        | _ -> failf "expected exactly one committed Component transaction"
      in
      Plugins.dispose plugins;
      let replayed = Replay.run replay |> must in
      expect
        (History.current replayed |> Document.snapshot
       |> Document_snapshot.contents = "!alpha")
        "semantic replay required the unloaded Component runtime")

let test_component_generation_stress () =
  with_root (fun root ->
      ignore
        (create_package root ~binary:conformance_fixture ~id:conformance_id
           ~version:"1.0.0" ~contributions:all_contributions
           ~capabilities:all_capabilities ());
      let active =
        ref
          (Plugins.load ~config:(Plugins.Directories [ root ])
             ~base_commands:(base_commands ()) ~base_semantics:[] ())
      in
      for _ = 1 to 200 do
        let runtime = runtime_for_plugins !active in
        let invoked, _ =
          invoke_component_command runtime "com.example.conformance.insert"
          |> must
        in
        expect
          (runtime_contents invoked = "!alpha")
          "a staged Component generation did not remain callable";
        let next =
          Plugins.reload !active ~base_commands:(base_commands ())
            ~base_semantics:[] ()
        in
        expect
          (List.length (Plugins.providers next) = 1)
          "Component reload left stale or missing active providers";
        active := next;
        Gc.full_major ()
      done;
      Plugins.dispose !active)

let test_mixed_runtime_snapshot_and_cross_runtime_collision () =
  with_root (fun root ->
      ignore
        (create_package root ~directory:"component" ~id:"com.example.m9"
           ~version:"1.0.0"
           ~contributions:
             [ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:
             [ "document.edit"; "selection.read"; "selection.write" ]
           ());
      ignore
        (create_lua_command_package root ~directory:"lua" ~id:"com.example.lua"
           ~input:"Ctrl-J" ~text:"L");
      let trace = Trace.enabled ~capacity:256 |> must in
      let value = session root ~trace () in
      let value = Zenbu_app.Session.handle_input value (ctrl "K") in
      let value = Zenbu_app.Session.handle_input value (ctrl "J") in
      expect
        (Zenbu_app.Session.contents value = "!Lalpha")
        "Lua and Component commands did not coexist in one snapshot: %S"
        (Zenbu_app.Session.contents value);
      let plugins =
        Zenbu_app.Session.inspect value Zenbu_app.Session.Plugins
        |> String.concat "\n"
      in
      expect
        (contains plugins "com.example.m9 1.0.0 active wasm-component"
        && contains plugins "com.example.lua 1.0.0 active lua-trusted")
        "mixed-runtime plugin inspection lost an active provider: %s" plugins;
      let history =
        Zenbu_app.Session.inspect value Zenbu_app.Session.History
        |> String.concat "\n"
      in
      expect
        (contains history "com.example.m9@1.0.0 (wasm-component)"
        && contains history "com.example.lua@1.0.0 (lua-trusted)")
        "mixed-runtime history attribution is incomplete: %s" history;
      expect
        (List.exists
           (function
             | Trace_event.Extension_callback { provider; _ } ->
                 Provider.runtime provider = Some "wasm-component"
             | _ -> false)
           (Trace.events trace))
        "mixed runtime session omitted Component callback tracing");
  with_root (fun root ->
      ignore
        (create_package root ~directory:"component" ~binary:conformance_fixture
           ~id:conformance_id ~version:"1.0.0" ~contributions:all_contributions
           ~capabilities:all_capabilities ());
      let package = Filename.concat root "lua" in
      Unix.mkdir package 0o700;
      write
        (Filename.concat package "zenbu-plugin.toml")
        (lua_manifest ~id:"com.example" ~version:"1.0.0"
           ~contributions:[ "commands" ] ~capabilities:[]);
      write
        (Filename.concat package "init.lua")
        {|
zenbu.command {
  id = "com.example.conformance.insert",
  title = "Colliding command",
  description = "Intentional cross-runtime collision.",
  run = function(_) return nil end,
}
|};
      let plugins =
        Plugins.load ~config:(Plugins.Directories [ root ])
          ~base_commands:(base_commands ()) ~base_semantics:[] ()
      in
      expect
        (Plugins.providers plugins = [])
        "cross-runtime command collision partially activated a provider";
      expect
        (List.for_all
           (fun view -> Plugins.view_state view = Plugins.Failed)
           (Plugins.views plugins))
        "cross-runtime command collision did not use the M8 failure policy";
      Plugins.dispose plugins)

let test_component_semantic_conformance_and_provenance () =
  with_root (fun root ->
      ignore
        (create_package root ~id:"com.example.m9" ~version:"1.0.0"
           ~contributions:
             [ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:
             [ "document.edit"; "selection.read"; "selection.write" ]
           ());
      let trace = Trace.enabled ~capacity:256 |> must in
      let profiler = Profiler.enabled ~capacity:256 |> must in
      let inserted =
        session root ~trace ~profiler () |> fun value ->
        Zenbu_app.Session.handle_input value (ctrl "K")
      in
      expect
        (Zenbu_app.Session.contents inserted = "!alpha")
        "Component command did not commit through the shared semantic path";
      let applied =
        session root () |> fun value ->
        Zenbu_app.Session.handle_input value (ctrl "A")
      in
      expect
        (Zenbu_app.Session.contents applied = "done")
        "Component selector/transformation did not produce a normal \
         transaction: %s"
        (scripts applied);
      let history =
        Zenbu_app.Session.inspect inserted Zenbu_app.Session.History
        |> String.concat "\n"
      in
      expect
        (contains history "com.example.m9@1.0.0 (wasm-component)")
        "Component provenance omitted plugin version/runtime: %s" history;
      let why =
        Zenbu_app.Session.inspect inserted Zenbu_app.Session.Why
        |> String.concat "\n"
      in
      expect
        (contains why "com.example.m9@1.0.0 (wasm-component)")
        "Component why inspection omitted plugin/version/runtime: %s" why;
      expect
        (List.exists
           (function
             | Trace_event.Extension_callback { provider; _ } ->
                 Provider.runtime provider = Some "wasm-component"
             | _ -> false)
           (Trace.events trace))
        "Component callback was absent from normal extension tracing";
      let runtime_stage stage =
        List.exists
          (function
            | Trace_event.Extension_runtime { stage = actual; _ } ->
                String.equal actual stage
            | _ -> false)
          (Trace.events trace)
      in
      expect
        (List.for_all runtime_stage
           [ "compile"; "instantiate"; "register"; "call" ])
        "Component runtime telemetry omitted a required lifecycle stage";
      expect
        (List.exists
           (function
             | Trace_event.Extension_runtime
                 {
                   stage = "call";
                   runtime = "wasm-component";
                   fuel_consumed = Some fuel;
                   _;
                 } ->
                 fuel > 0
             | _ -> false)
           (Trace.events trace))
        "Component call telemetry did not report fuel consumption";
      expect
        (List.exists
           (fun aggregate ->
             Profiler.aggregate_stage aggregate
             = Profiler.Extension_wasm_compile
             || Profiler.aggregate_stage aggregate
                = Profiler.Extension_wasm_instantiate
             || Profiler.aggregate_stage aggregate
                = Profiler.Extension_wasm_register
             || Profiler.aggregate_stage aggregate
                = Profiler.Extension_wasm_call)
           (Profiler.aggregates profiler))
        "Component runtime telemetry did not create profiler samples")

let test_callback_failures_are_nonmutating_and_classified () =
  with_root (fun root ->
      ignore
        (create_package root ~id:"com.example.m9" ~version:"1.0.0"
           ~contributions:
             [ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:
             [ "document.edit"; "selection.read"; "selection.write" ]
           ());
      let assert_failure input code fragment =
        let value =
          session root () |> fun value ->
          Zenbu_app.Session.handle_input value (ctrl input)
        in
        expect
          (Zenbu_app.Session.contents value = "alpha")
          "failed Component callback %s changed the document" input;
        let message = scripts value in
        expect (contains message code)
          "Component callback %s did not report %s: %s" input code message;
        expect
          (contains message fragment)
          "Component callback %s omitted failure detail %S: %s" input fragment
          message
      in
      assert_failure "B" "extension-runtime-error" "WIT value has no root node";
      assert_failure "I" "extension-runtime-error" "unknown action kind";
      assert_failure "T" "extension-trap" "wasm trap";
      assert_failure "L" "extension-fuel-exhausted" "all fuel consumed";
      assert_failure "M" "extension-memory-exhausted" "memory allocation denied";
      let after_loop =
        session root () |> fun value ->
        Zenbu_app.Session.handle_input value (ctrl "L")
      in
      let health =
        Zenbu_app.Session.inspect after_loop Zenbu_app.Session.Plugins
        |> String.concat "\n"
      in
      expect
        (contains health "health: unavailable")
        "fuel exhaustion did not mark the Component runtime unavailable: %s"
        health;
      expect
        (contains health "extension-fuel-exhausted")
        "unavailable Component health did not retain the fatal cause: %s" health;
      let unavailable = Zenbu_app.Session.handle_input after_loop (ctrl "K") in
      expect
        (Zenbu_app.Session.contents unavailable = "alpha")
        "unavailable Component invocation changed the document";
      expect
        (contains (scripts unavailable) "extension-runtime-unavailable")
        "unavailable Component invocation did not report a structured error";
      let recovered =
        Zenbu_app.Session.handle_input after_loop (key "d") |> fun value ->
        Zenbu_app.Session.handle_input value (key "w")
      in
      expect
        (Zenbu_app.Session.contents recovered = "")
        "normal model input did not remain usable after a fuel-exhausted \
         Component";
      let plugins =
        Zenbu_app.Session.inspect recovered Zenbu_app.Session.Plugins
        |> String.concat "\n"
      in
      expect
        (contains plugins "com.example.m9 1.0.0 active wasm-component")
        "fuel exhaustion unexpectedly unloaded the Component generation";
      let reloaded = Zenbu_app.Session.reload_config after_loop in
      let reloaded_plugins =
        Zenbu_app.Session.inspect reloaded Zenbu_app.Session.Plugins
        |> String.concat "\n"
      in
      expect
        (contains reloaded_plugins "health: healthy")
        "Component reload did not restore runtime health: %s" reloaded_plugins;
      let restored = Zenbu_app.Session.handle_input reloaded (ctrl "K") in
      expect
        (Zenbu_app.Session.contents restored = "!alpha")
        "Component reload did not restore callback availability")

let test_capability_denial_remains_at_the_host_boundary () =
  with_root (fun root ->
      ignore
        (create_package root ~id:"com.example.m9" ~version:"1.0.0"
           ~contributions:
             [ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:[] ());
      let trace = Trace.enabled ~capacity:128 |> must in
      let value =
        session root ~trace () |> fun value ->
        Zenbu_app.Session.handle_input value (ctrl "K")
      in
      expect
        (Zenbu_app.Session.contents value = "alpha")
        "capability-denied Component action mutated the document";
      expect
        (contains (scripts value) "extension capability-denied")
        "capability denial was not surfaced by the session";
      expect
        (List.exists
           (function
             | Trace_event.Capability_denied { required = "document.edit"; _ }
               ->
                 true
             | _ -> false)
           (Trace.events trace))
        "capability denial did not use the ordinary trace path")

let test_invalid_wasi_component_and_malformed_binary_are_rejected () =
  let assert_failed ?binary ~entrypoint expected =
    with_root (fun root ->
        ignore
          (create_package root ?binary ~entrypoint ~id:"com.example.m9"
             ~version:"1.0.0" ~contributions:[ "commands" ] ~capabilities:[] ());
        let plugins =
          Plugins.load ~config:(Plugins.Directories [ root ])
            ~base_commands:(base_commands ()) ~base_semantics:[] ()
        in
        let view =
          match Plugins.views plugins with
          | [ view ] -> view
          | _ -> failf "expected exactly one failed Component package"
        in
        expect
          (Plugins.view_state view = Plugins.Failed)
          "invalid Component package became active";
        expect
          (Plugins.providers plugins = [])
          "invalid Component package left a provider active";
        let error = Plugins.view_error view |> Option.get in
        expect
          (error_code error = Some expected)
          "invalid Component package returned %s instead of expected runtime \
           code"
          (Error.to_string error);
        Plugins.dispose plugins)
  in
  assert_failed ~binary:wasi_fixture ~entrypoint:"wasi.wasm"
    Error.Extension_abi_mismatch;
  assert_failed
    ~binary:(lazy "not a WebAssembly component")
    ~entrypoint:"bad.wasm" Error.Extension_runtime_error

let test_unauthorized_component_import_is_rejected_during_staging () =
  with_root (fun root ->
      ignore
        (create_package root ~binary:unauthorized_import_fixture
           ~id:"com.example.unauthorized" ~version:"1.0.0" ~contributions:[]
           ~capabilities:[ "ui.message" ] ());
      let plugins =
        Plugins.load ~config:(Plugins.Directories [ root ])
          ~base_commands:(base_commands ()) ~base_semantics:[] ()
      in
      let view =
        match Plugins.views plugins with
        | [ view ] -> view
        | _ -> failf "expected exactly one unauthorized-import package view"
      in
      expect
        (Plugins.view_state view = Plugins.Failed)
        "Component importing document-read activated with only ui.message";
      expect
        (Plugins.providers plugins = [])
        "unauthorized Component import left a provider active";
      let error = Plugins.view_error view |> Option.get in
      expect
        (error_code error = Some Error.Extension_abi_mismatch)
        "unauthorized Component import returned %s" (Error.to_string error);
      expect
        (contains (Error.to_string error) "document-read")
        "unauthorized Component import error did not name document-read: %s"
        (Error.to_string error);
      Plugins.dispose plugins)

let test_failed_wasm_reload_retains_the_generation () =
  with_root (fun root ->
      let package =
        create_package root ~id:"com.example.m9" ~version:"1.0.0"
          ~contributions:
            [ "commands"; "selectors"; "transformations"; "bindings" ]
          ~capabilities:[ "document.edit"; "selection.read"; "selection.write" ]
          ()
      in
      let initial =
        Plugins.load ~config:(Plugins.Directories [ root ])
          ~base_commands:(base_commands ()) ~base_semantics:[] ()
      in
      write
        (Filename.concat package "zenbu-plugin.toml")
        (manifest ~id:"com.example.m9" ~version:"1.1.0"
           ~contributions:
             [ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:
             [ "document.edit"; "selection.read"; "selection.write" ]
           ());
      let updated =
        Plugins.reload initial ~base_commands:(base_commands ())
          ~base_semantics:[] ()
      in
      let updated_view =
        match Plugins.views updated with
        | [ view ] -> view
        | _ -> failf "successful Component reload lost the active generation"
      in
      expect
        (Plugins.view_version updated_view
        |> Option.map Extension.Plugin_version.to_string
        = Some "1.1.0")
        "successful Component reload did not atomically replace its generation";
      write (Filename.concat package "bad.wasm") "invalid component";
      write
        (Filename.concat package "zenbu-plugin.toml")
        (manifest ~entrypoint:"bad.wasm" ~id:"com.example.m9" ~version:"1.1.0"
           ~contributions:
             [ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:
             [ "document.edit"; "selection.read"; "selection.write" ]
           ());
      let retained =
        Plugins.reload updated ~base_commands:(base_commands ())
          ~base_semantics:[] ()
      in
      let view =
        match Plugins.views retained with
        | [ view ] -> view
        | _ -> failf "failed reload lost the previous Wasm generation"
      in
      expect
        (Plugins.view_state view = Plugins.Active)
        "failed Wasm reload did not retain the last-known-good generation";
      expect
        (Plugins.view_version view
        |> Option.map Extension.Plugin_version.to_string
        = Some "1.1.0")
        "failed Wasm reload replaced the active manifest";
      expect
        (Option.is_some (Plugins.view_error view))
        "failed Wasm reload did not retain the actionable staging error";
      Plugins.dispose retained)

let test_manifest_limits_are_applied_and_inspectable () =
  with_root (fun root ->
      let package =
        create_package root ~id:"com.example.m9" ~version:"1.0.0"
          ~contributions:
            [ "commands"; "selectors"; "transformations"; "bindings" ]
          ~capabilities:[ "document.edit"; "selection.read"; "selection.write" ]
          ()
      in
      write
        (Filename.concat package "zenbu-plugin.toml")
        (manifest ~id:"com.example.m9" ~version:"1.0.0"
           ~contributions:
             [ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:
             [ "document.edit"; "selection.read"; "selection.write" ]
           ()
        ^ "\n[wasm]\nfuel = 6000000\nmemory_bytes = 17825792\n");
      let plugins =
        Plugins.load ~config:(Plugins.Directories [ root ])
          ~base_commands:(base_commands ()) ~base_semantics:[] ()
      in
      let view =
        match Plugins.views plugins with
        | [ view ] -> view
        | _ -> failf "configured Component package did not have one view"
      in
      expect
        (Plugins.view_runtime_limits view = Some (6_000_000, 17_825_792))
        "configured Component resource limits were not retained for inspection";
      Plugins.dispose plugins)

let tests =
  [
    ( "same runtime-neutral conformance suite runs for Lua and Components",
      test_same_runtime_neutral_conformance_suite_runs_for_lua_and_component );
    ( "Component output limits, atomic rejection, and replay survive unload",
      test_component_output_limits_atomicity_and_replay_without_runtime );
    ("Component generation stress", test_component_generation_stress);
    ( "mixed Lua/Component snapshots and cross-runtime collisions",
      test_mixed_runtime_snapshot_and_cross_runtime_collision );
    ( "Component semantic conformance and provenance",
      test_component_semantic_conformance_and_provenance );
    ( "Component failure isolation and resource classification",
      test_callback_failures_are_nonmutating_and_classified );
    ( "Component capability denial stays at the host boundary",
      test_capability_denial_remains_at_the_host_boundary );
    ( "WASI and malformed Components are rejected",
      test_invalid_wasi_component_and_malformed_binary_are_rejected );
    ( "unauthorized Component imports fail during staging",
      test_unauthorized_component_import_is_rejected_during_staging );
    ( "failed Component reload retains generation",
      test_failed_wasm_reload_retains_the_generation );
    ( "Component manifest limits are inspectable",
      test_manifest_limits_are_applied_and_inspectable );
  ]

let () =
  List.iter
    (fun (name, test) ->
      try
        test ();
        print_endline ("ok - " ^ name)
      with Test_failure message ->
        Printf.eprintf "FAILED - %s: %s\n%!" name message;
        exit 1)
    tests
