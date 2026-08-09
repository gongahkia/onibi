open Zenbu_kernel
open Zenbu_model_api
open Zenbu_syntax
open Zenbu_structural_model

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

let must_syntax = function
  | Ok value -> value
  | Error error -> failf "%s" (Syntax.Error.to_string error)

let document ?(selections = []) id contents =
  Document.create
    ~id:(Document_id.of_string id |> must)
    ~contents ~initial_selections:selections ()
  |> must

let text document = Document.snapshot document |> Document_snapshot.contents

let language () =
  match Syntax.Language.find "ocaml" with
  | Some language -> language
  | None -> failf "OCaml grammar is not registered"

let json_language () =
  match Syntax.Language.find "json" with
  | Some language -> language
  | None -> failf "JSON grammar is not registered"

let edit snapshot start_offset stop_offset replacement =
  Document_snapshot.range snapshot ~start_offset ~stop_offset |> must
  |> fun range -> Edit.replace range ~text:replacement |> must

let transaction snapshot edits =
  Transaction.create
    ~document_id:(Document_snapshot.document_id snapshot)
    ~source_version:(Document_snapshot.version snapshot)
    ~edits
    ~metadata:(Transaction.metadata ~source:Transaction.Test ())
    ()
  |> must

let rec shape node =
  let kind = Syntax.Snapshot.Node.kind node |> Syntax.Kind.to_string in
  let children =
    Syntax.Snapshot.Node.named_children node
    |> List.map shape |> String.concat ","
  in
  Printf.sprintf "%s@%d:%d:%b:%b[%s]" kind
    (Syntax.Snapshot.Node.start_offset node)
    (Syntax.Snapshot.Node.stop_offset node)
    (Syntax.Snapshot.Node.is_error node)
    (Syntax.Snapshot.Node.is_missing node)
    children

let rec assert_ranges snapshot parent =
  let range = Syntax.Snapshot.Node.range parent |> must_syntax in
  Document_snapshot.validate_range snapshot range |> must;
  let start = Anchor.byte_offset (Range.start range) in
  let stop = Anchor.byte_offset (Range.stop range) in
  List.iter
    (fun child ->
      let child_range = Syntax.Snapshot.Node.range child |> must_syntax in
      let child_start = Anchor.byte_offset (Range.start child_range) in
      let child_stop = Anchor.byte_offset (Range.stop child_range) in
      expect
        (start <= child_start && child_stop <= stop)
        "child syntax range escaped its parent";
      assert_ranges snapshot child)
    (Syntax.Snapshot.Node.named_children parent)

let incremental_case name source start_offset stop_offset replacement =
  let document = document ("incremental-" ^ name) source in
  let before = Document.snapshot document in
  let service = Syntax.Service.create (language ()) in
  let initial = Syntax.Service.refresh service before |> must_syntax in
  let tx =
    transaction before [ edit before start_offset stop_offset replacement ]
  in
  let after = Document.apply document tx |> must |> Document.snapshot in
  let incremental =
    Syntax.Service.update service ~before ~transaction:tx ~after |> must_syntax
  in
  let fresh_service = Syntax.Service.create (language ()) in
  let fresh = Syntax.Service.refresh fresh_service after |> must_syntax in
  expect_string
    ~expected:(shape (Syntax.Snapshot.root fresh))
    ~actual:(shape (Syntax.Snapshot.root incremental));
  expect
    (Syntax.Snapshot.document_version incremental
    = Document_version.to_int (Document_snapshot.version after))
    "incremental snapshot did not receive the resulting document version";
  assert_ranges after (Syntax.Snapshot.root incremental);
  expect
    (not (Syntax.Snapshot.matches_document initial after))
    "a previous syntax snapshot matched a newer document"

let incremental_edits_case () =
  let source = "let alpha = 1\nlet beta = alpha + 2\n" in
  let document = document "incremental-multiple" source in
  let before = Document.snapshot document in
  let service = Syntax.Service.create (language ()) in
  ignore (Syntax.Service.refresh service before |> must_syntax);
  let newline = String.index source '\n' in
  let tx =
    transaction before
      [
        edit before 0 0 "(* header *)\n";
        edit before newline (newline + 1) " ";
        edit before 12 13 "0";
      ]
  in
  let after = Document.apply document tx |> must |> Document.snapshot in
  let incremental =
    Syntax.Service.update service ~before ~transaction:tx ~after |> must_syntax
  in
  let fresh =
    Syntax.Service.refresh (Syntax.Service.create (language ())) after
    |> must_syntax
  in
  expect_string
    ~expected:(shape (Syntax.Snapshot.root fresh))
    ~actual:(shape (Syntax.Snapshot.root incremental))

