open Zenbu_kernel
open Zenbu_model_api

module Dsl = Zenbu_model_dsl

exception Test_failure of string

let failf format = Printf.ksprintf (fun message -> raise (Test_failure message)) format

let expect condition format =
  Printf.ksprintf
    (fun message -> if not condition then raise (Test_failure message))
    format

let expect_string ~expected ~actual =
  expect (String.equal expected actual) "expected %S, got %S" expected actual

let expect_error = function
  | Error _ -> ()
  | Ok _ -> failf "expected an error"

let must = function
  | Ok value -> value
  | Error error -> failf "%s" (Error.to_string error)

let source =
  {|zenbu-model 1

model "zenbu.example.modal" {
  title "Minimal modal"
  initial normal

  state normal {
    status { label "NORMAL" input keys }
    on "i" -> insert
    on "d w" -> normal {
      apply selector "current-word" transform "delete"
    }
  }

  state insert {
    status { label "INSERT" input text }
    on "Escape" -> normal
    on "<text>" as text -> insert { insert $text }
  }
}
|}

let mismatch_source =
  {|zenbu-model 1
model "zenbu.example.mismatch" {
  title "Mismatch"
  initial normal
  state normal {
    status { label "NORMAL" input keys }
    on "d w" -> normal
    on "x" -> insert
  }
  state insert {
    status { label "INSERT" input text }
  }
}
|}

let branching_source =
  {|zenbu-model 1
model "zenbu.example.branching" {
  title "Branching"
  initial normal
  state normal {
    status { label "NORMAL" input keys }
    on "d w" -> after_word { apply selector "current-word" transform "delete" }
    on "d e" -> after_end
    on "g u w" -> normal
  }
  state after_word {
    status { label "AFTER WORD" input keys }
    on "g u w" -> normal
  }
  state after_end {
    status { label "AFTER END" input keys }
    on "d w" -> after_word
  }
}
|}

let text_source =
  {|zenbu-model 1
model "zenbu.example.text" {
  title "Text"
  initial insert
  state insert {
    status { label "INSERT" input text }
    on "Escape" -> normal
    on "<text>" as text -> insert { insert $text }
  }
  state normal {
    status { label "NORMAL" input keys }
  }
}
|}

let compile ?(source_name = "test.zenmodel") value =
  match Dsl.Compile.compile ~source_name ~source:value with
  | Ok (compiled, warnings) ->
      expect (warnings = []) "the base grammar unexpectedly produced warnings";
      compiled
  | Error diagnostics ->
      diagnostics |> List.map Dsl.Diagnostic.format |> String.concat "\n" |> failf "%s"

let diagnostics value =
  match Dsl.Compile.compile ~source_name:"broken.zenmodel" ~source:value with
  | Ok _ -> failf "expected invalid grammar to fail"
  | Error diagnostics -> diagnostics

let diagnostic_contains value needle =
  let rendered = diagnostics value |> List.map Dsl.Diagnostic.format |> String.concat "\n" in
  expect
    (String.contains rendered needle.[0]
    && String.length needle <= String.length rendered
    &&
    let rec contains index =
      if index + String.length needle > String.length rendered then false
      else if String.sub rendered index (String.length needle) = needle then true
      else contains (index + 1)
    in
    contains 0)
    "expected diagnostic containing %S, got %s" needle rendered;
  rendered

let key text = Input_event.key_press (Input_event.logical_text text |> must)
let named value = Input_event.key_press (Input_event.named_key value)
let committed text = Input_event.text_input text |> must

let context () =
  let document =
    Document.create ~id:(Document_id.of_string "dsl-test" |> must)
      ~contents:"alpha beta" ()
    |> must
  in
  Editor_context.from_snapshot ~snapshot:(Document.snapshot document) ~commands:[] ()

