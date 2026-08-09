open Zenbu_kernel
open Zenbu_model_api
open Zenbu_proof_models

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

let expect_error = function Error _ -> () | Ok _ -> failf "expected an error"
let document_id value = must (Document_id.of_string value)

let selection anchor_offset head_offset =
  must (Selection_spec.make ~anchor_offset ~head_offset)

let document ?(selections = []) ?(primary = 0) id contents =
  Document.create ~id:(document_id id) ~contents ~initial_selections:selections
    ~primary ()
  |> must

let text document = Document_snapshot.contents (Document.snapshot document)

let selection_offsets document =
  Selection_set.to_list
    (Document_snapshot.selections (Document.snapshot document))
  |> List.map (fun selection ->
      ( Anchor.byte_offset (Selection.anchor selection),
        Anchor.byte_offset (Selection.head selection) ))

let key text = Input_event.key_press (Input_event.logical_text text |> must)
let text_input text = Input_event.text_input text |> must

let registry () =
  Command_registry.register Command_registry.empty Proof_commands.apply_command
  |> must

module Operator_runtime = Model_runtime.Make (Operator_first_model)
module Selection_runtime = Model_runtime.Make (Selection_first_model)

let test_logical_input_representation () =
  let physical = Input_event.physical_key "KeyW" |> must in
  let event =
    Input_event.key_press
      ~modifiers:[ Input_event.Alt; Input_event.Control; Input_event.Alt ]
      ~physical_key:physical
      (Input_event.logical_text "ω" |> must)
  in
  expect
    (Input_event.modifiers event = [ Input_event.Control; Input_event.Alt ])
    "modifiers were not normalized";
  expect
    (Input_event.key event = Some (Input_event.Logical_text "ω"))
    "logical text key was lost";
  expect
    (Option.is_some (Input_event.physical event))
    "physical key metadata was lost";
  let committed = text_input "界" in
  expect
    (Input_event.key committed = None)
    "committed text must not masquerade as a key";
  expect
    (Input_event.text committed = Some "界")
    "committed Unicode text was lost";
  expect_error (Input_event.logical_text (String.make 1 (Char.chr 255)))

let test_selector_transformation_composition () =
  let initial = document "selector" "aé🙂" in
  let history = History.create initial in
  let select_next =
    Model_intent.apply ~selector:Model_intent.Next_text_unit
      ~transformation:Model_intent.Select
  in
  let history =
    History.apply_intent ~source:Transaction.Test history
      (Model_intent.to_kernel select_next)
    |> must
  in
  expect
    (selection_offsets (History.current history) = [ (0, 1) ])
    "next text unit did not select the first UTF-8 code point";
  let replace =
    Model_intent.apply ~selector:Model_intent.Current_selections
      ~transformation:(Model_intent.Replace_text "Z")
  in
  let history =
    History.apply_intent ~source:Transaction.Test history
      (Model_intent.to_kernel replace)
    |> must
  in
  expect_string ~expected:"Zé🙂" ~actual:(text (History.current history));
  let at_end = document ~selections:[ selection 7 7 ] "selector-end" "aé🙂" in
  let history = History.create at_end in
  let next =
    Model_intent.apply ~selector:Model_intent.Next_text_unit
      ~transformation:Model_intent.Delete
  in
  expect_error
    (History.apply_intent ~source:Transaction.Test history
       (Model_intent.to_kernel next));
  expect_string ~expected:"aé🙂" ~actual:(text (History.current history));
  let multiple =
    document
      ~selections:[ selection 0 0; selection 3 3 ]
      ~primary:1 "selector-multiple" "ab cd"
  in
  let history = History.create multiple in
  let history =
    History.apply_intent ~source:Transaction.Test history
      (Model_intent.to_kernel select_next)
    |> must
  in
  expect
    (selection_offsets (History.current history) = [ (0, 1); (3, 4) ])
    "next text unit did not apply consistently to multiple selections"

