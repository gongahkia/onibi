open Zenbu_kernel
open Zenbu_model_api
open Zenbu_proof_models

let fail error =
  prerr_endline (Error.to_string error);
  exit 1

let proof_registry () =
  match
    Command_registry.register Command_registry.empty
      Proof_commands.apply_command
  with
  | Ok registry -> registry
  | Error error -> fail error

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

module Operator_runtime = Model_runtime.Make (Operator_first_model)
module Selection_runtime = Model_runtime.Make (Selection_first_model)

let read_file path =
  let channel = open_in_bin path in
  Fun.protect
    ~finally:(fun () -> close_in_noerr channel)
    (fun () -> really_input_string channel (in_channel_length channel))

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

let print_effects effects =
  match effects with
  | [] -> Printf.printf "Effect: none\n"
  | effects ->
      List.iter
        (fun model_effect ->
          Printf.printf "Effect: %s\n" (Model_effect.describe model_effect))
        effects

let print_intents intents =
  List.iter
    (fun intent ->
      Printf.printf "Semantic intent: %s\n" (Model_intent.identity intent))
    intents

let demo () =
  let initial = "alpha beta gamma" in
  let registry = proof_registry () in
  let operator =
    match
      Operator_runtime.create ~commands:registry
        ~document:(document_for "operator-demo" initial)
        ()
    with
    | Ok runtime -> runtime
    | Error error -> fail error
  in
  let operator, _ =
    match Operator_runtime.handle_input operator (logical_key "d") with
    | Ok value -> value
    | Error error -> fail error
  in
  let operator, operator_step =
    match Operator_runtime.handle_input operator (logical_key "w") with
    | Ok value -> value
    | Error error -> fail error
  in
  Printf.printf "Initial:\n%S\n\n" initial;
  Printf.printf "Model: operator-first\nInput: d w\n";
  print_effects (Operator_runtime.effects operator_step);
  print_intents (Operator_runtime.intents operator_step);
  Printf.printf "Result:\n%S\n\n"
    (Document_snapshot.contents
       (Document.snapshot (History.current (Operator_runtime.history operator))));
  let selection =
    match
      Selection_runtime.create ~commands:registry
        ~document:(document_for "selection-demo" initial)
        ()
    with
    | Error error -> fail error
    | Ok runtime -> runtime
  in
  let selection, selection_step =
    match Selection_runtime.handle_input selection (logical_key "w") with
    | Ok value -> value
    | Error error -> fail error
  in
  let selection, delete_step =
    match Selection_runtime.handle_input selection (logical_key "d") with
    | Ok value -> value
    | Error error -> fail error
  in
  Printf.printf "Model: selection-first\nInput: w d\n";
  print_effects (Selection_runtime.effects selection_step);
  print_intents (Selection_runtime.intents selection_step);
  print_effects (Selection_runtime.effects delete_step);
  print_intents (Selection_runtime.intents delete_step);
  Printf.printf "Result:\n%S\n"
    (Document_snapshot.contents
       (Document.snapshot
          (History.current (Selection_runtime.history selection))))

let usage () =
  prerr_endline "usage: zenbu-headless demo | replay <fixture.replay>";
  exit 2

let () =
  let replay =
    match Array.to_list Sys.argv with
    | [ _; "demo" ] ->
        demo ();
        exit 0
    | [ _; "replay"; path ] -> (
        match Replay.of_string (read_file path) with
        | Ok replay -> replay
        | Error error -> fail error)
    | _ -> usage ()
  in
  match Replay.run replay with
  | Ok history -> print_result history
  | Error error -> fail error
