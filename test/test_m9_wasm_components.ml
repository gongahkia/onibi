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
  lazy
    (fixture "test/fixtures/m9_component.wasm.b64" |> read |> decode_base64)

let wasi_fixture =
  lazy
    (fixture "test/fixtures/m9_wasi_component.wasm.b64" |> read |> decode_base64)

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

let create_package root ?(entrypoint = "plugin.wasm") ?(binary = component_fixture)
    ~id ~version ~contributions ~capabilities () =
  let package = Filename.concat root "fixture" in
  Unix.mkdir package 0o700;
  write (Filename.concat package entrypoint) (Lazy.force binary);
  write (Filename.concat package "zenbu-plugin.toml")
    (manifest ~entrypoint ~id ~version ~contributions ~capabilities ());
  package

let ctrl text =
  Input_event.logical_text (String.lowercase_ascii text)
  |> must
  |> Input_event.key_press ~modifiers:[ Input_event.Control ]

let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 }

let session root ?(trace = Trace.disabled ()) () =
  Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha" ~trace
    ~config:Zenbu_scripting.Scripting.Disabled
    ~plugins:(Plugins.Directories [ root ]) ~dimensions ()
  |> must

let scripts value =
  Zenbu_app.Session.inspect value Zenbu_app.Session.Scripts |> String.concat "\n"

let base_commands () =
  Command_registry.register Command_registry.empty
    Zenbu_proof_models.Semantic_commands.apply_command
  |> must

let error_code = function
  | Error.Extension_error { code; _ } -> Some code
  | _ -> None

let test_component_semantic_conformance_and_provenance () =
  with_root (fun root ->
      ignore
        (create_package root ~id:"com.example.m9" ~version:"1.0.0"
           ~contributions:[ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:[ "document.edit"; "selection.read"; "selection.write" ]
           ());
      let trace = Trace.enabled ~capacity:256 |> must in
      let inserted = session root ~trace () |> fun value -> Zenbu_app.Session.handle_input value (ctrl "K") in
      expect (Zenbu_app.Session.contents inserted = "!alpha")
        "Component command did not commit through the shared semantic path";
      let applied = session root () |> fun value -> Zenbu_app.Session.handle_input value (ctrl "A") in
      expect (Zenbu_app.Session.contents applied = "done")
        "Component selector/transformation did not produce a normal transaction: %s"
        (scripts applied);
      let history =
        Zenbu_app.Session.inspect inserted Zenbu_app.Session.History
        |> String.concat "\n"
      in
      expect (contains history "com.example.m9@1.0.0 (wasm-component)")
        "Component provenance omitted plugin version/runtime: %s" history;
      expect
        (List.exists
           (function
             | Trace_event.Extension_callback { provider; _ } ->
                 Provider.runtime provider = Some "wasm-component"
             | _ -> false)
           (Trace.events trace))
        "Component callback was absent from normal extension tracing")

let test_callback_failures_are_nonmutating_and_classified () =
  with_root (fun root ->
      ignore
        (create_package root ~id:"com.example.m9" ~version:"1.0.0"
           ~contributions:[ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:[ "document.edit"; "selection.read"; "selection.write" ]
           ());
      let assert_failure input code fragment =
        let value = session root () |> fun value -> Zenbu_app.Session.handle_input value (ctrl input) in
        expect (Zenbu_app.Session.contents value = "alpha")
          "failed Component callback %s changed the document" input;
        let message = scripts value in
        expect (contains message code)
          "Component callback %s did not report %s: %s" input code message;
        expect (contains message fragment)
          "Component callback %s omitted failure detail %S: %s" input fragment message
      in
      assert_failure "B" "extension-runtime-error" "WIT value has no root node";
      assert_failure "I" "extension-runtime-error" "unknown action kind";
      assert_failure "T" "extension-trap" "wasm trap";
      assert_failure "L" "extension-fuel-exhausted" "all fuel consumed";
      assert_failure "M" "extension-memory-exhausted" "memory allocation denied")

let test_capability_denial_remains_at_the_host_boundary () =
  with_root (fun root ->
      ignore
        (create_package root ~id:"com.example.m9" ~version:"1.0.0"
           ~contributions:[ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:[] ());
      let trace = Trace.enabled ~capacity:128 |> must in
      let value = session root ~trace () |> fun value -> Zenbu_app.Session.handle_input value (ctrl "K") in
      expect (Zenbu_app.Session.contents value = "alpha")
        "capability-denied Component action mutated the document";
      expect (contains (scripts value) "extension capability-denied")
        "capability denial was not surfaced by the session";
      expect
        (List.exists
           (function
             | Trace_event.Capability_denied { required = "document.edit"; _ } -> true
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
        expect (Plugins.view_state view = Plugins.Failed)
          "invalid Component package became active";
        expect (Plugins.providers plugins = [])
          "invalid Component package left a provider active";
        let error = Plugins.view_error view |> Option.get in
        expect (error_code error = Some expected)
          "invalid Component package returned %s instead of expected runtime code"
          (Error.to_string error);
        Plugins.dispose plugins)
  in
  assert_failed ~binary:wasi_fixture ~entrypoint:"wasi.wasm"
    Error.Extension_abi_mismatch;
  assert_failed ~binary:(lazy "not a WebAssembly component") ~entrypoint:"bad.wasm"
    Error.Extension_runtime_error

let test_failed_wasm_reload_retains_the_generation () =
  with_root (fun root ->
      let package =
        create_package root ~id:"com.example.m9" ~version:"1.0.0"
          ~contributions:[ "commands"; "selectors"; "transformations"; "bindings" ]
          ~capabilities:[ "document.edit"; "selection.read"; "selection.write" ] ()
      in
      let initial =
        Plugins.load ~config:(Plugins.Directories [ root ])
          ~base_commands:(base_commands ()) ~base_semantics:[] ()
      in
      write (Filename.concat package "bad.wasm") "invalid component";
      write (Filename.concat package "zenbu-plugin.toml")
        (manifest ~entrypoint:"bad.wasm" ~id:"com.example.m9" ~version:"1.1.0"
           ~contributions:[ "commands"; "selectors"; "transformations"; "bindings" ]
           ~capabilities:[ "document.edit"; "selection.read"; "selection.write" ] ());
      let retained =
        Plugins.reload initial ~base_commands:(base_commands ()) ~base_semantics:[] ()
      in
      let view =
        match Plugins.views retained with
        | [ view ] -> view
        | _ -> failf "failed reload lost the previous Wasm generation"
      in
      expect (Plugins.view_state view = Plugins.Active)
        "failed Wasm reload did not retain the last-known-good generation";
      expect
        (Plugins.view_version view |> Option.map Extension.Plugin_version.to_string
        = Some "1.0.0")
        "failed Wasm reload replaced the active manifest";
      expect (Option.is_some (Plugins.view_error view))
        "failed Wasm reload did not retain the actionable staging error";
      Plugins.dispose retained)

let tests =
  [
    ( "Component semantic conformance and provenance",
      test_component_semantic_conformance_and_provenance );
    ( "Component failure isolation and resource classification",
      test_callback_failures_are_nonmutating_and_classified );
    ( "Component capability denial stays at the host boundary",
      test_capability_denial_remains_at_the_host_boundary );
    ( "WASI and malformed Components are rejected",
      test_invalid_wasi_component_and_malformed_binary_are_rejected );
    ("failed Component reload retains generation", test_failed_wasm_reload_retains_the_generation);
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
