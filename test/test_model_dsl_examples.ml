open Zenbu_kernel
open Zenbu_model_api
open Zenbu_proof_models
module Dsl = Zenbu_model_dsl

exception Test_failure of string

let failf format =
  Printf.ksprintf (fun message -> raise (Test_failure message)) format

let expect condition format =
  Printf.ksprintf
    (fun message -> if not condition then raise (Test_failure message))
    format

let expect_string ~expected ~actual =
  expect (String.equal expected actual) "expected %S, got %S" expected actual

let must = function
  | Ok value -> value
  | Error error -> failf "%s" (Error.to_string error)

let read_file path =
  let channel = open_in_bin path in
  Fun.protect
    ~finally:(fun () -> close_in_noerr channel)
    (fun () -> really_input_string channel (in_channel_length channel))

let command_registry () =
  List.fold_left
    (fun registry command ->
      Result.bind registry (fun registry ->
          Command_registry.register registry command))
    (Ok Command_registry.empty)
    (Semantic_commands.apply_command :: Semantic_commands.selection_commands)
  |> must

let compile path =
  match
    Dsl.Compile.compile ~commands:(command_registry ()) ~source_name:path
      ~source:(read_file path) ()
  with
  | Ok (grammar, warnings) ->
      expect (warnings = []) "%s unexpectedly produced warnings" path;
      grammar
  | Error diagnostics ->
      diagnostics
      |> List.map Dsl.Diagnostic.format
      |> String.concat "\n" |> failf "%s"

let key text = Input_event.key_press (Input_event.logical_text text |> must)
let named value = Input_event.key_press (Input_event.named_key value)

let shift_named value =
  Input_event.key_press ~modifiers:[ Input_event.Shift ]
    (Input_event.named_key value)

let text value = Input_event.text_input value |> must

let contains text fragment =
  let text_length = String.length text in
  let fragment_length = String.length fragment in
  let rec loop offset =
    if offset + fragment_length > text_length then false
    else if String.sub text offset fragment_length = fragment then true
    else loop (offset + 1)
  in
  fragment_length = 0 || loop 0

let transition_count (grammar : Dsl.Compile.t) =
  List.fold_left
    (fun total (state : Dsl.Compile.compiled_state) ->
      total + List.length state.ir.transitions)
    0 grammar.states

let prefix_count (grammar : Dsl.Compile.t) =
  List.fold_left
    (fun total state -> total + List.length (Dsl.Compile.prefixes state))
    0 grammar.states

module Runtime = Model_runtime.Make (Dsl.Runtime.Adapter)

let contents runtime =
  Runtime.history runtime |> History.current |> Document.snapshot
  |> Document_snapshot.contents

let make_runtime grammar ~id contents =
  let document =
    Document.create ~id:(Document_id.of_string id |> must) ~contents () |> must
  in
  Dsl.Runtime.Adapter.clear ();
  Dsl.Runtime.Adapter.configure grammar;
  Fun.protect ~finally:Dsl.Runtime.Adapter.clear (fun () ->
      Runtime.create ~commands:(command_registry ()) ~document () |> must)

let input_rule_exists rules pattern kind =
  List.exists
    (fun rule ->
      Input_rule.pattern rule = pattern && Input_rule.kind rule = kind)
    rules

