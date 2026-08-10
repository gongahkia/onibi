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

let artifact path =
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
  | None -> failf "missing generated extension artifact %s" path

let write path text =
  let channel = open_out_bin path in
  Fun.protect
    ~finally:(fun () -> close_out_noerr channel)
    (fun () -> output_string channel text)

let remove_tree root =
  let remove_package path =
    if Sys.file_exists path && Sys.is_directory path then (
      Sys.readdir path
      |> Array.iter (fun entry ->
          let child = Filename.concat path entry in
          if Sys.file_exists child && not (Sys.is_directory child) then
            Sys.remove child);
      Unix.rmdir path)
  in
  if Sys.file_exists root && Sys.is_directory root then (
    Sys.readdir root
    |> Array.iter (fun entry -> remove_package (Filename.concat root entry));
    Unix.rmdir root)

let with_root f =
  let root = Filename.temp_file "zenbu-m8" "" in
  Sys.remove root;
  Unix.mkdir root 0o700;
  Fun.protect ~finally:(fun () -> remove_tree root) (fun () -> f root)

let toml_array values =
  values
  |> List.map (fun value -> Printf.sprintf "%S" value)
  |> String.concat ", "

let manifest ~id ~version ~contributions ~capabilities =
  Printf.sprintf
    {|
manifest_version = 1

[plugin]
id = %S
name = "M8 test plugin"
version = %S
api = 1
runtime = "lua-trusted"
entrypoint = "init.lua"
contributions = [%s]
capabilities = [%s]
|}
    id version (toml_array contributions) (toml_array capabilities)

let create_plugin root directory ~id ~version ~contributions ~capabilities
    source =
  let package = Filename.concat root directory in
  Unix.mkdir package 0o700;
  write
    (Filename.concat package "zenbu-plugin.toml")
    (manifest ~id ~version ~contributions ~capabilities);
  write (Filename.concat package "init.lua") source;
  package

let replace_plugin package ~id ~version ~contributions ~capabilities source =
  write
    (Filename.concat package "zenbu-plugin.toml")
    (manifest ~id ~version ~contributions ~capabilities);
  write (Filename.concat package "init.lua") source

let ctrl text =
  Input_event.logical_text (String.lowercase_ascii text)
  |> must
  |> Input_event.key_press ~modifiers:[ Input_event.Control ]

let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 }

let base_commands () =
  Command_registry.register Command_registry.empty
    Zenbu_proof_models.Semantic_commands.apply_command
  |> must

let base_semantics () =
  Inspector.semantic_registry () |> Semantic_registry.descriptors

let plugin_source id text input =
  Printf.sprintf
    {|
zenbu.command {
  id = %S,
  title = "Insert",
  description = "Insert a plugin-owned marker.",
  run = function(_) return {{ kind = "insert", text = %S }} end,
}
zenbu.bind { input = %S, command = %S, scope = "global" }
zenbu.on {
  event = "document-changed",
  run = function(_) return {{ kind = "message", text = "plugin event" }} end,
}
|}
    (id ^ ".insert") text input (id ^ ".insert")

let command_binding_source id text input =
  Printf.sprintf
    {|
zenbu.command {
  id = %S,
  title = "Insert",
  description = "Insert a plugin-owned marker.",
  run = function(_) return {{ kind = "insert", text = %S }} end,
}
zenbu.bind { input = %S, command = %S, scope = "global" }
|}
    (id ^ ".insert") text input (id ^ ".insert")

let document_read_source id input =
  Printf.sprintf
    {|
zenbu.command {
  id = %S,
  title = "Read",
  description = "Read a document slice.",
  run = function(_) return {{ kind = "message", text = zenbu.text(0, 1) }} end,
}
zenbu.bind { input = %S, command = %S, scope = "global" }
|}
    (id ^ ".read") input (id ^ ".read")

let plugin_view host id =
  match
    Plugins.views host
    |> List.find_opt (fun view ->
        Plugins.view_id view
        |> Option.map Extension.Plugin_id.to_string
        |> Option.value ~default:"" |> String.equal id)
  with
  | Some view -> view
  | None -> failf "missing plugin view for %s" id

let error_code = function
  | Error.Extension_error { code; _ } -> Some code
  | _ -> None

