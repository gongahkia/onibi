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

let must = function
  | Ok value -> value
  | Error error -> failf "%s" (Error.to_string error)

let registry () =
  List.fold_left
    (fun registry command ->
      Result.bind registry (fun registry ->
          Command_registry.register registry command))
    (Ok Command_registry.empty)
    (Semantic_commands.apply_command :: Semantic_commands.selection_commands)
  |> must

let compile ?(commands = registry ()) source =
  match
    Dsl.Compile.compile ~commands ~source_name:"v11.zenmodel" ~source ()
  with
  | Ok (grammar, warnings) ->
      expect (warnings = []) "valid grammar unexpectedly produced warnings";
      grammar
  | Error diagnostics ->
      diagnostics
      |> List.map Dsl.Diagnostic.format
      |> String.concat "\n" |> failf "%s"

let diagnostics ?(commands = registry ()) source =
  match
    Dsl.Compile.compile ~commands ~source_name:"v11-invalid.zenmodel" ~source ()
  with
  | Ok _ -> failf "expected invalid grammar to fail"
  | Error diagnostics ->
      diagnostics |> List.map Dsl.Diagnostic.format |> String.concat "\n"

let contains text fragment =
  let rec loop offset =
    if offset + String.length fragment > String.length text then false
    else if String.sub text offset (String.length fragment) = fragment then true
    else loop (offset + 1)
  in
  String.length fragment = 0 || loop 0

let expect_diagnostic ?commands source fragment =
  let rendered = diagnostics ?commands source in
  expect
    (contains rendered fragment)
    "expected diagnostic containing %S, got %s" fragment rendered

let expect_diagnostic_without_commands source fragment =
  let rendered =
    match
      Dsl.Compile.compile ~source_name:"v11-invalid.zenmodel" ~source ()
    with
    | Ok _ -> failf "expected invalid grammar to fail"
    | Error diagnostics ->
        diagnostics |> List.map Dsl.Diagnostic.format |> String.concat "\n"
  in
  expect
    (contains rendered fragment)
    "expected diagnostic containing %S, got %s" fragment rendered

let key text = Input_event.key_press (Input_event.logical_text text |> must)

let context ?(selected = false) () =
  let initial_selections =
    if selected then
      [ Selection_spec.make ~anchor_offset:0 ~head_offset:1 |> must ]
    else []
  in
  let document =
    Document.create
      ~id:(Document_id.of_string "dsl-v11" |> must)
      ~contents:"alpha" ~initial_selections ()
    |> must
  in
  Editor_context.from_snapshot
    ~snapshot:(Document.snapshot document)
    ~commands:[] ()

let action_source =
  {|zenbu-model 1
model "zenbu.test.actions" {
  title "Actions"
  initial normal
  action delete_word {
    apply selector "current-word" transform "delete"
  }
  action delete_twice {
    do delete_word
    do delete_word
  }
  state normal {
    status { label "NORMAL" input keys }
    on "x" -> normal { do delete_twice }
  }
}
|}

let guard_source =
  {|zenbu-model 1
model "zenbu.test.guards" {
  title "Guards"
  initial direct
  state direct {
    status { label "DIRECT" input keys }
    on "Backspace" when selection.any_nonempty -> direct {
      apply selector "current-selections" transform "delete"
    }
    on "Backspace" else -> direct {
      apply selector "previous-text-unit" transform "delete"
    }
    on "d w" when selection.any_nonempty -> direct {
      apply selector "current-selections" transform "delete"
    }
  }
}
|}

let no_else_source =
  {|zenbu-model 1
model "zenbu.test.no-else" {
  title "No else"
  initial direct
  state direct {
    status { label "DIRECT" input keys }
    on "x" when selection.any_nonempty -> direct {
      apply selector "current-selections" transform "delete"
    }
  }
}
|}

let command_source =
  {|zenbu-model 1
model "zenbu.test.commands" {
  title "Commands"
  initial select
  state select {
    status { label "SELECT" input keys }
    on "m" -> select { command "editor.selection.merge-consecutive" }
  }
}
|}

module Runtime = Model_runtime.Make (Dsl.Runtime.Adapter)

let test_actions () =
  let grammar = compile action_source in
  expect
    (Dsl.Compile.action_count grammar = 2)
    "actions were not retained for inspection";
  let state, effects =
    Dsl.Runtime.handle_input
      (Dsl.Runtime.initialize grammar)
      (key "x") (context ())
  in
  expect
    (List.map Model_effect.identity effects
    = [
        "execute apply:current-word:delete"; "execute apply:current-word:delete";
      ])
    "action expansion did not produce the inline effects";
  expect
    (Model_status.id (Dsl.Runtime.status state) = "normal")
    "action transition selected the wrong target";
  let description = Dsl.Describe.render ~warnings:[] grammar in
  expect
    (contains description "action: delete_word")
    "model-describe omitted an action declaration";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a action x { do missing } state a { status { label "A" input keys } } }|}
    "unknown action `missing`";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a action x { } state a { status { label "A" input keys } } }|}
    "must not be empty";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a action x { do x } state a { status { label "A" input keys } } }|}
    "recursive action reference `x`";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a action x { do y } action y { do x } state a { status { label "A" input keys } } }|}
    "recursive action reference";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a action x { apply selector "current-word" transform "delete" } action x { apply selector "current-word" transform "delete" } state a { status { label "A" input keys } } }|}
    "duplicate action `x`";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a action x { insert $text } state a { status { label "A" input text } on "<text>" as text -> a { do x } } }|}
    "actions may not reference transition captures"