let test_valid_model_and_description () =
  let compiled = compile source in
  expect_string ~expected:"zenbu.example.modal"
    ~actual:(Editing_model.id compiled.descriptor);
  expect (List.length compiled.states = 2) "valid model did not retain both states";
  let description = Dsl.Describe.render ~warnings:[] compiled in
  expect (String.contains description 'd') "description omitted transitions";
  expect (String.contains description 'M') "description omitted status metadata";
  expect
    (String.contains description 'p')
    "description omitted generated prefix information";
  expect_string ~expected:description
    ~actual:(Dsl.Describe.render ~warnings:[] compiled)

let test_diagnostics () =
  let invalid_utf8 = "zenbu-model 1\n" ^ String.make 1 (Char.chr 255) in
  diagnostic_contains invalid_utf8 "source is not valid UTF-8" |> ignore;
  diagnostic_contains "zenbu-model 2\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input keys } } }"
    "unsupported zenbu-model version" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input keys } } state a { status { label \"A\" input keys } } }"
    "duplicate state `a`" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" state a { status { label \"A\" input keys } } }"
    "requires exactly one `initial`" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial missing state a { status { label \"A\" input keys } } }"
    "unknown initial state `missing`" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input keys } on \"x\" -> missing } }"
    "unknown state `missing`" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input keys } on \"\" -> a } }"
    "invalid input sequence" |> ignore;
  let long_pattern = String.concat " " (List.init 17 (fun _ -> "a")) in
  diagnostic_contains
    ("zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input keys } on \""
   ^ long_pattern ^ "\" -> a } }")
    "at most 16" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input keys } on \"g\" -> a on \"g\" -> a } }"
    "duplicate or overlapping" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input keys } on \"g\" -> a on \"g g\" -> a } }"
    "use an explicit intermediate state" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input text } on \"<text>\" as first -> a on \"<text>\" as second -> a } }"
    "duplicate or overlapping" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input text } on \"a <text>\" -> a } }"
    "must be the entire input sequence" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input keys } on \"<text>\" as text -> a { insert $text } } }"
    "only valid in a state with `input text`" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input text } on \"<text>\" as text -> a { apply selector \"missing\" transform \"delete\" } } }"
    "unknown selector `missing`" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input text } on \"<text>\" as text -> a { apply selector \"current-word\" transform \"missing\" } } }"
    "unknown transformation `missing`" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input text } on \"<text>\" -> a { insert $text } } }"
    "undefined text capture `$text`" |> ignore;
  diagnostic_contains "zenbu-model 1\nmodel \"x\" { title \"X\" initial a state a { status { label \"A\" input keys } on \"x\" -> a { command } } }"
    "unsupported transition effect `command`" |> ignore;
  diagnostic_contains
    "zenbu-model 999999999999999999999999999999999999999999999999\n"
    "integer literal is outside the supported range" |> ignore;
  let malformed = diagnostics "zenbu-model 1\nmodel \"x\" {" |> List.hd in
  expect (Dsl.Diagnostic.line malformed = 2) "parser diagnostic lost source line";
  expect (Dsl.Diagnostic.column malformed > 0) "parser diagnostic lost source column";
  let crlf = diagnostics "zenbu-model 1\r\nmodel \"é\" {\r\n" |> List.hd in
  expect (Dsl.Diagnostic.line crlf = 3) "CRLF source diagnostic has the wrong line";
  expect (Dsl.Diagnostic.column crlf = 1) "CRLF source diagnostic has the wrong column";
  expect
    (Source_span.start_offset (Dsl.Diagnostic.span crlf)
    = String.length "zenbu-model 1\r\nmodel \"é\" {\r\n")
    "UTF-8 source diagnostic did not retain byte offsets"