let test_contract_and_manifest_validation () =
  expect (Extension.Contract.api_version = 1) "unexpected Extension API version";
  expect (Extension.Contract.manifest_version = 1) "unexpected manifest version";
  expect
    (Extension.Contract.runtime_ids = [ "lua-trusted"; "wasm-component" ])
    "runtime identifiers are not deterministic";
  expect
    (List.map Extension.Capability.id Extension.Capability.all
    = [
        "document.read";
        "document.edit";
        "selection.read";
        "selection.write";
        "syntax.read";
        "command.invoke";
        "ui.message";
        "event.subscribe";
      ])
    "capability contract changed unexpectedly";
  expect
    (List.map Extension.Contribution.id Extension.Contribution.all
    = [ "commands"; "selectors"; "transformations"; "bindings"; "events" ])
    "contribution contract changed unexpectedly";
  expect
    (Extension.Contract.stable_error_codes
    = [
        "invalid-manifest";
        "incompatible-api";
        "unknown-runtime";
        "unknown-capability";
        "unknown-contribution";
        "capability-denied";
        "contribution-not-declared";
        "namespace-violation";
        "invalid-plugin-package";
        "plugin-not-active";
        "extension-runtime-error";
        "extension-abi-mismatch";
        "extension-fuel-exhausted";
        "extension-memory-exhausted";
        "extension-trap";
        "duplicate-id";
        "unknown-semantic-id";
        "invalid-range";
        "invalid-selection";
        "invalid-action";
        "transaction-rejected";
      ])
    "stable extension errors changed unexpectedly";
  expect
    (read (artifact "docs/generated/EXTENSION_API.md")
    = Extension.Contract.markdown ())
    "generated Extension API reference is stale";
  expect
    (read (artifact "sdk/lua/zenbu.lua") = Extension.Contract.lua_stub ())
    "generated Lua SDK stub is stale";
  with_root (fun root ->
      let package =
        create_plugin root "bad-capability" ~id:"zenbu.m8badcap"
          ~version:"1.0.0" ~contributions:[ "commands" ]
          ~capabilities:[ "not.real" ] ""
      in
      match
        Extension.Manifest.parse (Filename.concat package "zenbu-plugin.toml")
      with
      | Error error ->
          expect
            (error_code error = Some Error.Unknown_capability)
            "unknown capability produced %s" (Error.to_string error)
      | Ok _ -> failf "unknown capability was accepted")

