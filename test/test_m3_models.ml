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

let document id contents =
  Document.create ~id:(Document_id.of_string id |> must) ~contents () |> must

let text history =
  History.current history |> Document.snapshot |> Document_snapshot.contents

let offsets history =
  History.current history |> Document.snapshot |> Document_snapshot.selections
  |> Selection_set.to_list
  |> List.map (fun selection ->
      ( Anchor.byte_offset (Selection.anchor selection),
        Anchor.byte_offset (Selection.head selection) ))

let key text = Input_event.key_press (Input_event.logical_text text |> must)
let named value = Input_event.key_press (Input_event.named_key value)

let control text =
  Input_event.key_press ~modifiers:[ Input_event.Control ]
    (Input_event.logical_text text |> must)

let alt text =
  Input_event.key_press ~modifiers:[ Input_event.Alt ]
    (Input_event.logical_text text |> must)

let committed text = Input_event.text_input text |> must

let registry () =
  List.fold_left
    (fun registry command -> Command_registry.register registry command |> must)
    Command_registry.empty
    (Semantic_commands.apply_command :: Semantic_commands.selection_commands)

let history_with_selections id contents selection_offsets ~primary =
  let initial_selections =
    List.map
      (fun (anchor_offset, head_offset) ->
        Selection_spec.make ~anchor_offset ~head_offset |> must)
      selection_offsets
  in
  let history =
    Document.create
      ~id:(Document_id.of_string id |> must)
      ~contents ~initial_selections ()
    |> must |> History.create
  in
  Model_intent.set_selections ~selections:selection_offsets ~primary
  |> must |> Model_intent.to_kernel
  |> History.apply_intent ~source:Transaction.Test history
  |> must

let context history =
  let snapshot = History.current history |> Document.snapshot in
  Editor_context.from_snapshot ~snapshot ~commands:[] ()

let apply_selection_intent history intent =
  History.apply_intent ~source:Transaction.Test history
    (Model_intent.to_kernel intent)
  |> must

module Vim_runtime = Model_runtime.Make (Vim_model)
module Selection_runtime = Model_runtime.Make (Selection_model)

let vim contents =
  Vim_runtime.create ~commands:(registry ()) ~document:(document "vim" contents)
    ()
  |> must

let selection contents =
  Selection_runtime.create ~commands:(registry ())
    ~document:(document "selection" contents)
    ()
  |> must

let send_vim runtime input = Vim_runtime.handle_input runtime input |> must

let send_selection runtime input =
  Selection_runtime.handle_input runtime input |> must

let fold_vim runtime inputs =
  List.fold_left
    (fun runtime input -> fst (send_vim runtime input))
    runtime inputs

let fold_selection runtime inputs =
  List.fold_left
    (fun runtime input -> fst (send_selection runtime input))
    runtime inputs

