open Zenbu_kernel
open Zenbu_model_api
open Zenbu_proof_models

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
  match Input_event.logical_text text with
  | Ok key -> Input_event.key_press ~modifiers:[ Input_event.Control ] key
  | Error error -> fail error

module Vim_runtime = Model_runtime.Make (Vim_model)
module Selection_runtime = Model_runtime.Make (Selection_model)

let semantic_registry () =
  match
    Command_registry.register Command_registry.empty Semantic_commands.apply_command
  with
  | Ok registry -> registry
  | Error error -> fail error

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
        (Transaction.source_to_string (Transaction.source metadata)) intent
        description
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
  Printf.printf "step %d: %s -> %s\n" index (Input_event.to_string input)
    (Model_status.label status);
  print_effects effects;
  print_intents intents;
  let snapshot = Document.snapshot (History.current history) in
  Printf.printf "  document: %S\n" (Document_snapshot.contents snapshot)

let run_vim_session contents inputs =
  let runtime =
    match
      Vim_runtime.create ~commands:(semantic_registry ())
        ~document:(document_for "vim-session" contents) ()
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
            print_step (List.length (Vim_runtime.input_trace runtime)) input
              (Vim_runtime.effects step) (Vim_runtime.intents step)
              (Vim_runtime.status_after step) (Vim_runtime.history runtime);
            runtime)
      runtime inputs
  in
  print_result (Vim_runtime.history runtime)

let run_selection_session contents inputs =
  let runtime =
    match
      Selection_runtime.create ~commands:(semantic_registry ())
        ~document:(document_for "selection-session" contents) ()
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
            print_step (List.length (Selection_runtime.input_trace runtime)) input
              (Selection_runtime.effects step) (Selection_runtime.intents step)
              (Selection_runtime.status_after step)
              (Selection_runtime.history runtime);
            runtime)
      runtime inputs
  in
  print_result (Selection_runtime.history runtime)

type session_model = Vim | Selection_first

type session = {
  model : session_model;
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
  | "Ctrl-r" -> Ok (control_key "r")
  | value -> Ok (logical_key value)

let session_of_string text =
  let parse_line session line =
    if String.length line = 0 || line.[0] = '#' then Ok session
    else
      match String.split_on_char '=' line with
      | [ "model"; "vim" ] -> Ok { session with model = Vim }
      | [ "model"; "selection-first" ] ->
          Ok { session with model = Selection_first }
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
  let initial = { model = Vim; contents = ""; inputs = [] } in
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
  | Ok { model = Vim; contents; inputs } -> run_vim_session contents inputs
  | Ok { model = Selection_first; contents; inputs } ->
      run_selection_session contents inputs

let demo () =
  Printf.printf "Zenbu M3: incompatible grammars, shared semantics\n\n";
  Printf.printf "Initial: \"alpha beta gamma\"\n\nVIM-STYLE: d w\n";
  run_vim_session "alpha beta gamma" [ logical_key "d"; logical_key "w" ];
  Printf.printf "\nSELECTION-FIRST: w d\n";
  run_selection_session "alpha beta gamma" [ logical_key "w"; logical_key "d" ];
  Printf.printf "\nSELECTION-FIRST MULTI-SELECTION: W * d\n";
  run_selection_session "foo bar foo baz foo"
    [ logical_key "W"; logical_key "*"; logical_key "d" ]

let usage () =
  prerr_endline
    "usage: zenbu-headless demo | replay <fixture.replay> | session <fixture.session>";
  exit 2

let () =
  match Array.to_list Sys.argv with
  | [ _; "demo" ] -> demo ()
  | [ _; "session"; path ] -> run_session path
  | [ _; "replay"; path ] -> (
      match Replay.of_string (read_file path) with
      | Ok replay -> (
          match Replay.run replay with
          | Ok history -> print_result history
          | Error error -> fail error)
      | Error error -> fail error)
  | _ -> usage ()