let test_activation_observability_and_capability_denial () =
  with_root (fun root ->
      let id = "zenbu.m8active" in
      ignore
        (create_plugin root "active" ~id ~version:"1.2.3"
           ~contributions:[ "commands"; "bindings"; "events" ]
           ~capabilities:[ "document.edit"; "ui.message"; "event.subscribe" ]
           (plugin_source id "!" "Ctrl-K"));
      let trace = Trace.enabled ~capacity:256 |> must in
      let profiler = Profiler.enabled ~capacity:256 |> must in
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~trace ~profiler ~config:Zenbu_scripting.Scripting.Disabled
          ~plugins:(Plugins.Directories [ root ]) ~dimensions ()
        |> must
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      expect
        (Zenbu_app.Session.contents session = "!alpha")
        "plugin command was not executed through the shared runtime: %S; \
         plugins: %s; why: %s"
        (Zenbu_app.Session.contents session)
        (Zenbu_app.Session.inspect session Zenbu_app.Session.Plugins
        |> String.concat " | ")
        (Zenbu_app.Session.inspect session Zenbu_app.Session.Why
        |> String.concat " | ");
      let plugins =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Plugins
        |> String.concat "\n"
      in
      expect
        (contains plugins "zenbu.m8active 1.2.3 active lua-trusted")
        "plugin inspection omits identity, version, state, or runtime: %s"
        plugins;
      let history =
        Zenbu_app.Session.inspect session Zenbu_app.Session.History
        |> String.concat "\n"
      in
      expect
        (contains history "zenbu.m8active@1.2.3 (lua-trusted)")
        "plugin version/runtime is absent from provenance: %s" history;
      let events = Trace.events trace in
      expect
        (List.exists
           (function
             | Trace_event.Extension_lifecycle
                 { phase = "load"; outcome = "succeeded"; _ } ->
                 true
             | Trace_event.Extension_callback { provider; _ }
               when Provider.kind provider = Provider.Plugin ->
                 true
             | _ -> false)
           events)
        "plugin lifecycle or callback trace is absent";
      expect
        (List.exists
           (fun aggregate ->
             Profiler.aggregate_stage aggregate = Profiler.Extension_load
             || Profiler.aggregate_stage aggregate = Profiler.Extension_command
             || Profiler.aggregate_stage aggregate = Profiler.Extension_event)
           (Profiler.aggregates profiler))
        "extension profiling stages are absent";
      let denied_id = "zenbu.m8denied" in
      ignore
        (create_plugin root "denied" ~id:denied_id ~version:"1.0.0"
           ~contributions:[ "commands"; "bindings" ] ~capabilities:[]
           (command_binding_source denied_id "x" "Ctrl-L"));
      let denied_read_id = "zenbu.m8deniedread" in
      ignore
        (create_plugin root "denied-read" ~id:denied_read_id ~version:"1.0.0"
           ~contributions:[ "commands"; "bindings" ]
           ~capabilities:[ "ui.message" ]
           (document_read_source denied_read_id "Ctrl-N"));
      let denied_trace = Trace.enabled ~capacity:256 |> must in
      let denied =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~trace:denied_trace ~config:Zenbu_scripting.Scripting.Disabled
          ~plugins:(Plugins.Directories [ root ]) ~dimensions ()
        |> must
      in
      let denied = Zenbu_app.Session.handle_input denied (ctrl "L") in
      expect
        (Zenbu_app.Session.contents denied = "alpha")
        "capability-denied edit mutated the document";
      expect
        (List.exists
           (function
             | Trace_event.Capability_denied
                 { provider; required = "document.edit"; _ } ->
                 Provider.kind provider = Provider.Plugin
             | _ -> false)
           (Trace.events denied_trace))
        "capability denial was not recorded with the required capability";
      ignore (Zenbu_app.Session.handle_input denied (ctrl "N"));
      expect
        (List.exists
           (function
             | Trace_event.Capability_denied
                 { provider; required = "document.read"; _ } ->
                 Provider.kind provider = Provider.Plugin
             | _ -> false)
           (Trace.events denied_trace))
        "data-only document reads bypassed capability enforcement")