let test_modal_operator (grammar : Dsl.Compile.t) =
  expect
    (List.length grammar.states = 2)
    "modal example did not retain two declared states";
  expect
    (transition_count grammar = 20)
    "modal example transition count changed unexpectedly";
  expect
    (prefix_count grammar = 3)
    "modal example should generate d, c, and v prefix nodes";
  expect
    (List.length grammar.ir.actions = 2)
    "modal example did not retain its compile-time actions";
  let description = Dsl.Describe.render ~warnings:[] grammar in
  expect_string ~expected:description
    ~actual:(Dsl.Describe.render ~warnings:[] grammar);
  expect
    (contains description "prefix: d")
    "modal description omitted the delete prefix";
  let runtime = make_runtime grammar ~id:"dsl-modal-insert" "alpha beta" in
  let runtime, _ = Runtime.handle_input runtime (key "i") |> must in
  expect_string ~expected:"insert"
    ~actual:(Model_status.id (Runtime.status runtime));
  let runtime, _ = Runtime.handle_input runtime (text "界") |> must in
  expect_string ~expected:"界alpha beta" ~actual:(contents runtime);
  let runtime, _ =
    Runtime.handle_input runtime (named Input_event.Escape) |> must
  in
  expect_string ~expected:"normal"
    ~actual:(Model_status.id (Runtime.status runtime));
  let runtime = make_runtime grammar ~id:"dsl-modal-delete" "alpha beta" in
  let runtime, prefix_step = Runtime.handle_input runtime (key "d") |> must in
  expect (Runtime.effects prefix_step = []) "modal d prefix emitted an effect";
  expect
    (Model_status.pending_input (Runtime.status runtime) = Some "d")
    "modal d prefix was not visible through status";
  expect
    (input_rule_exists
       (Runtime.input_rules runtime)
       (Input_rule.Exact "w") Input_rule.Binding)
    "modal pending input rules omitted d w continuation";
  let runtime, delete_step = Runtime.handle_input runtime (key "w") |> must in
  expect
    (List.map Model_effect.identity (Runtime.effects delete_step)
    = [ "execute apply:current-word:delete" ])
    "modal d w emitted the wrong semantic operation";
  expect_string ~expected:" beta" ~actual:(contents runtime);
  let runtime =
    make_runtime grammar ~id:"dsl-modal-shared-prefix" "alpha beta"
  in
  let runtime, _ = Runtime.handle_input runtime (key "d") |> must in
  let _runtime, line_step = Runtime.handle_input runtime (key "l") |> must in
  expect
    (List.map Model_effect.identity (Runtime.effects line_step)
    = [ "execute apply:current-line:delete" ])
    "modal d l did not select the shared d prefix branch";
  let runtime = make_runtime grammar ~id:"dsl-modal-mismatch" "alpha beta" in
  let runtime, _ = Runtime.handle_input runtime (key "d") |> must in
  let runtime, mismatch = Runtime.handle_input runtime (key "x") |> must in
  expect
    (Runtime.effects mismatch = [])
    "modal prefix mismatch emitted an effect";
  expect_string ~expected:"alpha beta" ~actual:(contents runtime);
  expect
    (Model_status.pending_input (Runtime.status runtime) = None)
    "modal prefix mismatch did not clear its pending input"

let test_selection_first (grammar : Dsl.Compile.t) =
  expect
    (List.length grammar.states = 2)
    "selection-first example did not retain two declared states";
  expect
    (transition_count grammar = 24)
    "selection-first example transition count changed unexpectedly";
  expect
    (prefix_count grammar = 1)
    "selection-first example should generate only the g prefix node";
  let runtime = make_runtime grammar ~id:"dsl-selection-first" "alpha beta" in
  expect_string ~expected:"select"
    ~actual:(Model_status.id (Runtime.status runtime));
  let runtime, select_step = Runtime.handle_input runtime (key "w") |> must in
  expect
    (List.map Model_effect.identity (Runtime.effects select_step)
    = [ "execute apply:next-word:select" ])
    "selection-first w did not create a visible selection";
  let runtime, merge_step = Runtime.handle_input runtime (key "m") |> must in
  expect
    (List.map Model_effect.identity (Runtime.effects merge_step)
    = [ "invoke editor.selection.merge-consecutive" ])
    "selection-first m did not invoke the registered selection command";
  expect_string ~expected:"alpha beta" ~actual:(contents runtime);
  let runtime, delete_step = Runtime.handle_input runtime (key "d") |> must in
  expect
    (List.map Model_effect.identity (Runtime.effects delete_step)
    = [ "execute apply:current-selections:delete" ])
    "selection-first d did not act on the current visible selection";
  expect_string ~expected:"beta" ~actual:(contents runtime);
  let runtime = make_runtime grammar ~id:"dsl-selection-prefix" "alpha beta" in
  let runtime, prefix_step = Runtime.handle_input runtime (key "g") |> must in
  expect
    (Runtime.effects prefix_step = [])
    "selection-first g emitted an effect";
  expect
    (Model_status.pending_input (Runtime.status runtime) = Some "g")
    "selection-first g prefix was not inspectable";
  let runtime, start_step = Runtime.handle_input runtime (key "g") |> must in
  expect
    (List.map Model_effect.identity (Runtime.effects start_step)
    = [ "execute apply:document-start:select" ])
    "selection-first g g emitted the wrong semantic operation";
  expect_string ~expected:"select"
    ~actual:(Model_status.id (Runtime.status runtime));
  let runtime = make_runtime grammar ~id:"dsl-selection-change" "alpha beta" in
  let runtime, _ = Runtime.handle_input runtime (key "w") |> must in
  let runtime, _ = Runtime.handle_input runtime (key "c") |> must in
  expect_string ~expected:"insert"
    ~actual:(Model_status.id (Runtime.status runtime));
  let runtime, _ = Runtime.handle_input runtime (text "X") |> must in
  expect_string ~expected:"Xbeta" ~actual:(contents runtime)