let test_shared_word_and_line_selectors () =
  let command_id =
    Command.descriptor Semantic_commands.apply_command
    |> Command_descriptor.id |> Command_id.to_string
  in
  expect
    (String.equal command_id "editor.apply")
    "M3 semantic command id was not stable";
  let history = History.create (document "selector" "alpha,  café\nsecond") in
  let next_word =
    Model_intent.apply ~selector:Model_intent.Next_word
      ~transformation:Model_intent.Select
  in
  let history =
    History.apply_intent ~source:Transaction.Test history
      (Model_intent.to_kernel next_word)
    |> must
  in
  expect
    (offsets history = [ (0, 5) ])
    "next-word should stop before punctuation: got %s"
    (offsets history
    |> List.map (fun (anchor, head) -> Printf.sprintf "%d:%d" anchor head)
    |> String.concat ",");
  let current_line =
    Model_intent.apply ~selector:Model_intent.Current_line
      ~transformation:Model_intent.Select
  in
  let history =
    History.apply_intent ~source:Transaction.Test history
      (Model_intent.to_kernel current_line)
    |> must
  in
  expect
    (offsets history = [ (0, 14) ])
    "current-line should include its newline";
  let unicode = History.create (document "unicode" "é界🙂") in
  let unicode =
    History.apply_intent ~source:Transaction.Test unicode
      (Model_intent.to_kernel
         (Model_intent.apply ~selector:Model_intent.Next_text_unit
            ~transformation:Model_intent.Collapse_to_end))
    |> must
  in
  expect
    (offsets unicode = [ (2, 2) ])
    "Unicode scalar movement split a code point";
  let touching =
    Document.create
      ~id:(Document_id.of_string "touching" |> must)
      ~contents:"aaaa"
      ~initial_selections:
        [ Selection_spec.make ~anchor_offset:0 ~head_offset:2 |> must ]
      ()
    |> must
  in
  let touching = History.create touching in
  let touching =
    History.apply_intent ~source:Transaction.Test touching
      (Model_intent.to_kernel
         (Model_intent.apply ~selector:Model_intent.All_occurrences
            ~transformation:Model_intent.Select))
    |> must
  in
  let touching =
    History.apply_intent ~source:Transaction.Test touching
      (Model_intent.to_kernel
         (Model_intent.apply ~selector:Model_intent.Current_selections
            ~transformation:Model_intent.Delete))
    |> must
  in
  expect_string ~expected:"" ~actual:(text touching)

let test_vim_pending_counts_and_cancellation () =
  let runtime = vim "a b c d" in
  let runtime, counted = send_vim runtime (key "3") in
  expect
    (Model_status.metadata (Vim_runtime.status_after counted)
    = [ ("clipboard-slot", "unnamed"); ("count", "3") ])
    "count state was not exposed through status";
  let runtime, pending = send_vim runtime (key "d") in
  expect
    (Model_status.id (Vim_runtime.status_after pending) = "operator-pending")
    "d did not enter operator-pending state";
  let runtime, deleted = send_vim runtime (key "w") in
  expect_string ~expected:"d" ~actual:(text (Vim_runtime.history runtime));
  expect
    (List.length (Vim_runtime.intents deleted) = 3)
    "3dw did not compile to three semantic operations";
  expect
    (List.length (Vim_runtime.input_trace runtime) = 3)
    "input trace did not retain count/operator/motion events";
  let runtime, _ = send_vim runtime (key "l") in
  expect
    (offsets (Vim_runtime.history runtime) = [ (1, 1) ])
    "completed commands did not reset the count before the next motion";
  let runtime = vim "alpha" in
  let runtime =
    fold_vim runtime [ key "2"; key "d"; named Input_event.Escape ]
  in
  expect_string ~expected:"alpha" ~actual:(text (Vim_runtime.history runtime));
  expect
    (Model_status.id (Vim_runtime.status runtime) = "normal")
    "Escape did not clear pending grammar";
  let runtime = vim "a b c d e f g" in
  let runtime = fold_vim runtime [ key "3"; key "d"; key "2"; key "w" ] in
  expect_string ~expected:"g" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "a b c d e f g h i j k l m" in
  let runtime, _ = send_vim runtime (key "1") in
  let _, status = send_vim runtime (key "2") in
  expect
    (List.mem ("count", "12")
       (Model_status.metadata (Vim_runtime.status_after status)))
    "multiple count digits were not retained"

