open Zenbu_kernel

let fail error =
  prerr_endline (Error.to_string error);
  exit 1

let selection anchor_offset head_offset =
  match Selection_spec.make ~anchor_offset ~head_offset with
  | Ok selection -> selection
  | Error error -> fail error

let demo_replay () =
  let initial_selections =
    { Replay.selections = [ selection 0 0 ]; primary = 0 }
  in
  match
    Replay.create ~document_id:"demo" ~contents:"world" ~initial_selections
      ~actions:
        [
          Replay.Intent (Intent.Insert_text "hello ");
          Replay.Intent
            (Intent.Set_selections
               { selections = [ selection 6 11 ]; primary = 0 });
          Replay.Intent (Intent.Replace_selected_ranges "zenbu");
          Replay.Transaction
            {
              source = Transaction.System;
              intent = Some "demo-punctuation";
              description = Some "append punctuation";
              edits =
                [ { Replay.start_offset = 11; stop_offset = 11; text = "!" } ];
              selection_change = None;
            };
        ]
  with
  | Ok replay -> replay
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

let usage () =
  prerr_endline "usage: zenbu-headless demo | replay <fixture.replay>";
  exit 2

let () =
  let replay =
    match Array.to_list Sys.argv with
    | [ _; "demo" ] -> demo_replay ()
    | [ _; "replay"; path ] -> (
        match Replay.of_string (read_file path) with
        | Ok replay -> replay
        | Error error -> fail error)
    | _ -> usage ()
  in
  match Replay.run replay with
  | Ok history -> print_result history
  | Error error -> fail error