let test_direct (grammar : Dsl.Compile.t) =
  expect
    (List.length grammar.states = 1)
    "direct example should have one user-declared state";
  expect
    (transition_count grammar = 19)
    "direct example transition count changed unexpectedly";
  expect
    (prefix_count grammar = 0)
    "direct example should not create prefix nodes";
  let description = Dsl.Describe.render ~warnings:[] grammar in
  expect
    (contains description "state: direct")
    "direct description omitted its only state";
  let runtime = make_runtime grammar ~id:"dsl-direct-text" "alpha" in
  expect_string ~expected:"direct"
    ~actual:(Model_status.id (Runtime.status runtime));
  expect
    (input_rule_exists
       (Runtime.input_rules runtime)
       Input_rule.Text_input Input_rule.Catch_all)
    "direct grammar did not expose committed text as a catch-all rule";
  let runtime, _ = Runtime.handle_input runtime (text "界") |> must in
  expect_string ~expected:"界alpha" ~actual:(contents runtime);
  let runtime = make_runtime grammar ~id:"dsl-direct-select" "alpha" in
  let _runtime, select_step =
    Runtime.handle_input runtime (shift_named Input_event.Arrow_right) |> must
  in
  expect
    (List.map Model_effect.identity (Runtime.effects select_step)
    = [ "execute apply:next-text-unit:select" ])
    "direct Shift-ArrowRight did not select the next text unit";
  let runtime = make_runtime grammar ~id:"dsl-direct-delete" "alpha" in
  let runtime, delete_step =
    Runtime.handle_input runtime (named Input_event.Delete) |> must
  in
  expect
    (List.map Model_effect.identity (Runtime.effects delete_step)
    = [ "execute apply:next-text-unit:delete" ])
    "direct Delete did not request an immediate semantic edit";
  expect_string ~expected:"lpha" ~actual:(contents runtime);
  let runtime =
    make_runtime grammar ~id:"dsl-direct-delete-selection" "alpha"
  in
  let runtime, _ =
    Runtime.handle_input runtime (shift_named Input_event.Arrow_right) |> must
  in
  let runtime, delete_selection_step =
    Runtime.handle_input runtime (named Input_event.Delete) |> must
  in
  expect
    (List.map Model_effect.identity (Runtime.effects delete_selection_step)
    = [ "execute apply:current-selections:delete" ])
    "direct Delete did not select the non-empty-selection guard arm";
  expect_string ~expected:"lpha" ~actual:(contents runtime);
  let runtime = make_runtime grammar ~id:"dsl-direct-enter" "alpha" in
  let runtime, enter_step =
    Runtime.handle_input runtime (named Input_event.Enter) |> must
  in
  expect
    (List.map Model_effect.identity (Runtime.effects enter_step)
    = [ "execute apply:current-selections:replace-text" ])
    "direct Enter did not use the existing literal replacement transformation";
  expect_string ~expected:"\nalpha" ~actual:(contents runtime)

let () =
  if Array.length Sys.argv <> 4 then
    failwith "expected modal, selection-first, and direct example paths";
  let modal = compile Sys.argv.(1) in
  let selection = compile Sys.argv.(2) in
  let direct = compile Sys.argv.(3) in
  let tests =
    [
      ("modal operator example", fun () -> test_modal_operator modal);
      ("selection-first example", fun () -> test_selection_first selection);
      ("direct example", fun () -> test_direct direct);
    ]
  in
  let failures =
    List.fold_left
      (fun count (name, test) ->
        try
          test ();
          Printf.printf "ok - %s\n%!" name;
          count
        with
        | Test_failure message ->
            Printf.eprintf "not ok - %s: %s\n%!" name message;
            count + 1
        | exception_ ->
            Printf.eprintf "not ok - %s: unexpected %s\n%!" name
              (Printexc.to_string exception_);
            count + 1)
      0 tests
  in
  if failures <> 0 then exit 1
