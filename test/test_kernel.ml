open Zenbu_kernel

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

let selection_offsets selections =
  List.map
    (fun selection ->
      ( Anchor.byte_offset (Selection.anchor selection),
        Anchor.byte_offset (Selection.head selection) ))
    (Selection_set.to_list selections)

let transaction snapshot edits =
  Transaction.create
    ~document_id:(Document_snapshot.document_id snapshot)
    ~source_version:(Document_snapshot.version snapshot)
    ~edits
    ~metadata:(Transaction.metadata ~source:Transaction.Test ())
    ()
  |> must

let replace snapshot start_offset stop_offset replacement =
  Document_snapshot.range snapshot ~start_offset ~stop_offset |> must
  |> fun range -> Edit.replace range ~text:replacement |> must

let insert snapshot offset replacement =
  Document_snapshot.anchor snapshot ~byte_offset:offset |> must |> fun anchor ->
  Edit.insert ~at:anchor ~text:replacement |> must

let apply_intent history intent =
  History.apply_intent ~source:Transaction.Test history intent |> must

let test_document_snapshots_and_unicode_boundaries () =
  let doc = document "unicode-boundaries" "aé🙂" in
  let snapshot = Document.snapshot doc in
  expect
    (Document_snapshot.byte_length snapshot = 7)
    "unexpected UTF-8 byte length";
  List.iter
    (fun offset ->
      ignore (Document_snapshot.anchor snapshot ~byte_offset:offset |> must))
    [ 0; 1; 3; 7 ];
  List.iter
    (fun offset ->
      expect_error (Document_snapshot.anchor snapshot ~byte_offset:offset))
    [ -1; 2; 4; 8 ];
  let other = document "other" "aé🙂" in
  let foreign_anchor =
    Document_snapshot.anchor (Document.snapshot other) ~byte_offset:0 |> must
  in
  expect_error (Document_snapshot.validate_anchor snapshot foreign_anchor)

let test_selection_set_invariants () =
  let document =
    document
      ~selections:[ selection 5 3; selection 0 1 ]
      ~primary:0 "selection-invariants" "abcdef"
  in
  let selections = Document_snapshot.selections (Document.snapshot document) in
  expect
    (Selection_set.primary_index selections = 1)
    "primary index was not normalized";
  expect
    (selection_offsets selections = [ (0, 1); (5, 3) ])
    "selection ordering or direction changed";
  let snapshot = Document.snapshot document in
  let range =
    Document_snapshot.range snapshot ~start_offset:0 ~stop_offset:2 |> must
  in
  let range_overlap =
    Document_snapshot.range snapshot ~start_offset:1 ~stop_offset:3 |> must
  in
  let first =
    Selection.make ~anchor:(Range.start range) ~head:(Range.stop range) |> must
  in
  let duplicate =
    Selection.make ~anchor:(Range.start range) ~head:(Range.stop range) |> must
  in
  let overlapping =
    Selection.make
      ~anchor:(Range.start range_overlap)
      ~head:(Range.stop range_overlap)
    |> must
  in
  expect_error (Selection_set.create ~primary:0 [ first; duplicate ]);
  expect_error (Selection_set.create ~primary:0 [ first; overlapping ]);
  let touching =
    Document_snapshot.range snapshot ~start_offset:2 ~stop_offset:3 |> must
  in
  let touching_selection =
    Selection.make ~anchor:(Range.start touching) ~head:(Range.stop touching)
    |> must
  in
  ignore (Selection_set.create ~primary:0 [ first; touching_selection ] |> must)

let test_transaction_ordering_and_conflicts () =
  let doc = document "ordering" "abcd" in
  let snapshot = Document.snapshot doc in
  let edits =
    [
      replace snapshot 1 3 "Z";
      insert snapshot 1 "X";
      insert snapshot 1 "Y";
      insert snapshot 3 "Q";
    ]
  in
  let updated = Document.apply doc (transaction snapshot edits) |> must in
  expect_string ~expected:"aXYZQd" ~actual:(text updated);
  let conflict_document = document "conflict" "abcd" in
  let conflict_snapshot = Document.snapshot conflict_document in
  let conflicting =
    [ replace conflict_snapshot 1 3 "Z"; insert conflict_snapshot 2 "inside" ]
  in
  expect_error
    (Transaction.create
       ~document_id:(Document_snapshot.document_id conflict_snapshot)
       ~source_version:(Document_snapshot.version conflict_snapshot)
       ~edits:conflicting
       ~metadata:(Transaction.metadata ~source:Transaction.Test ())
       ());
  let overlap =
    [ replace conflict_snapshot 0 2 "x"; replace conflict_snapshot 1 3 "y" ]
  in
  expect_error
    (Transaction.create
       ~document_id:(Document_snapshot.document_id conflict_snapshot)
       ~source_version:(Document_snapshot.version conflict_snapshot)
       ~edits:overlap
       ~metadata:(Transaction.metadata ~source:Transaction.Test ())
       ())