let test_command_registry () =
  let make_command id title marker =
    let id = Command_id.of_string id |> must in
    let descriptor = Command_descriptor.create ~id ~title () |> must in
    Command.create ~descriptor ~handler:(fun context _invocation ->
        expect_string ~expected:"abc" ~actual:(Editor_context.contents context);
        Ok [ Model_intent.insert_text marker ])
  in
  let zeta = make_command "test.zeta" "Zeta" "z" in
  let alpha = make_command "test.alpha" "Alpha" "a" in
  let registry =
    Command_registry.register Command_registry.empty zeta |> must
  in
  let registry = Command_registry.register registry alpha |> must in
  expect_error (Command_registry.register registry alpha);
  let descriptor_ids =
    Command_registry.descriptors registry
    |> List.map (fun descriptor ->
        Command_id.to_string (Command_descriptor.id descriptor))
  in
  expect
    (descriptor_ids = [ "test.alpha"; "test.zeta" ])
    "command enumeration was not deterministic";
  let alpha_id = Command_id.of_string "test.alpha" |> must in
  ignore (Command_registry.find registry alpha_id |> must);
  let unknown = Command_id.of_string "test.unknown" |> must in
  expect_error (Command_registry.find registry unknown);
  let invocation =
    Command_invocation.create ~id:alpha_id ~arguments:[] |> must
  in
  let context =
    Editor_context.from_snapshot
      ~snapshot:(Document.snapshot (document "command" "abc"))
      ~commands:(Command_registry.descriptors registry)
  in
  let intents = Command_registry.invoke registry ~context invocation |> must in
  expect
    (List.map Model_intent.identity intents = [ "insert-text" ])
    "command invocation did not return semantic intents";
  let apply_descriptor = Command.descriptor Proof_commands.apply_command in
  expect
    (Command_descriptor.description apply_descriptor
    = Some "Apply a model-neutral selector and transformation.")
    "command description metadata was lost";
  expect
    (List.map
       (fun parameter -> parameter.Command_descriptor.name)
       (Command_descriptor.parameters apply_descriptor)
    = [ "selector"; "transformation" ])
    "command parameter metadata was lost";
  expect
    (Command_descriptor.examples apply_descriptor
    = [ "editor.apply(selector: next-text-unit, transformation: delete)" ])
    "command examples metadata was lost"

let test_operator_runtime_state_machine () =
  let original = document "operator" "alpha beta" in
  let runtime =
    Operator_runtime.create ~commands:(registry ()) ~document:original ()
    |> must
  in
  let runtime, pending =
    Operator_runtime.handle_input runtime (key "d") |> must
  in
  expect
    (Model_status.id (Operator_runtime.status_after pending) = "pending-delete")
    "operator model did not enter its pending state";
  expect
    (Operator_runtime.effects pending = [])
    "pending transition emitted an edit";
  expect_string ~expected:"alpha beta"
    ~actual:(text (History.current (Operator_runtime.history runtime)));
  let runtime, cancelled =
    Operator_runtime.handle_input runtime
      (Input_event.key_press (Input_event.named_key Input_event.Escape))
    |> must
  in
  expect
    (Model_status.id (Operator_runtime.status_after cancelled) = "command")
    "Escape did not cancel the pending state";
  let runtime, _ = Operator_runtime.handle_input runtime (key "d") |> must in
  let runtime, applied =
    Operator_runtime.handle_input runtime (key "w") |> must
  in
  expect_string ~expected:"lpha beta"
    ~actual:(text (History.current (Operator_runtime.history runtime)));
  expect
    (List.map Model_intent.identity (Operator_runtime.intents applied)
    = [ "apply:next-text-unit:delete" ])
    "operator transformation did not cross the semantic intent boundary";
  expect
    (List.length (Operator_runtime.input_trace runtime) = 4)
    "input trace did not retain logical events";
  expect_string ~expected:"alpha beta" ~actual:(text original);
  let runtime, inserting =
    Operator_runtime.handle_input runtime (key "i") |> must
  in
  expect
    (Model_status.id (Operator_runtime.status_after inserting) = "inserting")
    "operator model did not enter insertion state";
  let runtime, insertion =
    Operator_runtime.handle_input runtime (text_input "界") |> must
  in
  expect
    (List.map Model_intent.identity (Operator_runtime.intents insertion)
    = [ "insert-text" ])
    "committed text did not resolve into an insertion intent";
  expect_string ~expected:"界lpha beta"
    ~actual:(text (History.current (Operator_runtime.history runtime)));
  let runtime = Operator_runtime.reset runtime |> must in
  expect
    (Model_status.id (Operator_runtime.status runtime) = "command")
    "model reset did not restore its initial state"

let test_runtime_rejected_effect_is_atomic () =
  let bad_id = Command_id.of_string "missing.command" |> must in
  let bad_invocation =
    Command_invocation.create ~id:bad_id ~arguments:[] |> must
  in
  let descriptor =
    Editing_model.descriptor ~id:"test.failing" ~title:"Failing test model" ()
    |> must
  in
  let module Failing_model = struct
    type state = int

    let descriptor = descriptor
    let initialize _ = 0
    let reset _ _ = 0

    let status value =
      Model_status.create ~id:"counter" ~label:(string_of_int value) () |> must

    let handle_input state _ _ =
      (state + 1, [ Model_effect.Invoke_command bad_invocation ])
  end in
  let module Runtime = Model_runtime.Make (Failing_model) in
  let document = document "failed-effect" "abc" in
  let runtime = Runtime.create ~document () |> must in
  expect_error (Runtime.handle_input runtime (key "x"));
  expect_string ~expected:"abc"
    ~actual:(text (History.current (Runtime.history runtime)));
  expect
    (Model_status.label (Runtime.status runtime) = "0")
    "failed effects advanced model state";
  expect
    (Runtime.input_trace runtime = [])
    "failed effects entered the input trace"