let test_runtime () =
  let compiled = compile source in
  let context = context () in
  let state = Dsl.Runtime.initialize compiled in
  let state, effects = Dsl.Runtime.handle_input state (key "i") context in
  expect (effects = []) "mode transition unexpectedly emitted an effect";
  expect_string ~expected:"insert" ~actual:(Model_status.id (Dsl.Runtime.status state));
  let state, effects = Dsl.Runtime.handle_input state (committed "界") context in
  expect (List.length effects = 1) "committed text did not produce one effect";
  expect_string ~expected:"execute insert-text"
    ~actual:(Model_effect.identity (List.hd effects));
  (match List.hd effects with
  | Model_effect.Execute_intent intent -> (
      match Model_intent.to_kernel intent with
      | Intent.Insert_text text -> expect_string ~expected:"界" ~actual:text
      | _ -> failf "text rule emitted the wrong semantic intent")
  | _ -> failf "text rule emitted the wrong effect variant");
  let state, effects = Dsl.Runtime.handle_input state (named Input_event.Escape) context in
  expect (effects = []) "Escape unexpectedly emitted an effect";
  expect_string ~expected:"normal" ~actual:(Model_status.id (Dsl.Runtime.status state));
  let state, effects = Dsl.Runtime.handle_input state (key "d") context in
  expect (effects = []) "a pending prefix emitted an effect";
  expect
    (Model_status.pending_input (Dsl.Runtime.status state) = Some "d")
    "pending prefix was not exposed through status";
  let state, effects = Dsl.Runtime.handle_input state (key "w") context in
  expect (List.length effects = 1) "d w did not emit its semantic effect";
  expect_string ~expected:"execute apply:current-word:delete"
    ~actual:(Model_effect.identity (List.hd effects));
  expect_string ~expected:"normal" ~actual:(Model_status.id (Dsl.Runtime.status state));
  expect
    (Model_status.pending_input (Dsl.Runtime.status state) = None)
    "completed transition did not clear pending input";
  let pending, _ = Dsl.Runtime.handle_input state (key "d") context in
  let mismatched, effects = Dsl.Runtime.handle_input pending (key "x") context in
  expect (effects = []) "prefix mismatch emitted an effect";
  expect_string ~expected:"normal"
    ~actual:(Model_status.id (Dsl.Runtime.status mismatched));
  expect
    (Model_status.pending_input (Dsl.Runtime.status mismatched) = None)
    "prefix mismatch did not clear pending input";
  let mismatch = Dsl.Runtime.initialize (compile mismatch_source) in
  let mismatch, _ = Dsl.Runtime.handle_input mismatch (key "d") context in
  let mismatch, effects = Dsl.Runtime.handle_input mismatch (key "x") context in
  expect (effects = []) "mismatching input ran an effect";
  expect_string ~expected:"normal"
    ~actual:(Model_status.id (Dsl.Runtime.status mismatch));
  let left, left_effects = Dsl.Runtime.handle_input (Dsl.Runtime.initialize compiled) (key "d") context in
  let right, right_effects = Dsl.Runtime.handle_input (Dsl.Runtime.initialize compiled) (key "d") context in
  expect (left_effects = right_effects) "identical runs produced different effects";
  expect
    (Model_status.pending_input (Dsl.Runtime.status left)
    = Model_status.pending_input (Dsl.Runtime.status right))
    "identical runs produced different states"

let test_input_rules_and_adapter () =
  let compiled = compile source in
  let rules = Dsl.Runtime.initialize compiled |> Dsl.Runtime.input_rules in
  expect
    (List.exists
       (fun rule ->
         Input_rule.pattern rule = Input_rule.Exact "i"
         && Input_rule.kind rule = Input_rule.Binding)
       rules)
    "immediate transition did not become an input binding";
  expect
    (List.exists
       (fun rule ->
         Input_rule.pattern rule = Input_rule.Exact "d"
         && Input_rule.kind rule = Input_rule.Prefix)
       rules)
    "multi-event transition did not become a prefix rule";
  let insert_state, _ =
    Dsl.Runtime.handle_input (Dsl.Runtime.initialize compiled) (key "i") (context ())
  in
  expect
    (List.exists
       (fun rule ->
         Input_rule.pattern rule = Input_rule.Text_input
         && Input_rule.kind rule = Input_rule.Catch_all)
       (Dsl.Runtime.input_rules insert_state))
    "singleton text transition did not become a catch-all rule";
  Dsl.Runtime.Adapter.configure compiled;
  let module Runtime = Model_runtime.Make (Dsl.Runtime.Adapter) in
  let document =
    Document.create ~id:(Document_id.of_string "dsl-adapter" |> must)
      ~contents:"alpha" ()
    |> must
  in
  let runtime = Runtime.create ~commands:Command_registry.empty ~document () |> must in
  let runtime, _ = Runtime.handle_input runtime (key "d") |> must in
  let runtime, step = Runtime.handle_input runtime (key "w") |> must in
  expect
    (List.map Model_effect.identity (Runtime.effects step)
    = [ "execute apply:current-word:delete" ])
    "adapter did not return the normal model effect";
  expect_string ~expected:"" 
    ~actual:
      (Runtime.history runtime |> History.current |> Document.snapshot
     |> Document_snapshot.contents);
  Dsl.Runtime.Adapter.clear ()