let test_incremental_full_equivalence () =
  let source = "let alpha = 1\nlet beta = alpha + 2\n" in
  incremental_case "insertion" source 12 12 "0";
  incremental_case "deletion" source 12 13 "";
  incremental_case "replacement" source 4 9 "gamma";
  incremental_case "newline" source 13 13 "\n  + 3";
  let newline = String.index source '\n' in
  incremental_case "newline-deletion" source newline (newline + 1) "";
  incremental_case "boundary" source 0 0 "(* comment *)\n";
  incremental_case "invalid" source 12 13 "(";
  incremental_case "repair" "let alpha = (\n" 12 13 "1)";
  incremental_edits_case ()

let test_repeated_incremental_edits_performance () =
  let source =
    List.init 512 (fun index -> Printf.sprintf "let item_%d = %d\n" index index)
    |> String.concat ""
  in
  let document = document "incremental-performance" source in
  let service = Syntax.Service.create (language ()) in
  let document = ref document in
  let before = ref (Document.snapshot !document) in
  ignore (Syntax.Service.refresh service !before |> must_syntax);
  let started = Sys.time () in
  for _ = 1 to 96 do
    let tx = transaction !before [ edit !before 0 0 " " ] in
    document := Document.apply !document tx |> must;
    let after = Document.snapshot !document in
    ignore
      (Syntax.Service.update service ~before:!before ~transaction:tx ~after
      |> must_syntax);
    before := after
  done;
  let elapsed = Sys.time () -. started in
  expect (elapsed < 5.)
    "96 incremental edits of a 512-declaration source took %.3fs" elapsed

let test_invalid_syntax_and_registry () =
  let document = document "invalid" "let x =\nlet y = [" in
  let snapshot = Document.snapshot document in
  let service = Syntax.Service.create (language ()) in
  let syntax = Syntax.Service.refresh service snapshot |> must_syntax in
  ignore (shape (Syntax.Snapshot.root syntax));
  assert_ranges snapshot (Syntax.Snapshot.root syntax);
  expect
    (Option.is_some (Syntax.Language.detect_path "example.ml")
    && Option.is_some (Syntax.Language.detect_path "fixture.json")
    && Option.is_none (Syntax.Language.detect_path "notes.txt"))
    "language detection did not distinguish known and plain-text extensions";
  let stale_context =
    Editor_context.from_snapshot ~snapshot ~commands:[] ~syntax ()
  in
  expect
    (Option.is_some (Editor_context.syntax stale_context))
    "a matching syntax snapshot was not exposed through the model context";
  let updated =
    Document.apply document
      (transaction snapshot [ edit snapshot 0 0 "(* still invalid *)\n" ])
    |> must |> Document.snapshot
  in
  let context =
    Editor_context.from_snapshot ~snapshot:updated ~commands:[] ~syntax ()
  in
  expect
    (Option.is_none (Editor_context.syntax context))
    "a stale syntax snapshot reached an editor context"

let test_json_syntax () =
  let document = document "json" {|{"items": [1, 2, 3], "ok": true}|} in
  let snapshot = Document.snapshot document in
  let syntax =
    Syntax.Service.refresh (Syntax.Service.create (json_language ())) snapshot
    |> must_syntax
  in
  expect
    (not (Syntax.Snapshot.has_error syntax))
    "registered JSON grammar did not parse a valid JSON fixture";
  assert_ranges snapshot (Syntax.Snapshot.root syntax)

let structural_runtime contents =
  let service = Syntax.Service.create (language ()) in
  let module Runtime = Model_runtime.Make (Structural_model) in
  Runtime.create ~syntax_service:service
    ~document:(document "structural-model" contents)
    ()
  |> must

let key text = Input_event.key_press (Input_event.logical_text text |> must)
let named value = Input_event.key_press (Input_event.named_key value)
let text_input text = Input_event.text_input text |> must

let selection_offsets history =
  Document.snapshot (History.current history)
  |> Document_snapshot.selections |> Selection_set.to_list
  |> List.map (fun selection ->
      ( Anchor.byte_offset (Selection.anchor selection),
        Anchor.byte_offset (Selection.head selection) ))