let test_rejection_is_atomic_and_versions_are_checked () =
  let document = document "atomic" "abc" in
  let snapshot = Document.snapshot document in
  let transaction = transaction snapshot [ insert snapshot 1 "X" ] in
  let changed = Document.apply document transaction |> must in
  expect_string ~expected:"aXbc" ~actual:(text changed);
  expect_error (Document.apply changed transaction);
  expect_string ~expected:"aXbc" ~actual:(text changed);
  expect
    (Document_version.to_int (Document.version changed) = 1)
    "document version did not increment"

let test_intents_resolve_to_transactions () =
  let document = document ~selections:[ selection 1 3 ] "intent" "abcd" in
  let history = History.create document in
  let history = apply_intent history (Intent.Replace_selected_ranges "X") in
  expect_string ~expected:"aXd" ~actual:(text (History.current history));
  let snapshot = Document.snapshot (History.current history) in
  expect
    (selection_offsets (Document_snapshot.selections snapshot) = [ (2, 2) ])
    "replacement did not place the selection after replacement text";
  let change =
    match History.current_change history with
    | Some change -> change
    | None -> failf "missing committed change"
  in
  let metadata = Transaction.metadata_of (History.transaction change) in
  expect
    (Transaction.intent metadata = Some "replace-selected-ranges")
    "intent identity was not retained in transaction metadata"

let test_per_selection_replacement_is_atomic_and_replayable () =
  let document =
    document
      ~selections:[ selection 0 1; selection 2 4 ]
      "per-selection-replacement" "a bb"
  in
  let history = History.create document in
  let history =
    apply_intent history (Intent.Replace_selection_contents [ "bb"; "a" ])
  in
  expect_string ~expected:"bb a" ~actual:(text (History.current history));
  let change =
    match History.current_change history with
    | Some change -> change
    | None -> failf "per-selection replacement did not create a history change"
  in
  expect
    (List.length (Transaction.edits (History.transaction change)) = 2)
    "per-selection replacement did not create one edit per selection";
  expect
    (Transaction.intent (Transaction.metadata_of (History.transaction change))
    = Some "replace-selection-contents")
    "per-selection replacement did not retain its intent identity";
  let unchanged = History.create document in
  expect_error
    (History.apply_intent ~source:Transaction.Test unchanged
       (Intent.Replace_selection_contents [ "only-one" ]));
  expect_string ~expected:"a bb" ~actual:(text (History.current unchanged));
  let replay =
    Replay.create ~document_id:"per-selection-replacement-replay"
      ~contents:"a bb"
      ~initial_selections:
        { Replay.selections = [ selection 0 1; selection 2 4 ]; primary = 0 }
      ~actions:
        [ Replay.Intent (Intent.Replace_selection_contents [ "bb"; "a" ]) ]
    |> must
  in
  let replayed =
    Replay.to_string replay |> Replay.of_string |> must |> Replay.run |> must
  in
  expect_string ~expected:"bb a" ~actual:(text (History.current replayed))

let test_history_undo_redo_and_branches () =
  let history = History.create (document "history" "abc") in
  let history = apply_intent history (Intent.Insert_text "x") in
  let history = apply_intent history (Intent.Insert_text "y") in
  let second_change =
    match History.current_change history with
    | Some change -> History.change_id change
    | None -> failf "missing second change"
  in
  expect_string ~expected:"xyabc" ~actual:(text (History.current history));
  let undone = History.undo history |> must in
  expect_string ~expected:"xabc" ~actual:(text (History.current undone));
  let redone = History.redo undone |> must in
  expect_string ~expected:"xyabc" ~actual:(text (History.current redone));
  let branch_point = History.undo redone |> must in
  let branch = apply_intent branch_point (Intent.Insert_text "z") in
  expect_string ~expected:"xzabc" ~actual:(text (History.current branch));
  expect
    (Document_version.to_int (Document.version (History.current branch)) = 3)
    "branch commit did not allocate a unique next version";
  let original_branch =
    History.redo ~change_id:second_change branch_point |> must
  in
  expect_string ~expected:"xyabc"
    ~actual:(text (History.current original_branch))