let test_runtime_selector_failure_is_atomic () =
  let runtime =
    Selection_runtime.create ~commands:(registry ())
      ~document:(document ~selections:[ selection 5 5 ] "runtime-end" "alpha")
      ()
    |> must
  in
  expect_error (Selection_runtime.handle_input runtime (key "w"));
  expect_string ~expected:"alpha"
    ~actual:(text (History.current (Selection_runtime.history runtime)));
  expect
    (Selection_runtime.input_trace runtime = [])
    "failed selector execution entered the input trace"

let run_operator contents inputs =
  let runtime =
    Operator_runtime.create ~commands:(registry ())
      ~document:(document "operator-cross" contents)
      ()
    |> must
  in
  let runtime =
    List.fold_left
      (fun runtime input ->
        fst (Operator_runtime.handle_input runtime input |> must))
      runtime inputs
  in
  text (History.current (Operator_runtime.history runtime))

let run_selection contents inputs =
  let runtime =
    Selection_runtime.create ~commands:(registry ())
      ~document:(document "selection-cross" contents)
      ()
    |> must
  in
  let runtime =
    List.fold_left
      (fun runtime input ->
        fst (Selection_runtime.handle_input runtime input |> must))
      runtime inputs
  in
  text (History.current (Selection_runtime.history runtime))

let test_cross_model_proof () =
  let same_inputs = [ key "d"; key "w" ] in
  expect_string ~expected:"lpha beta"
    ~actual:(run_operator "alpha beta" same_inputs);
  expect_string ~expected:"alpha beta"
    ~actual:(run_selection "alpha beta" same_inputs);
  let operator_result = run_operator "alpha beta" [ key "d"; key "w" ] in
  let selection_result = run_selection "alpha beta" [ key "w"; key "d" ] in
  expect_string ~expected:operator_result ~actual:selection_result

let test_semantic_replay_is_not_input_trace () =
  let runtime =
    Operator_runtime.create ~commands:(registry ())
      ~document:(document "trace" "alpha") ()
    |> must
  in
  let runtime, _ = Operator_runtime.handle_input runtime (key "d") |> must in
  let runtime, step = Operator_runtime.handle_input runtime (key "w") |> must in
  expect
    (List.length (Operator_runtime.input_trace runtime) = 2)
    "input trace should retain both grammatical events";
  expect
    (List.map Model_intent.identity (Operator_runtime.intents step)
    = [ "apply:next-text-unit:delete" ])
    "one semantic intent should represent the completed grammar";
  let spec = selection 0 0 in
  let replay =
    Replay.create ~document_id:"semantic-only" ~contents:"alpha"
      ~initial_selections:{ Replay.selections = [ spec ]; primary = 0 }
      ~actions:
        [
          Replay.Intent
            (Intent.Apply
               {
                 selector = Selector.Next_text_unit;
                 transformation = Transformation.Delete;
               });
        ]
    |> must
  in
  let encoded = Replay.to_string replay in
  expect
    (not (String.contains encoded 'd' && String.contains encoded 'w'))
    "semantic replay encoded raw grammar input";
  let replay = Replay.of_string encoded |> must in
  let history = Replay.run replay |> must in
  expect_string ~expected:"lpha" ~actual:(text (History.current history))

let test_model_determinism () =
  let create () =
    Operator_runtime.create ~commands:(registry ())
      ~document:(document "deterministic" "alpha")
      ()
    |> must
  in
  let left, left_step =
    Operator_runtime.handle_input (create ()) (key "d") |> must
  in
  let right, right_step =
    Operator_runtime.handle_input (create ()) (key "d") |> must
  in
  expect
    (Model_status.id (Operator_runtime.status left)
    = Model_status.id (Operator_runtime.status right))
    "same model/input/context produced different states";
  expect
    (Operator_runtime.effects left_step = Operator_runtime.effects right_step)
    "same model/input/context produced different effects"

let tests =
  [
    ("logical input representation", test_logical_input_representation);
    ( "selector and transformation composition",
      test_selector_transformation_composition );
    ("command registry", test_command_registry);
    ("operator-first runtime state machine", test_operator_runtime_state_machine);
    ("rejected runtime effect is atomic", test_runtime_rejected_effect_is_atomic);
    ( "rejected runtime selector is atomic",
      test_runtime_selector_failure_is_atomic );
    ("cross-model proof", test_cross_model_proof);
    ( "semantic replay versus input trace",
      test_semantic_replay_is_not_input_trace );
    ("model determinism", test_model_determinism);
  ]

let () =
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
