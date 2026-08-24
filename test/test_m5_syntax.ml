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

let must_query = function
  | Ok value -> value
  | Error error -> failf "%s" (Syntax.Query.error_to_string error)

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

module Grammar = Syntax.Grammar

let grammar_candidate ?source ?version ?abi ?integrity ~id ~display_name
    ~extensions ~bundle () =
  let source = Option.value source ~default:(Grammar.Bundle.source bundle) in
  let version = Option.value version ~default:(Grammar.Bundle.version bundle) in
  let abi = Option.value abi ~default:(Grammar.Bundle.abi bundle) in
  let integrity =
    Option.value integrity ~default:(Grammar.Bundle.integrity bundle)
  in
  Grammar.Candidate.create ~id ~display_name ~extensions ~source ~version ~abi
    ~integrity ~bundle

let builtin_grammar_candidates () =
  [
    grammar_candidate ~id:"ocaml" ~display_name:"OCaml"
      ~extensions:[ ".ml"; ".mli" ] ~bundle:Grammar.Bundle.Ocaml ();
    grammar_candidate ~id:"json" ~display_name:"JSON" ~extensions:[ ".json" ]
      ~bundle:Grammar.Bundle.Json ();
  ]

let registry_error result fragment =
  match result with
  | Ok _ -> failf "grammar registry accepted an invalid candidate"
  | Error error ->
      let message = Grammar.Registry.error_to_string error in
      expect
        (String.contains message fragment.[0]
        &&
        let rec contains offset =
          offset + String.length fragment <= String.length message
          && (String.sub message offset (String.length fragment) = fragment
             || contains (offset + 1))
        in
        contains 0)
        "grammar registry error %S did not contain %S" message fragment