let read_file path =
  let channel = open_in_bin path in
  Fun.protect
    ~finally:(fun () -> close_in_noerr channel)
    (fun () -> really_input_string channel (in_channel_length channel))

let fixture_path name =
  match Sys.getenv_opt "DUNE_SOURCEROOT" with
  | Some root -> Filename.concat root ("test/fixtures/" ^ name)
  | None -> Filename.concat "test/fixtures" name

let load_fixture name = Replay.of_string (read_file (fixture_path name)) |> must

let final_state replay =
  let history = Replay.run replay |> must in
  let snapshot = Document.snapshot (History.current history) in
  ( Document_snapshot.contents snapshot,
    selection_offsets (Document_snapshot.selections snapshot),
    Document_version.to_int (Document_snapshot.version snapshot),
    List.length (History.lineage history) )

let test_replay_fixtures_and_serialization () =
  let basic = load_fixture "basic.replay" in
  let basic_state = final_state basic in
  expect
    (basic_state = ("zenbu kernel!", [ (13, 13) ], 4, 4))
    "basic replay produced an unexpected final state";
  let unicode = load_fixture "unicode.replay" in
  expect
    (final_state unicode = ("é🦀 sushi", [ (6, 6) ], 3, 3))
    "unicode replay produced an unexpected final state";
  let multi = load_fixture "multiselection.replay" in
  expect
    (final_state multi = ("X X three", [ (1, 1); (3, 3) ], 1, 1))
    "multi-selection replay produced an unexpected final state";
  let encoded = Replay.to_string basic in
  let decoded = Replay.of_string encoded |> must in
  expect
    (final_state decoded = basic_state)
    "replay serialization changed semantics";
  expect_error
    (Replay.create ~document_id:"empty" ~contents:""
       ~initial_selections:{ Replay.selections = []; primary = 0 }
       ~actions:[]);
  expect_error (Replay.of_string "zenbu-replay-v1\ndocument=bad\n")

let random_text state length =
  String.init length (fun _ ->
      Char.chr (Char.code 'a' + Random.State.int state 26))

let expected_insertions source insertions =
  let buffer = Buffer.create (String.length source + 16) in
  for offset = 0 to String.length source do
    List.iter
      (fun (position, value) ->
        if position = offset then Buffer.add_string buffer value)
      insertions;
    if offset < String.length source then Buffer.add_char buffer source.[offset]
  done;
  Buffer.contents buffer

let test_properties () =
  let state = Random.State.make [| 0x5EED; 0xC0DE |] in
  for case = 1 to 250 do
    let source = random_text state (Random.State.int state 25) in
    let insertion_count = 1 + Random.State.int state 6 in
    let rec insertions count values =
      if count = 0 then List.rev values
      else
        let offset = Random.State.int state (String.length source + 1) in
        let value = random_text state (Random.State.int state 5) in
        insertions (count - 1) ((offset, value) :: values)
    in
    let insertions = insertions insertion_count [] in
    let document = document ("property-" ^ string_of_int case) source in
    let snapshot = Document.snapshot document in
    let edits =
      List.map (fun (offset, value) -> insert snapshot offset value) insertions
    in
    let transaction = transaction snapshot edits in
    let once = Document.apply document transaction |> must in
    let twice = Document.apply document transaction |> must in
    let expected = expected_insertions source insertions in
    expect_string ~expected ~actual:(text once);
    expect_string ~expected:(text once) ~actual:(text twice);
    expect
      (Document_version.to_int (Document.version once) = 1)
      "property case %d: version did not increment" case
  done;
  let replay = load_fixture "basic.replay" in
  expect
    (final_state replay = final_state replay)
    "replay was not deterministic"

let tests =
  [
    ( "document snapshots and UTF-8 boundaries",
      test_document_snapshots_and_unicode_boundaries );
    ("selection set invariants", test_selection_set_invariants);
    ( "transaction ordering and conflicts",
      test_transaction_ordering_and_conflicts );
    ( "rejection atomicity and version checks",
      test_rejection_is_atomic_and_versions_are_checked );
    ("semantic intent resolution", test_intents_resolve_to_transactions);
    ( "per-selection replacement is atomic and replayable",
      test_per_selection_replacement_is_atomic_and_replayable );
    ("history undo, redo, and branches", test_history_undo_redo_and_branches);
    ("replay fixtures and serialization", test_replay_fixtures_and_serialization);
    ("deterministic generated properties", test_properties);
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