let test_vim_insert_change_and_character_edits () =
  let runtime = vim "" in
  let runtime =
    fold_vim runtime
      [
        key "i";
        committed "é界🙂";
        named Input_event.Backspace;
        named Input_event.Enter;
        named Input_event.Escape;
      ]
  in
  expect_string ~expected:"é界\n" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha beta" in
  let runtime =
    fold_vim runtime
      [ key "c"; key "w"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"Xbeta" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "é🙂" in
  let runtime = fold_vim runtime [ key "x" ] in
  expect_string ~expected:"🙂" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "abc" in
  let runtime =
    fold_vim runtime [ key "a"; committed "界"; named Input_event.Escape ]
  in
  expect_string ~expected:"a界bc" ~actual:(text (Vim_runtime.history runtime))

let test_vim_linewise_register_history_and_repeat () =
  let runtime = vim "one\ntwo\n" in
  let runtime =
    fold_vim runtime
      [ key "\""; key "a"; key "y"; key "y"; key "\""; key "a"; key "p" ]
  in
  expect_string ~expected:"one\none\ntwo\n"
    ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "abc" in
  let runtime = fold_vim runtime [ key "x"; key "u" ] in
  expect_string ~expected:"abc" ~actual:(text (Vim_runtime.history runtime));
  let runtime = fst (send_vim runtime (control "r")) in
  expect_string ~expected:"bc" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha beta gamma" in
  let runtime = fold_vim runtime [ key "d"; key "w"; key "." ] in
  expect_string ~expected:"gamma" ~actual:(text (Vim_runtime.history runtime))

let test_vim_text_objects_and_line_counts () =
  let runtime = vim "foo bar" in
  let runtime = fold_vim runtime [ key "d"; key "i"; key "w" ] in
  expect_string ~expected:" bar" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "foo bar" in
  let runtime = fold_vim runtime [ key "d"; key "a"; key "w" ] in
  expect_string ~expected:"bar" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "one\ntwo\nthree" in
  let runtime = fold_vim runtime [ key "2"; key "d"; key "d" ] in
  expect_string ~expected:"three" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "  alpha" in
  let runtime = fold_vim runtime [ key "^"; key "x" ] in
  expect_string ~expected:"  lpha" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "one\ntwo" in
  let runtime =
    fold_vim runtime
      [ key "c"; key "c"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"X\ntwo" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha beta" in
  let runtime = fold_vim runtime [ key "y"; key "w"; key "P" ] in
  expect_string ~expected:"alpha alpha beta"
    ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "éx\n界\nz" in
  let runtime = fold_vim runtime [ key "j" ] in
  expect
    (offsets (Vim_runtime.history runtime) = [ (4, 4) ])
    "j did not retain a Unicode-scalar column";
  let runtime = fold_vim runtime [ key "k" ] in
  expect
    (offsets (Vim_runtime.history runtime) = [ (0, 0) ])
    "k did not return to the preceding line";
  let runtime = vim "a\nb" in
  let runtime = fold_vim runtime [ key "G"; key "g"; key "g" ] in
  expect
    (offsets (Vim_runtime.history runtime) = [ (0, 0) ])
    "gg/G document navigation was not deterministic"

let test_vim_compatibility_baseline () =
  let runtime = vim "  alpha" in
  let runtime =
    fold_vim runtime [ key "I"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"  Xalpha"
    ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha" in
  let runtime =
    fold_vim runtime [ key "A"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"alphaX" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha" in
  let runtime =
    fold_vim runtime [ key "o"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"alpha\nX"
    ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha" in
  let runtime =
    fold_vim runtime [ key "O"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"X\nalpha"
    ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha beta" in
  let runtime = fold_vim runtime [ key "w"; key "D" ] in
  expect_string ~expected:"alpha " ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha beta" in
  let runtime =
    fold_vim runtime
      [ key "w"; key "C"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"alpha X" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha\nbeta" in
  let runtime =
    fold_vim runtime [ key "S"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"X\nbeta" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha\nbeta" in
  let runtime =
    fold_vim runtime
      [ key "c"; key "c"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"X\nbeta" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "one\ntwo\nthree" in
  let runtime =
    fold_vim runtime
      [ key "2"; key "c"; key "c"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"X\nthree"
    ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha" in
  let runtime = fold_vim runtime [ key "r"; key "X" ] in
  expect_string ~expected:"Xlpha" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha" in
  let runtime =
    fold_vim runtime [ key "R"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"Xlpha" ~actual:(text (Vim_runtime.history runtime))

let test_vim_find_visual_and_insert_controls () =
  let runtime = vim "abxxb" in
  let runtime = fold_vim runtime [ key "f"; key "b"; key ";" ] in
  expect
    (offsets (Vim_runtime.history runtime) = [ (4, 4) ])
    "f/; did not retain and repeat the last find motion";
  let runtime = fold_vim runtime [ key "," ] in
  expect
    (offsets (Vim_runtime.history runtime) = [ (1, 1) ])
    "comma did not repeat the last find in the opposite direction";
  let runtime = vim "aZb" in
  let runtime = fold_vim runtime [ key "t"; key "b" ] in
  expect
    (offsets (Vim_runtime.history runtime) = [ (1, 1) ])
    "t did not stop before its find target";
  let runtime = vim "abxxb" in
  let runtime, step = send_vim runtime (key "d") in
  let runtime, _ = send_vim runtime (key "f") in
  let runtime, _ = send_vim runtime (key "b") in
  expect_string ~expected:"xxb" ~actual:(text (Vim_runtime.history runtime));
  expect
    (List.length (Vim_runtime.change_ids step) = 0)
    "an operator prefix unexpectedly committed an edit";
  expect
    (match History.current_change (Vim_runtime.history runtime) with
    | Some change ->
        List.length (Transaction.edits (History.transaction change)) = 1
    | None -> false)
    "df did not commit one atomic find-selected edit";
  let runtime = vim "abxxb" in
  let runtime =
    fold_vim runtime
      [ key "c"; key "f"; key "b"; committed "X"; named Input_event.Escape ]
  in
  expect_string ~expected:"Xxxb" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "abxxb" in
  let runtime = fold_vim runtime [ key "y"; key "f"; key "b"; key "P" ] in
  expect_string ~expected:"ababxxb" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "abcd" in
  let runtime = fold_vim runtime [ key "v"; key "l"; key "d" ] in
  expect_string ~expected:"bcd" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "one\ntwo" in
  let runtime = fold_vim runtime [ key "V"; key "d" ] in
  expect_string ~expected:"two" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha beta" in
  let runtime =
    fold_vim runtime [ key "A"; control "w"; named Input_event.Escape ]
  in
  expect_string ~expected:"alpha " ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha beta" in
  let runtime =
    fold_vim runtime [ key "w"; key "i"; control "u"; named Input_event.Escape ]
  in
  expect_string ~expected:"beta" ~actual:(text (Vim_runtime.history runtime));
  let runtime = vim "alpha beta" in
  let runtime =
    fold_vim runtime
      [ key "\""; key "a"; key "y"; key "w"; key "i"; control "r"; key "a" ]
  in
  expect_string ~expected:"alpha alpha beta"
    ~actual:(text (Vim_runtime.history runtime))

let test_m3_boundaries_and_atomic_failures () =
  let runtime = vim "" in
  (match Vim_runtime.handle_input runtime (key "x") with
  | Error _ -> ()
  | Ok _ -> failf "x on an empty document should reject rather than clip");
  expect_string ~expected:"" ~actual:(text (Vim_runtime.history runtime));
  expect
    (Vim_runtime.input_trace runtime = [])
    "failed empty-document edit entered the input trace";
  let runtime = vim "a" in
  let runtime, _ = send_vim runtime (key "4") in
  (match Vim_runtime.handle_input runtime (key "x") with
  | Error _ -> ()
  | Ok _ -> failf "4x beyond document end should reject atomically");
  expect_string ~expected:"a" ~actual:(text (Vim_runtime.history runtime));
  let runtime = selection "" in
  let runtime =
    fold_selection runtime
      [
        key "i";
        committed "界";
        named Input_event.Escape;
        key "h";
        key "y";
        key "d";
        key "p";
      ]
  in
  expect_string ~expected:"界" ~actual:(text (Selection_runtime.history runtime))

let test_selector_property_boundaries () =
  let selectors =
    [
      Selector.Next_text_unit;
      Selector.Previous_text_unit;
      Selector.Next_word;
      Selector.Previous_word;
      Selector.Word_end;
      Selector.Current_line;
      Selector.Line_start;
      Selector.Line_end;
      Selector.Next_line;
      Selector.Previous_line;
    ]
  in
  let state = Random.State.make [| 0x4D33; 0x5345 |] in
  for case = 1 to 80 do
    let units = [| "a"; " "; ","; "é"; "界"; "\n" |] in
    let count = Random.State.int state 20 in
    let contents =
      List.init count (fun _ ->
          units.(Random.State.int state (Array.length units)))
      |> String.concat ""
    in
    let offsets =
      let rec collect offset values =
        if offset = String.length contents then List.rev (offset :: values)
        else
          let next =
            if Char.code contents.[offset] land 0x80 = 0 then offset + 1
            else
              let rec seek index =
                if
                  index = String.length contents
                  || Char.code contents.[index] land 0xC0 <> 0x80
                then index
                else seek (index + 1)
              in
              seek (offset + 1)
          in
          collect next (offset :: values)
      in
      collect 0 []
    in
    List.iter
      (fun offset ->
        let document =
          Document.create
            ~id:
              (Document_id.of_string ("selector-property-" ^ string_of_int case)
              |> must)
            ~contents
            ~initial_selections:
              [
                Selection_spec.make ~anchor_offset:offset ~head_offset:offset
                |> must;
              ]
            ()
          |> must
        in
        let snapshot = Document.snapshot document in
        List.iter
          (fun selector ->
            match Selector.resolve snapshot selector with
            | Error _ -> ()
            | Ok selections ->
                List.iter
                  (fun selection ->
                    ignore
                      (Document_snapshot.validate_anchor snapshot
                         (Selection.anchor selection)
                      |> must);
                    ignore
                      (Document_snapshot.validate_anchor snapshot
                         (Selection.head selection)
                      |> must))
                  (Selection_set.to_list selections))
          selectors)
      offsets
  done

let test_selection_first_semantics_and_multiple_selections () =
  let selection_runtime = selection "alpha beta" in
  let selection_runtime, _ = send_selection selection_runtime (key "w") in
  expect
    (offsets (Selection_runtime.history selection_runtime) = [ (0, 6) ])
    "selection-first w did not visibly select a shared next-word target";
  let selection_runtime, deleted = send_selection selection_runtime (key "d") in
  expect_string ~expected:"beta"
    ~actual:(text (Selection_runtime.history selection_runtime));
  expect
    (List.map Model_intent.identity (Selection_runtime.intents deleted)
    = [ "apply:current-selections:delete" ])
    "selection-first delete bypassed shared transformation semantics";
  let multi = selection "foo bar foo baz foo" in
  let multi = fold_selection multi [ key "W"; key "*" ] in
  expect
    (offsets (Selection_runtime.history multi) = [ (0, 3); (8, 11); (16, 19) ])
    "all-occurrences did not create deterministic multi-selections";
  let multi, step = send_selection multi (key "d") in
  expect_string ~expected:" bar  baz "
    ~actual:(text (Selection_runtime.history multi));
  expect
    (List.length (Selection_runtime.intents step) = 1)
    "multi-selection delete should be one shared semantic transaction";
  let change =
    match History.current_change (Selection_runtime.history multi) with
    | Some change -> change
    | None -> failf "multi-selection delete did not create a history change"
  in
  expect
    (List.length (Transaction.edits (History.transaction change)) = 3)
    "multi-selection delete did not preserve deterministic three-edit ordering"

let test_selection_algebra_and_regex_commands () =
  let history =
    history_with_selections "selection-algebra" "red, green, blue"
      [ (0, 16) ]
      ~primary:0
  in
  let split =
    Selection_algebra.split_regex (context history) ~pattern:", *" |> must
  in
  let history = apply_selection_intent history split in
  expect
    (offsets history = [ (0, 3); (5, 10); (12, 16) ])
    "regex split did not preserve ordered, separator-free selection ranges";
  let selected =
    Selection_algebra.select_regex (context history) ~pattern:"[a-z]+" |> must
  in
  let selected = apply_selection_intent history selected in
  expect
    (offsets selected = [ (0, 3); (5, 10); (12, 16) ])
    "regex select did not return non-empty matches in document order";
  let kept =
    Selection_algebra.keep_matching (context selected) ~pattern:"green" |> must
  in
  let kept = apply_selection_intent selected kept in
  expect
    (offsets kept = [ (5, 10) ])
    "regex filter did not retain the matching selection";
  let removed =
    Selection_algebra.remove_matching (context selected) ~pattern:"green"
    |> must
  in
  let removed = apply_selection_intent selected removed in
  expect
    (offsets removed = [ (0, 3); (12, 16) ])
    "regex filter did not remove the matching selection";
  let rotated =
    Selection_algebra.rotate_primary (context selected)
      Selection_algebra.Forward
    |> must
  in
  let rotated = apply_selection_intent selected rotated in
  let rotated_selections = Editor_context.selections (context rotated) in
  expect
    (rotated_selections.primary_index = 1)
    "primary rotation did not move to the next selection";
  let contents =
    history_with_selections "selection-contents" "one two three"
      [ (0, 3); (4, 7); (8, 13) ]
      ~primary:1
  in
  let forward =
    Selection_algebra.rotate_contents (context contents)
      Selection_algebra.Forward ()
    |> must
  in
  let forward = apply_selection_intent contents forward in
  expect_string ~expected:"three one two" ~actual:(text forward);
  let backward =
    Selection_algebra.rotate_contents (context contents)
      Selection_algebra.Backward ()
    |> must
  in
  let backward = apply_selection_intent contents backward in
  expect_string ~expected:"two three one" ~actual:(text backward);
  let unequal =
    history_with_selections "selection-contents-unequal" "a bb ccc"
      [ (0, 1); (2, 4); (5, 8) ]
      ~primary:0
  in
  let unequal =
    Selection_algebra.rotate_contents (context unequal)
      Selection_algebra.Forward ()
    |> must
    |> apply_selection_intent unequal
  in
  expect_string ~expected:"ccc a bb" ~actual:(text unequal);
  let grouped =
    history_with_selections "selection-contents-grouped" "a b c d"
      [ (0, 1); (2, 3); (4, 5); (6, 7) ]
      ~primary:0
  in
  let grouped_command =
    Command_registry.find (registry ())
      (Command_id.of_string "editor.selection.rotate-contents-forward" |> must)
    |> must
  in
  let grouped_invocation =
    Command_invocation.create
      ~id:
        (Command_id.of_string "editor.selection.rotate-contents-forward" |> must)
      ~arguments:
        [
          Command_argument.make ~name:"group-size"
            ~value:(Command_argument.Text "2")
          |> must;
        ]
    |> must
  in
  let grouped_intent =
    match
      Command.execute grouped_command (context grouped) grouped_invocation
    with
    | Ok [ intent ] -> intent
    | Ok _ ->
        failf "grouped content rotation returned an unexpected intent count"
    | Error error -> failf "%s" (Error.to_string error)
  in
  let grouped = apply_selection_intent grouped grouped_intent in
  expect_string ~expected:"b a d c" ~actual:(text grouped);
  expect
    (Result.is_error
       (Selection_algebra.rotate_contents (context grouped)
          Selection_algebra.Forward ~group_size:3 ()))
    "content rotation accepted a group size that does not divide the selection \
     count";
  let invalid_group_invocation =
    Command_invocation.create
      ~id:
        (Command_id.of_string "editor.selection.rotate-contents-forward" |> must)
      ~arguments:
        [
          Command_argument.make ~name:"group-size"
            ~value:(Command_argument.Text "many")
          |> must;
        ]
    |> must
  in
  expect
    (Result.is_error
       (Command.execute grouped_command (context grouped)
          invalid_group_invocation))
    "content rotation accepted a non-integer group size";
  expect
    (Result.is_error
       (Selection_algebra.rotate_contents (context kept)
          Selection_algebra.Forward ()))
    "content rotation accepted one selection";
  let with_empty =
    history_with_selections "selection-contents-empty" "one two"
      [ (0, 3); (4, 4) ]
      ~primary:0
  in
  expect
    (Result.is_error
       (Selection_algebra.rotate_contents (context with_empty)
          Selection_algebra.Forward ()))
    "content rotation accepted an empty selection";
  expect
    (Result.is_error
       (History.apply_intent ~source:Transaction.Test contents
          (Model_intent.replace_selection_contents [ "one" ]
          |> Model_intent.to_kernel)))
    "mismatched per-selection replacement count was accepted";
  let replay =
    Replay.create ~document_id:"selection-content-replay"
      ~contents:"one two three"
      ~initial_selections:
        {
          Replay.selections =
            [
              Selection_spec.make ~anchor_offset:0 ~head_offset:3 |> must;
              Selection_spec.make ~anchor_offset:4 ~head_offset:7 |> must;
              Selection_spec.make ~anchor_offset:8 ~head_offset:13 |> must;
            ];
          primary = 1;
        }
      ~actions:
        [
          Replay.Intent
            (Selection_algebra.rotate_contents (context contents)
               Selection_algebra.Forward ()
            |> must |> Model_intent.to_kernel);
        ]
    |> must
  in
  let replayed =
    Replay.to_string replay |> Replay.of_string |> must |> Replay.run |> must
  in
  expect_string ~expected:"three one two" ~actual:(text replayed);
  let content_runtime_document =
    Document.create
      ~id:(Document_id.of_string "selection-content-runtime" |> must)
      ~contents:"one two three"
      ~initial_selections:
        [
          Selection_spec.make ~anchor_offset:0 ~head_offset:3 |> must;
          Selection_spec.make ~anchor_offset:4 ~head_offset:7 |> must;
          Selection_spec.make ~anchor_offset:8 ~head_offset:13 |> must;
        ]
      ~primary:0 ()
    |> must
  in
  let content_runtime =
    Selection_runtime.create ~commands:(registry ())
      ~document:content_runtime_document ()
    |> must
  in
  let content_runtime = fold_selection content_runtime [ alt ")" ] in
  expect_string ~expected:"three one two"
    ~actual:(text (Selection_runtime.history content_runtime));
  let grouped_runtime_document =
    Document.create
      ~id:(Document_id.of_string "selection-content-group-runtime" |> must)
      ~contents:"a b c d"
      ~initial_selections:
        [
          Selection_spec.make ~anchor_offset:0 ~head_offset:1 |> must;
          Selection_spec.make ~anchor_offset:2 ~head_offset:3 |> must;
          Selection_spec.make ~anchor_offset:4 ~head_offset:5 |> must;
          Selection_spec.make ~anchor_offset:6 ~head_offset:7 |> must;
        ]
      ~primary:0 ()
    |> must
  in
  let grouped_runtime =
    Selection_runtime.create ~commands:(registry ())
      ~document:grouped_runtime_document ()
    |> must
  in
  let grouped_runtime = fold_selection grouped_runtime [ key "2"; alt ")" ] in
  expect_string ~expected:"b a d c"
    ~actual:(text (Selection_runtime.history grouped_runtime));
  let merged =
    history_with_selections "selection-merge" "abcdef"
      [ (0, 1); (1, 3); (4, 6) ]
      ~primary:1
  in
  let merge = Selection_algebra.merge_consecutive (context merged) |> must in
  let merged = apply_selection_intent merged merge in
  expect
    (offsets merged = [ (0, 3); (4, 6) ])
    "merge-consecutive did not merge exactly touching ranges";
  let reversed =
    history_with_selections "selection-orientation" "abcdef"
      [ (3, 0); (6, 4) ]
      ~primary:1
  in
  let flipped = Selection_algebra.flip (context reversed) |> must in
  let flipped = apply_selection_intent reversed flipped in
  expect
    (offsets flipped = [ (0, 3); (4, 6) ])
    "flip did not reverse every selection orientation";
  let forward = Selection_algebra.ensure_forward (context reversed) |> must in
  let forward = apply_selection_intent reversed forward in
  expect
    (offsets forward = [ (0, 3); (4, 6) ])
    "ensure-forward did not normalize every selection orientation";
  let unicode =
    history_with_selections "selection-unicode" "é" [ (0, 2) ] ~primary:0
  in
  expect
    (Result.is_error
       (Selection_algebra.select_regex (context unicode) ~pattern:"."))
    "a byte-oriented regex match that splits UTF-8 was accepted";
  expect
    (Result.is_error
       (Selection_algebra.split_regex (context history) ~pattern:"["))
    "an invalid regex was accepted";
  expect
    (Result.is_error
       (Selection_algebra.select_regex (context history) ~pattern:""))
    "a zero-width regex was accepted";
  let multi =
    selection "foo bar foo" |> fun runtime ->
    fold_selection runtime [ key "W"; key "*"; key ")" ]
  in
  let multi_selections =
    Selection_runtime.context multi |> Editor_context.selections
  in
  expect
    (multi_selections.primary_index = 1)
    "selection-first primary rotation did not dispatch the generic command"

let test_cross_model_equivalence_and_replay_boundary () =
  let vim_runtime = fold_vim (vim "alpha beta") [ key "d"; key "w" ] in
  let selection_runtime =
    fold_selection (selection "alpha beta") [ key "w"; key "d" ]
  in
  expect_string
    ~expected:(text (Vim_runtime.history vim_runtime))
    ~actual:(text (Selection_runtime.history selection_runtime));
  let vim_runtime = vim "alpha beta" in
  let vim_runtime, _ = send_vim vim_runtime (key "d") in
  let vim_runtime, step = send_vim vim_runtime (key "w") in
  expect
    (List.length (Vim_runtime.input_trace vim_runtime) = 2)
    "input trace should retain Vim grammar events";
  let intent = List.hd (Vim_runtime.intents step) |> Model_intent.to_kernel in
  let replay =
    Replay.create ~document_id:"m3-replay" ~contents:"alpha beta"
      ~initial_selections:
        {
          Replay.selections =
            [ Selection_spec.make ~anchor_offset:0 ~head_offset:0 |> must ];
          primary = 0;
        }
      ~actions:[ Replay.Intent intent ]
    |> must
  in
  let replayed = Replay.run replay |> must in
  expect_string ~expected:"beta" ~actual:(text replayed)

let tests =
  [
    ("shared word and line selectors", test_shared_word_and_line_selectors);
    ( "Vim pending counts and cancellation",
      test_vim_pending_counts_and_cancellation );
    ( "Vim insert, change, and character edits",
      test_vim_insert_change_and_character_edits );
    ( "Vim linewise register, history, and repeat",
      test_vim_linewise_register_history_and_repeat );
    ("Vim text objects and line counts", test_vim_text_objects_and_line_counts);
    ("Vim compatibility baseline", test_vim_compatibility_baseline);
    ( "Vim find, visual, and insert controls",
      test_vim_find_visual_and_insert_controls );
    ("M3 boundaries and atomic failures", test_m3_boundaries_and_atomic_failures);
    ("M3 selector property boundaries", test_selector_property_boundaries);
    ( "selection-first semantics and multiple selections",
      test_selection_first_semantics_and_multiple_selections );
    ( "selection algebra and regex commands",
      test_selection_algebra_and_regex_commands );
    ( "cross-model equivalence and replay boundary",
      test_cross_model_equivalence_and_replay_boundary );
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