let test_guards () =
  let grammar = compile guard_source in
  let _, selected_effects =
    Dsl.Runtime.handle_input
      (Dsl.Runtime.initialize grammar)
      (Input_event.key_press (Input_event.named_key Input_event.Backspace))
      (context ~selected:true ())
  in
  expect
    (List.map Model_effect.identity selected_effects
    = [ "execute apply:current-selections:delete" ])
    "true guard did not choose the selection arm";
  let _, empty_effects =
    Dsl.Runtime.handle_input
      (Dsl.Runtime.initialize grammar)
      (Input_event.key_press (Input_event.named_key Input_event.Backspace))
      (context ())
  in
  expect
    (List.map Model_effect.identity empty_effects
    = [ "execute apply:previous-text-unit:delete" ])
    "else arm did not choose the empty-selection behavior";
  let pending, prefix_effects =
    Dsl.Runtime.handle_input
      (Dsl.Runtime.initialize grammar)
      (key "d")
      (context ~selected:true ())
  in
  expect (prefix_effects = []) "guard was evaluated before its input completed";
  expect
    (Model_status.pending_input (Dsl.Runtime.status pending) = Some "d")
    "guarded prefix was not inspectable";
  let _, completed_effects =
    Dsl.Runtime.handle_input pending (key "w") (context ~selected:true ())
  in
  expect
    (List.map Model_effect.identity completed_effects
    = [ "execute apply:current-selections:delete" ])
    "guard was not evaluated after a completed prefix";
  let no_else = compile no_else_source in
  let state, effects =
    Dsl.Runtime.handle_input
      (Dsl.Runtime.initialize no_else)
      (key "x") (context ())
  in
  expect (effects = []) "a failed guard without else did not consume as a no-op";
  expect
    (Model_status.id (Dsl.Runtime.status state) = "direct")
    "a failed guard without else left its stable state";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a state a { status { label "A" input keys } on "x" when selection.any_nonempty -> a on "x" when selection.any_nonempty -> a } }|}
    "duplicate guard";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a state a { status { label "A" input keys } on "x" else -> a on "x" else -> a } }|}
    "duplicate `else`";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a state a { status { label "A" input keys } on "x" -> a on "x" when selection.any_nonempty -> a } }|}
    "unguarded transition cannot coexist"

let test_commands () =
  let grammar = compile command_source in
  let _, effects =
    Dsl.Runtime.handle_input
      (Dsl.Runtime.initialize grammar)
      (key "m") (context ())
  in
  expect
    (List.map Model_effect.identity effects
    = [ "invoke editor.selection.merge-consecutive" ])
    "command transition did not return the ordinary command effect";
  let trace = Trace.enabled ~capacity:16 |> must in
  let document =
    Document.create
      ~id:(Document_id.of_string "dsl-v11-command" |> must)
      ~contents:"alpha" ()
    |> must
  in
  Dsl.Runtime.Adapter.configure grammar;
  let runtime =
    Fun.protect ~finally:Dsl.Runtime.Adapter.clear (fun () ->
        Runtime.create ~commands:(registry ()) ~trace ~document () |> must)
  in
  let runtime, _ = Runtime.handle_input runtime (key "m") |> must in
  expect
    (List.exists
       (function
         | Trace_event.Command_invoked
             { command_id = "editor.selection.merge-consecutive"; _ } ->
             true
         | _ -> false)
       (Trace.events (Runtime.trace runtime)))
    "command invocation was not recorded through the ordinary runtime trace";
  let description = Dsl.Describe.render ~warnings:[] grammar in
  expect
    (contains description "command \"editor.selection.merge-consecutive\"")
    "model-describe omitted command invocation";
  expect_diagnostic ~commands:Command_registry.empty command_source
    "unknown command `editor.selection.merge-consecutive`";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a state a { status { label "A" input keys } on "x" -> a { command "editor.selection.select-regex" } } }|}
    "requires arguments";
  expect_diagnostic
    {|zenbu-model 1
model "x" { title "X" initial a state a { status { label "A" input keys } on "x" -> a { command "editor.apply" } } }|}
    "not eligible for .zenmodel";
  expect_diagnostic_without_commands command_source
    "requires a host command registry"

let tests =
  [
    ("compile-time actions", test_actions);
    ("deterministic guards", test_guards);
    ("validated no-argument commands", test_commands);
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