let test_structural_model_and_shared_effects () =
  let module Runtime = Model_runtime.Make (Structural_model) in
  let runtime = structural_runtime "let alpha = 1\nlet beta = alpha\n" in
  let runtime, focus = Runtime.handle_input runtime (key "f") |> must in
  expect
    (List.map Model_intent.identity (Runtime.intents focus)
    = [ "set-selections" ])
    "focus did not produce an ordinary selection intent";
  let runtime, parent =
    Runtime.handle_input runtime (named Input_event.Arrow_up) |> must
  in
  expect
    (List.map Model_intent.identity (Runtime.intents parent)
    = [ "set-selections" ])
    "parent navigation did not produce an ordinary selection intent";
  let runtime, child =
    Runtime.handle_input runtime (named Input_event.Arrow_down) |> must
  in
  expect
    (List.map Model_intent.identity (Runtime.intents child)
    = [ "set-selections" ])
    "child navigation did not produce an ordinary selection intent";
  let child_offsets = selection_offsets (Runtime.history runtime) in
  let runtime, _ = Runtime.handle_input runtime (key "e") |> must in
  let runtime, _ = Runtime.handle_input runtime (key "r") |> must in
  expect
    (selection_offsets (Runtime.history runtime) = child_offsets)
    "structural shrink did not restore the previous expanded selection";
  let runtime, sibling =
    Runtime.handle_input runtime (named Input_event.Arrow_right) |> must
  in
  expect
    (List.map Model_intent.identity (Runtime.intents sibling)
    = [ "set-selections" ])
    "sibling navigation did not produce an ordinary selection intent";
  let runtime, multiple = Runtime.handle_input runtime (key "m") |> must in
  let snapshot =
    Document.snapshot (History.current (Runtime.history runtime))
  in
  expect
    (List.length (Selection_set.to_list (Document_snapshot.selections snapshot))
    = 2)
    "same-kind structural selection did not use two ordinary selections";
  expect
    (List.map Model_intent.identity (Runtime.intents multiple)
    = [ "set-selections" ])
    "same-kind selection bypassed the semantic selection path";
  let runtime, deleted = Runtime.handle_input runtime (key "x") |> must in
  expect
    (List.map Model_intent.identity (Runtime.intents deleted)
    = [ "delete-selected-ranges" ])
    "structural delete did not use the shared delete intent";
  expect_string ~expected:"\n\n"
    ~actual:(text (History.current (Runtime.history runtime)));
  let runtime, _ = Runtime.handle_input runtime (key "u") |> must in
  expect_string ~expected:"let alpha = 1\nlet beta = alpha\n"
    ~actual:(text (History.current (Runtime.history runtime)));
  let runtime, _ = Runtime.handle_input runtime (key "f") |> must in
  let runtime, _ = Runtime.handle_input runtime (key "c") |> must in
  expect
    (Model_status.input_mode (Runtime.status runtime) = Model_status.Text_entry)
    "structural change did not enter shared text input mode";
  let runtime, _ =
    Runtime.handle_input runtime (text_input "let repaired =") |> must
  in
  let runtime, _ =
    Runtime.handle_input runtime (named Input_event.Escape) |> must
  in
  let context = Runtime.context runtime in
  expect
    (match Editor_context.syntax context with
    | Some syntax -> Syntax.Snapshot.has_error syntax
    | None -> false)
    "temporarily invalid structural text did not remain parseable"

let test_structural_model_without_syntax () =
  let module Runtime = Model_runtime.Make (Structural_model) in
  let runtime =
    Runtime.create ~document:(document "plain" "plain text") () |> must
  in
  expect
    (Model_status.id (Runtime.status runtime) = "struct-no-syntax")
    "structural model did not report missing syntax";
  let runtime, step = Runtime.handle_input runtime (key "f") |> must in
  expect
    (Runtime.intents step = [])
    "structural focus invented a selection without syntax";
  expect_string ~expected:"plain text"
    ~actual:(text (History.current (Runtime.history runtime)))

let test_syntax_commands_are_described () =
  let ids =
    Syntax_commands.commands ()
    |> List.map (fun command ->
        Command.descriptor command |> Command_descriptor.id
        |> Command_id.to_string)
  in
  expect
    (ids
    = [
        "syntax.focus";
        "syntax.parent";
        "syntax.child";
        "syntax.next-sibling";
        "syntax.previous-sibling";
        "syntax.expand";
        "syntax.select-same-kind";
      ])
    "generic syntax command descriptors changed unexpectedly"

let run name test =
  try
    test ();
    Printf.printf "ok - %s\n" name
  with
  | Test_failure message ->
      Printf.eprintf "not ok - %s: %s\n" name message;
      exit 1
  | exception_ ->
      Printf.eprintf "not ok - %s: unexpected %s\n" name
        (Printexc.to_string exception_);
      exit 1

let () =
  [
    ("incremental/full syntax equivalence", test_incremental_full_equivalence);
    ( "repeated incremental syntax performance",
      test_repeated_incremental_edits_performance );
    ("invalid syntax and version safety", test_invalid_syntax_and_registry);
    ("registered JSON syntax", test_json_syntax);
    ("structural model shared effects", test_structural_model_and_shared_effects);
    ("structural model without syntax", test_structural_model_without_syntax);
    ("syntax command descriptors", test_syntax_commands_are_described);
  ]
  |> List.iter (fun (name, test) -> run name test)