let test_branching_and_deep_prefixes () =
  let compiled = compile branching_source in
  let context = context () in
  let state = Dsl.Runtime.initialize compiled in
  let pending, effects = Dsl.Runtime.handle_input state (key "d") context in
  expect (effects = []) "a shared prefix emitted an effect";
  expect
    (Model_status.pending_input (Dsl.Runtime.status pending) = Some "d")
    "a shared prefix was not inspectable";
  let after_end, effects = Dsl.Runtime.handle_input pending (key "e") context in
  expect (effects = []) "d e selected the d w transition";
  expect_string ~expected:"after_end"
    ~actual:(Model_status.id (Dsl.Runtime.status after_end));
  let after_end, _ = Dsl.Runtime.handle_input after_end (key "d") context in
  let after_word, effects = Dsl.Runtime.handle_input after_end (key "w") context in
  expect (List.length effects = 0) "the after-end transition emitted an effect";
  expect_string ~expected:"after_word"
    ~actual:(Model_status.id (Dsl.Runtime.status after_word));
  let after_word, _ = Dsl.Runtime.handle_input after_word (key "g") context in
  let deep_pending, effects = Dsl.Runtime.handle_input after_word (key "u") context in
  expect (effects = []) "a three-event prefix emitted an effect";
  expect
    (Model_status.pending_input (Dsl.Runtime.status deep_pending) = Some "g u")
    "the complete deep prefix was not retained";
  let reset, effects = Dsl.Runtime.handle_input deep_pending (key "x") context in
  expect (effects = []) "a deep prefix mismatch emitted an effect";
  expect_string ~expected:"after_word"
    ~actual:(Model_status.id (Dsl.Runtime.status reset));
  expect
    (Model_status.pending_input (Dsl.Runtime.status reset) = None)
    "a deep prefix mismatch did not reset to its stable source state";
  let initial = Dsl.Runtime.initialize compiled in
  let initial, _ = Dsl.Runtime.handle_input initial (key "d") context in
  let after_word, effects = Dsl.Runtime.handle_input initial (key "w") context in
  expect (List.length effects = 1) "d w did not select its own shared-prefix rule";
  let after_word, _ = Dsl.Runtime.handle_input after_word (key "g") context in
  let after_word, _ = Dsl.Runtime.handle_input after_word (key "u") context in
  let complete, effects = Dsl.Runtime.handle_input after_word (key "w") context in
  expect (effects = []) "a completed three-event transition emitted an effect";
  expect_string ~expected:"normal"
    ~actual:(Model_status.id (Dsl.Runtime.status complete))

let test_text_capture_preserves_utf8 () =
  let compiled = compile text_source in
  let context = context () in
  List.iter
    (fun value ->
      let _, effects =
        Dsl.Runtime.handle_input (Dsl.Runtime.initialize compiled) (committed value)
          context
      in
      match effects with
      | [ Model_effect.Execute_intent intent ] -> (
          match Model_intent.to_kernel intent with
          | Intent.Insert_text actual -> expect_string ~expected:value ~actual
          | _ -> failf "committed text emitted a non-insert intent")
      | _ -> failf "committed text did not emit exactly one intent")
    [ "a"; "é"; "你"; "🙂"; "á" ];
  let state, effects =
    Dsl.Runtime.handle_input (Dsl.Runtime.initialize compiled) (key "a") context
  in
  expect (effects = []) "a key event incorrectly matched <text>";
  expect_string ~expected:"insert" ~actual:(Model_status.id (Dsl.Runtime.status state));
  let state, effects =
    Dsl.Runtime.handle_input state (named Input_event.Escape) context
  in
  expect (effects = []) "Escape emitted an effect in a text state";
  expect_string ~expected:"normal" ~actual:(Model_status.id (Dsl.Runtime.status state))