let test_validation_atomicity_collision_and_reload () =
  with_root (fun root ->
      let invalid_id = "zenbu.m8invalid" in
      ignore
        (create_plugin root "invalid" ~id:invalid_id ~version:"1.0.0"
           ~contributions:[ "commands" ] ~capabilities:[]
           {|
zenbu.command { id = "user.not-namespaced", run = function(_) return nil end }
|});
      let invalid =
        Plugins.load ~config:(Plugins.Directories [ root ])
          ~base_commands:(base_commands ()) ~base_semantics:(base_semantics ())
          ()
      in
      let invalid_view = plugin_view invalid invalid_id in
      expect
        (Plugins.view_state invalid_view = Plugins.Failed)
        "namespace-invalid plugin was activated";
      expect
        (Option.bind (Plugins.view_error invalid_view) error_code
        = Some Error.Namespace_violation)
        "namespace violation was not structured";
      Plugins.dispose invalid;
      remove_tree root;
      Unix.mkdir root 0o700;
      let left_id = "zenbu.m8left" in
      let right_id = "zenbu.m8right" in
      ignore
        (create_plugin root "left" ~id:left_id ~version:"1.0.0"
           ~contributions:[ "commands"; "bindings" ]
           ~capabilities:[ "document.edit" ]
           (command_binding_source left_id "L" "Ctrl-K"));
      ignore
        (create_plugin root "right" ~id:right_id ~version:"1.0.0"
           ~contributions:[ "commands"; "bindings" ]
           ~capabilities:[ "document.edit" ]
           (command_binding_source right_id "R" "Ctrl-K"));
      let collision =
        Plugins.load ~config:(Plugins.Directories [ root ])
          ~base_commands:(base_commands ()) ~base_semantics:(base_semantics ())
          ()
      in
      expect
        (Plugins.providers collision = [])
        "colliding plugins partially activated";
      List.iter
        (fun id ->
          let view = plugin_view collision id in
          expect
            (Plugins.view_state view = Plugins.Failed)
            "colliding plugin %s was not failed" id)
        [ left_id; right_id ];
      Plugins.dispose collision;
      remove_tree root;
      Unix.mkdir root 0o700;
      let id = "zenbu.m8reload" in
      let package =
        create_plugin root "reload" ~id ~version:"1.0.0"
          ~contributions:[ "commands"; "bindings"; "events" ]
          ~capabilities:[ "document.edit"; "ui.message"; "event.subscribe" ]
          (plugin_source id "A" "Ctrl-K")
      in
      let initial =
        Plugins.load ~config:(Plugins.Directories [ root ])
          ~base_commands:(base_commands ()) ~base_semantics:(base_semantics ())
          ()
      in
      replace_plugin package ~id ~version:"1.1.0"
        ~contributions:[ "commands"; "bindings"; "events" ]
        ~capabilities:[ "document.edit"; "ui.message"; "event.subscribe" ]
        (plugin_source id "B" "Ctrl-K");
      let reloaded =
        Plugins.reload initial ~base_commands:(base_commands ())
          ~base_semantics:(base_semantics ()) ()
      in
      let updated = plugin_view reloaded id in
      expect
        (Plugins.view_version updated
        |> Option.map Extension.Plugin_version.to_string
        = Some "1.1.0")
        "successful reload did not replace the active plugin version";
      replace_plugin package ~id ~version:"1.2.0"
        ~contributions:[ "commands"; "bindings"; "events" ]
        ~capabilities:[ "document.edit"; "ui.message"; "event.subscribe" ]
        "local =";
      let retained =
        Plugins.reload reloaded ~base_commands:(base_commands ())
          ~base_semantics:(base_semantics ()) ()
      in
      let retained_view = plugin_view retained id in
      expect
        (Plugins.view_version retained_view
        |> Option.map Extension.Plugin_version.to_string
        = Some "1.1.0")
        "failed reload replaced the last known-good plugin";
      expect
        (Option.is_some (Plugins.view_error retained_view))
        "failed reload did not preserve an actionable error on the retained \
         plugin";
      let removed =
        Plugins.deactivate retained (Extension.Plugin_id.of_string id |> must)
      in
      expect
        (Plugins.providers removed = [])
        "plugin deactivation left a provider active")

let test_multiple_plugin_order () =
  with_root (fun root ->
      let alpha = "zenbu.m8alpha" in
      let beta = "zenbu.m8beta" in
      let capabilities = [ "document.edit"; "ui.message"; "event.subscribe" ] in
      let contributions = [ "commands"; "bindings"; "events" ] in
      ignore
        (create_plugin root "alpha" ~id:alpha ~version:"1.0.0" ~contributions
           ~capabilities
           (plugin_source alpha "A" "Ctrl-K"));
      ignore
        (create_plugin root "beta" ~id:beta ~version:"1.0.0" ~contributions
           ~capabilities
           (plugin_source beta "B" "Ctrl-L"));
      let trace = Trace.enabled ~capacity:256 |> must in
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~trace ~config:Zenbu_scripting.Scripting.Disabled
          ~plugins:(Plugins.Directories [ root ]) ~dimensions ()
        |> must
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      expect
        (Zenbu_app.Session.contents session = "Aalpha")
        "first plugin command did not remain independently usable";
      let event_providers =
        Trace.events trace
        |> List.filter_map (function
          | Trace_event.Extension_callback
              { kind = "event"; outcome = "started"; provider; _ } ->
              Provider.plugin_id provider
          | _ -> None)
      in
      expect
        (event_providers = [ alpha; beta ])
        "plugin event delivery is not ordered by plugin ID: %s"
        (String.concat ", " event_providers))

let tests =
  [
    ("contract and manifest validation", test_contract_and_manifest_validation);
    ( "activation, observability, and capability denial",
      test_activation_observability_and_capability_denial );
    ( "validation atomicity, collision, and reload",
      test_validation_atomicity_collision_and_reload );
    ("multiple plugin deterministic order", test_multiple_plugin_order);
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