let test_runtime_grammar_registry () =
  let original = builtin_grammar_candidates () in
  Fun.protect
    ~finally:(fun () -> ignore (Grammar.Registry.reload original))
    (fun () ->
      expect
        (String.equal
           (Grammar.Bundle.integrity Grammar.Bundle.Ocaml)
           "sha256:bb0e6d149a96a3815bf6e6a9b6e54e032cadd5575bc040efc48d741098024805"
        && String.equal
             (Grammar.Bundle.integrity Grammar.Bundle.Json)
             "sha256:412cb7f25022ca6e0db57bbbe56754f6aa7e1d5dd0dab173b5bd38b17ff2180c"
        )
        "grammar manifest integrity attestations changed unexpectedly";
      let registry =
        Grammar.Registry.stage original |> function
        | Ok registry -> registry
        | Error error -> failf "%s" (Grammar.Registry.error_to_string error)
      in
      let languages =
        Grammar.Registry.languages registry |> List.map Syntax.Language.id
      in
      expect
        (languages = [ "json"; "ocaml" ])
        "staged grammar registry did not sort language mappings \
         deterministically";
      expect
        (Option.is_none (Grammar.Registry.detect_path registry "notes.unknown"))
        "unknown extension did not retain plain-text fallback";
      let ocaml =
        match Grammar.Registry.find registry "ocaml" with
        | Some language -> language
        | None -> failf "staged OCaml grammar is missing"
      in
      (match Grammar.source ocaml with
      | Grammar.Source.Built_in { package; revision } ->
          expect
            (String.equal package "tree-sitter.ocaml"
            && String.equal revision "0.1.0"
            && String.equal (Grammar.version ocaml) "0.1.0"
            && Grammar.abi ocaml = 15
            && String.starts_with ~prefix:"sha256:" (Grammar.integrity ocaml))
            "grammar provenance, version, ABI, or integrity is not explicit");
      let incompatible =
        grammar_candidate ~id:"bad-abi" ~display_name:"Bad ABI"
          ~extensions:[ ".badabi" ] ~abi:16 ~bundle:Grammar.Bundle.Json ()
      in
      registry_error (Grammar.Registry.stage [ incompatible ]) "incompatible";
      let untrusted_source =
        grammar_candidate ~id:"untrusted" ~display_name:"Untrusted"
          ~extensions:[ ".untrusted" ]
          ~source:
            (Grammar.Source.Built_in
               { package = "native-path:/tmp/grammar.so"; revision = "0" })
          ~bundle:Grammar.Bundle.Json ()
      in
      registry_error (Grammar.Registry.stage [ untrusted_source ]) "source";
      let malformed_integrity =
        grammar_candidate ~id:"bad-integrity" ~display_name:"Bad integrity"
          ~extensions:[ ".badintegrity" ]
          ~integrity:("sha256:" ^ String.make 64 '0')
          ~bundle:Grammar.Bundle.Json ()
      in
      registry_error
        (Grammar.Registry.stage [ malformed_integrity ])
        "integrity";
      let duplicate_extensions =
        [
          grammar_candidate ~id:"json-one" ~display_name:"JSON one"
            ~extensions:[ ".fixture" ] ~bundle:Grammar.Bundle.Json ();
          grammar_candidate ~id:"json-two" ~display_name:"JSON two"
            ~extensions:[ ".fixture" ] ~bundle:Grammar.Bundle.Json ();
        ]
      in
      registry_error (Grammar.Registry.stage duplicate_extensions) "duplicate";
      let too_many =
        List.init (Grammar.maximum_registered_grammars + 1) (fun index ->
            grammar_candidate
              ~id:("json-" ^ string_of_int index)
              ~display_name:("JSON " ^ string_of_int index)
              ~extensions:[ ".unused" ^ string_of_int index ]
              ~bundle:Grammar.Bundle.Json ())
      in
      registry_error (Grammar.Registry.stage too_many) "limit";
      let old_service = Syntax.Service.create ocaml in
      let old_document = document "registry-retention" "let old = true\n" in
      ignore
        (Syntax.Service.refresh old_service (Document.snapshot old_document)
        |> must_syntax);
      let replacement =
        [
          grammar_candidate ~id:"fixture-json" ~display_name:"Fixture JSON"
            ~extensions:[ ".fixture" ] ~bundle:Grammar.Bundle.Json ();
        ]
      in
      ignore
        ( Grammar.Registry.reload replacement |> function
          | Ok registry -> registry
          | Error error -> failf "%s" (Grammar.Registry.error_to_string error)
        );
      expect
        (Option.is_none (Syntax.Language.find "ocaml")
        && Option.is_some (Syntax.Language.detect_path "sample.fixture"))
        "accepted registry reload did not replace future language mapping";
      ignore
        (Syntax.Service.refresh old_service (Document.snapshot old_document)
        |> must_syntax);
      ignore
        ( Grammar.Registry.reload original |> function
          | Ok registry -> registry
          | Error error -> failf "%s" (Grammar.Registry.error_to_string error)
        );
      registry_error
        (Grammar.Registry.reload [ malformed_integrity ])
        "integrity";
      expect
        (Option.is_some (Syntax.Language.find "ocaml")
        && Option.is_none (Syntax.Language.detect_path "notes.unknown"))
        "rejected registry reload replaced active language mappings";
      ignore
        (Syntax.Service.refresh old_service (Document.snapshot old_document)
        |> must_syntax))

let test_syntax_source_limit () =
  let oversized = String.make (Syntax.Service.maximum_source_bytes + 1) 'x' in
  let document = document "syntax-source-limit" oversized in
  let service = Syntax.Service.create (language ()) in
  match Syntax.Service.refresh service (Document.snapshot document) with
  | Error (Syntax.Error.Backend_failure message) ->
      expect
        (String.starts_with ~prefix:"syntax source exceeds" message)
        "source-size rejection did not preserve its resource-limit reason"
  | Error error ->
      failf "unexpected syntax source-limit error: %s"
        (Syntax.Error.to_string error)
  | Ok _ -> failf "syntax parser accepted an oversized source"

let capture_offsets captures =
  List.map
    (fun capture ->
      let range = Syntax.Query.capture_range capture in
      ( Syntax.Query.capture_name capture,
        Anchor.byte_offset (Range.start range),
        Anchor.byte_offset (Range.stop range) ))
    captures

let selection_offsets_of_set selections =
  Selection_set.to_list selections
  |> List.map (fun selection ->
      ( Anchor.byte_offset (Selection.anchor selection),
        Anchor.byte_offset (Selection.head selection) ))

let query_source = "(value_name) @name"