let test_adapter_configuration_isolation_history_and_replay () =
  let module Runtime = Model_runtime.Make (Dsl.Runtime.Adapter) in
  let grammar = compile source in
  let document id contents =
    Document.create ~id:(Document_id.of_string id |> must) ~contents () |> must
  in
  Dsl.Runtime.Adapter.clear ();
  Dsl.Runtime.Adapter.configure grammar;
  let left =
    Runtime.create ~commands:Command_registry.empty
      ~document:(document "dsl-left" "alpha beta") ()
    |> must
  in
  expect_error
    (Runtime.create ~commands:Command_registry.empty
       ~document:(document "dsl-no-stale-config" "alpha beta") ());
  Dsl.Runtime.Adapter.configure grammar;
  let right =
    Runtime.create ~commands:Command_registry.empty
      ~document:(document "dsl-right" "alpha beta") ()
    |> must
  in
  let left, _ = Runtime.handle_input left (key "i") |> must in
  expect_string ~expected:"insert"
    ~actual:(Model_status.id (Runtime.status left));
  expect_string ~expected:"normal"
    ~actual:(Model_status.id (Runtime.status right));
  let left, step = Runtime.handle_input left (committed "X") |> must in
  let contents runtime =
    Runtime.history runtime |> History.current |> Document.snapshot
    |> Document_snapshot.contents
  in
  expect_string ~expected:"Xalpha beta" ~actual:(contents left);
  expect (List.length (Runtime.change_ids step) = 1)
    "DSL text insertion did not create exactly one normal transaction";
  let left, _ = Runtime.execute_effects left ~input:(key "u") [ Model_effect.Undo ] |> must in
  expect_string ~expected:"alpha beta" ~actual:(contents left);
  let left, _ = Runtime.execute_effects left ~input:(key "r") [ Model_effect.Redo ] |> must in
  expect_string ~expected:"Xalpha beta" ~actual:(contents left);
  let replay_intent =
    match Runtime.intents step with
    | [ intent ] -> Model_intent.to_kernel intent
    | _ -> failf "DSL text transition did not produce one semantic replay intent"
  in
  let selection = Selection_spec.make ~anchor_offset:0 ~head_offset:0 |> must in
  let replay =
    Replay.create ~document_id:"dsl-semantic-replay" ~contents:"alpha beta"
      ~initial_selections:{ Replay.selections = [ selection ]; primary = 0 }
      ~actions:[ Replay.Intent replay_intent ]
    |> must
  in
  let encoded = Replay.to_string replay in
  expect (not (String.contains encoded '.'))
    "semantic replay unexpectedly encoded DSL model identity";
  let replayed = Replay.of_string encoded |> must |> Replay.run |> must in
  expect_string ~expected:"Xalpha beta"
    ~actual:
      (replayed |> History.current |> Document.snapshot
      |> Document_snapshot.contents);
  Dsl.Runtime.Adapter.clear ()

let tests =
  [
    ("valid model and deterministic description", test_valid_model_and_description);
    ("parser and validation diagnostics", test_diagnostics);
    ("deterministic runtime", test_runtime);
    ("input rules and model-runtime adapter", test_input_rules_and_adapter);
    ("branching and deep input prefixes", test_branching_and_deep_prefixes);
    ("UTF-8 committed text capture", test_text_capture_preserves_utf8);
    ( "adapter configuration isolation, history, and semantic replay",
      test_adapter_configuration_isolation_history_and_replay );
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