let test_bounded_syntax_queries () =
  let source = "let café = 1\nlet beta = café\n" in
  let query_document = document "syntax-query" source in
  let snapshot = Document.snapshot query_document in
  let syntax =
    Syntax.Service.refresh (Syntax.Service.create (language ())) snapshot
    |> must_syntax
  in
  let query = Syntax.Query.compile syntax ~source:query_source |> must_query in
  expect
    (Syntax.Query.document_id query = "syntax-query"
    && Syntax.Query.document_version query = 0
    && Syntax.Query.language_id query = "ocaml"
    && Syntax.Query.language_version query = "0.1.0")
    "syntax query did not retain explicit document and grammar bindings";
  let captures = Syntax.Query.captures query ~snapshot:syntax |> must_query in
  expect
    (capture_offsets captures
    = [ ("name", 4, 9); ("name", 18, 22); ("name", 25, 30) ])
    "syntax query captures were not stable across Unicode source boundaries";
  List.iter
    (fun capture ->
      Document_snapshot.validate_range snapshot
        (Syntax.Query.capture_range capture)
      |> must)
    captures;
  let selections =
    Syntax.Query.selections query ~snapshot:syntax ~capture:"name" |> must_query
  in
  expect
    (selection_offsets_of_set selections = [ (4, 9); (18, 22); (25, 30) ])
    "query capture selections did not preserve validated byte ranges";
  let context =
    Editor_context.from_snapshot ~snapshot ~commands:[] ~syntax ()
  in
  let intent =
    match
      Syntax_commands.resolve_query context ~source:query_source ~capture:"name"
    with
    | Ok [ intent ] -> intent
    | Ok _ -> failf "query command did not produce exactly one selection intent"
    | Error error -> failf "%s" (Error.to_string error)
  in
  expect
    (Model_intent.identity intent = "set-selections")
    "query command bypassed the ordinary selection intent";
  let query_command =
    Syntax_commands.commands ()
    |> List.find (fun command ->
        String.equal
          (Command.descriptor command |> Command_descriptor.id
         |> Command_id.to_string)
          "syntax.query.select")
  in
  let command_intents =
    Command.execute query_command context
      (Syntax_commands.query_invocation ~source:query_source ~capture:"name"
      |> must)
    |> must
  in
  expect
    (List.map Model_intent.identity command_intents = [ "set-selections" ])
    "registered syntax query command did not use the normal selection path";
  let selection_transaction =
    Intent.resolve ~source:Transaction.Test snapshot
      (Model_intent.to_kernel intent)
    |> must
  in
  let selected = Document.apply query_document selection_transaction |> must in
  expect
    (selection_offsets_of_set
       (Document_snapshot.selections (Document.snapshot selected))
    = [ (4, 9); (18, 22); (25, 30) ])
    "query selection intent did not commit as a normal selection transaction";
  let modified =
    Document.apply query_document
      (transaction snapshot [ edit snapshot 0 0 "(* newer *)\n" ])
    |> must
  in
  expect
    (Result.is_error (Document.apply modified selection_transaction))
    "a query selection transaction applied after its source snapshot changed";
  let initial_selection =
    Selection_spec.make ~anchor_offset:0 ~head_offset:0 |> must
  in
  let replay =
    Replay.create ~document_id:"syntax-query-replay" ~contents:source
      ~initial_selections:
        { Replay.selections = [ initial_selection ]; primary = 0 }
      ~actions:[ Replay.Intent (Model_intent.to_kernel intent) ]
    |> must
  in
  let replayed =
    Replay.to_string replay |> Replay.of_string |> must |> Replay.run |> must
  in
  expect
    (selection_offsets_of_set
       (Document_snapshot.selections
          (Document.snapshot (History.current replayed)))
    = [ (4, 9); (18, 22); (25, 30) ])
    "query selections did not replay as ordinary kernel intent data";
  (match Syntax.Query.compile syntax ~source:"(" with
  | Error (Syntax.Query.Invalid_query _) -> ()
  | Error error ->
      failf "unexpected invalid-query error: %s"
        (Syntax.Query.error_to_string error)
  | Ok _ -> failf "syntax query accepted malformed query text");
  (match Syntax.Query.compile syntax ~source:"\255" with
  | Error (Syntax.Query.Invalid_query _) -> ()
  | Error error ->
      failf "unexpected invalid-UTF-8 query error: %s"
        (Syntax.Query.error_to_string error)
  | Ok _ -> failf "syntax query accepted invalid UTF-8 text");
  (match
     Syntax.Query.compile syntax
       ~source:(String.make (Syntax.Query.maximum_query_bytes + 1) 'x')
   with
  | Error (Syntax.Query.Query_too_large _) -> ()
  | Error error ->
      failf "unexpected oversized-query error: %s"
        (Syntax.Query.error_to_string error)
  | Ok _ -> failf "syntax query accepted oversized source text");
  let too_many_patterns =
    List.init (Syntax.Query.maximum_patterns + 1) (fun _ -> query_source)
    |> String.concat "\n"
  in
  (match Syntax.Query.compile syntax ~source:too_many_patterns with
  | Error (Syntax.Query.Query_too_complex _) -> ()
  | Error error ->
      failf "unexpected complex-query error: %s"
        (Syntax.Query.error_to_string error)
  | Ok _ -> failf "syntax query accepted too many patterns");
  let error_document = document "syntax-query-error" "let alpha =" in
  let error_snapshot = Document.snapshot error_document in
  let error_syntax =
    Syntax.Service.refresh (Syntax.Service.create (language ())) error_snapshot
    |> must_syntax
  in
  expect
    (Syntax.Snapshot.has_error error_syntax)
    "invalid query fixture did not produce a parser error";
  (match Syntax.Query.compile error_syntax ~source:query_source with
  | Error Syntax.Query.Snapshot_has_parse_error -> ()
  | Error error ->
      failf "unexpected parse-error query rejection: %s"
        (Syntax.Query.error_to_string error)
  | Ok _ -> failf "syntax query accepted a parser-error snapshot");
  let stale_document = document "syntax-query-stale" "let alpha = 1\n" in
  let before = Document.snapshot stale_document in
  let stale_service = Syntax.Service.create (language ()) in
  let before_syntax =
    Syntax.Service.refresh stale_service before |> must_syntax
  in
  let stale_query =
    Syntax.Query.compile before_syntax ~source:query_source |> must_query
  in
  let after =
    Document.apply stale_document
      (transaction before [ edit before 0 0 "(* newer *)\n" ])
    |> must |> Document.snapshot
  in
  let after_syntax =
    Syntax.Service.refresh stale_service after |> must_syntax
  in
  (match Syntax.Query.captures stale_query ~snapshot:after_syntax with
  | Error (Syntax.Query.Stale_snapshot _) -> ()
  | Error error ->
      failf "unexpected stale-query error: %s"
        (Syntax.Query.error_to_string error)
  | Ok _ -> failf "syntax query ran against a newer snapshot");
  let language_document = document "syntax-query-language" "{}" in
  let language_snapshot = Document.snapshot language_document in
  let json_syntax =
    Syntax.Service.refresh
      (Syntax.Service.create (json_language ()))
      language_snapshot
    |> must_syntax
  in
  let ocaml_syntax =
    Syntax.Service.refresh
      (Syntax.Service.create (language ()))
      language_snapshot
    |> must_syntax
  in
  let language_query =
    Syntax.Query.compile json_syntax ~source:"(_) @node" |> must_query
  in
  (match Syntax.Query.captures language_query ~snapshot:ocaml_syntax with
  | Error (Syntax.Query.Wrong_language _) -> ()
  | Error error ->
      failf "unexpected language-bound query error: %s"
        (Syntax.Query.error_to_string error)
  | Ok _ -> failf "syntax query ran under a different language grammar");
  let many_source =
    List.init (Syntax.Query.maximum_captures + 1) (fun index ->
        Printf.sprintf "let item_%d = %d\n" index index)
    |> String.concat ""
  in
  let many_document = document "syntax-query-limit" many_source in
  let many_snapshot = Document.snapshot many_document in
  let many_syntax =
    Syntax.Service.refresh (Syntax.Service.create (language ())) many_snapshot
    |> must_syntax
  in
  let many_query =
    Syntax.Query.compile many_syntax ~source:query_source |> must_query
  in
  match Syntax.Query.captures many_query ~snapshot:many_syntax with
  | Error (Syntax.Query.Result_limit_exceeded _) -> ()
  | Error error ->
      failf "unexpected result-limit error: %s"
        (Syntax.Query.error_to_string error)
  | Ok _ -> failf "syntax query accepted an oversized capture result"

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
        "syntax.query.select";
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
    ("runtime grammar registry", test_runtime_grammar_registry);
    ("syntax source resource limit", test_syntax_source_limit);
    ("bounded syntax queries", test_bounded_syntax_queries);
    ("structural model shared effects", test_structural_model_and_shared_effects);
    ("structural model without syntax", test_structural_model_without_syntax);
    ("syntax command descriptors", test_syntax_commands_are_described);
  ]
  |> List.iter (fun (name, test) -> run name test)
