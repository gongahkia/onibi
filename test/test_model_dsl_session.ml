open Zenbu_kernel
open Zenbu_model_api

module App = Zenbu_app
module Dsl = Zenbu_model_dsl

exception Test_failure of string

let failf format = Printf.ksprintf (fun message -> raise (Test_failure message)) format

let expect condition format =
  Printf.ksprintf
    (fun message -> if not condition then raise (Test_failure message))
    format

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

let compile () =
  match Dsl.Compile.compile ~source_name:"session.zenmodel" ~source with
  | Ok (grammar, warnings) ->
      expect (warnings = []) "the session grammar unexpectedly produced warnings";
      grammar
  | Error diagnostics ->
      diagnostics |> List.map Dsl.Diagnostic.format |> String.concat "\n" |> failf "%s"

let key value = Input_event.logical_text value |> must |> Input_event.key_press
let named value = Input_event.named_key value |> Input_event.key_press
let text value = Input_event.text_input value |> must

let dimensions = Zenbu_view.Renderer.{ columns = 100; rows = 24 }

let create ?trace ?(contents = "alpha beta") grammar =
  App.Session.create ~model:App.Session.Dsl ~dsl_model:grammar ~contents
    ?trace ~config:Zenbu_scripting.Scripting.Disabled ~dimensions ()
  |> must

let contains text fragment =
  let text_length = String.length text in
  let fragment_length = String.length fragment in
  let rec loop offset =
    if offset + fragment_length > text_length then false
    else if String.sub text offset fragment_length = fragment then true
    else loop (offset + 1)
  in
  fragment_length = 0 || loop 0

let lines_contain lines fragment = List.exists (fun line -> contains line fragment) lines

let test_requires_a_compiled_grammar () =
  match
    App.Session.create ~model:App.Session.Dsl
      ~config:Zenbu_scripting.Scripting.Disabled ~dimensions ()
  with
  | Error (Error.Invalid_command_arguments message) ->
      expect (contains message "--model-dsl")
        "DSL startup reported an unhelpful missing-grammar error: %s" message
  | Error error -> failf "DSL startup returned the wrong error: %s" (Error.to_string error)
  | Ok _ -> failf "DSL startup succeeded without a compiled grammar"

let test_status_bindings_and_text_edit () =
  let trace = Trace.enabled ~capacity:32 |> must in
  let session = create ~trace (compile ()) in
  expect (App.Session.model session = App.Session.Dsl)
    "session did not retain the DSL active-model identity";
  expect (Model_status.label (App.Session.status session) = "NORMAL")
    "DSL session did not start in NORMAL";
  let bindings = App.Session.inspect session App.Session.Bindings in
  expect (lines_contain bindings "Minimal modal / NORMAL")
    "generic binding inspection omitted the DSL model status";
  expect (lines_contain bindings "d")
    "generic binding inspection omitted the DSL prefix";
  let session = App.Session.handle_input session (key "i") in
  expect (Model_status.label (App.Session.status session) = "INSERT")
    "normal + i did not enter INSERT";
  expect
    (lines_contain (App.Session.inspect session App.Session.Why)
       "model: zenbu.example.modal / NORMAL")
    "the grammar model ID was not retained in ordinary model tracing";
  let session = App.Session.handle_input session (text "界") in
  expect (App.Session.contents session = "界alpha beta")
    "captured committed text did not traverse the normal runtime transaction path";
  let session = App.Session.handle_input session (named Input_event.Escape) in
  expect (Model_status.label (App.Session.status session) = "NORMAL")
    "Escape did not return the interactive session to NORMAL"

let test_prefix_execution_and_mismatch () =
  let grammar = compile () in
  let session = create grammar in
  let session = App.Session.handle_input session (key "d") in
  expect (App.Session.contents session = "alpha beta")
    "a DSL prefix edited the document before completion";
  expect (Model_status.pending_input (App.Session.status session) = Some "d")
    "DSL prefix is not exposed by the interactive status";
  let pending_bindings = App.Session.inspect session App.Session.Bindings in
  expect (lines_contain pending_bindings "w")
    "generic binding inspection did not expose the pending continuation";
  let session = App.Session.handle_input session (key "x") in
  expect (App.Session.contents session = "alpha beta")
    "a mismatching prefix input was reprocessed as an edit";
  expect (Model_status.pending_input (App.Session.status session) = None)
    "a mismatching prefix input did not clear the pending state";
  let session = App.Session.handle_input (create grammar) (key "d") in
  let session = App.Session.handle_input session (key "w") in
  expect (App.Session.contents session = " beta")
    "d w did not delete the current word through Model_runtime";
  expect
    (lines_contain (App.Session.inspect session App.Session.History) "edits=1")
    "d w did not produce an ordinary history transaction";
  expect (Model_status.label (App.Session.status session) = "NORMAL")
    "d w did not return to the target DSL state";
  expect (Model_status.pending_input (App.Session.status session) = None)
    "d w did not clear the interactive pending prefix"

let test_builtin_model_creation_is_unchanged () =
  let session =
    App.Session.create ~model:App.Session.Vim ~contents:"alpha"
      ~config:Zenbu_scripting.Scripting.Disabled ~dimensions ()
    |> must
  in
  expect (App.Session.model session = App.Session.Vim)
    "built-in model selection regressed while adding DSL selection"

let tests =
  [
    ("DSL session requires a compiled grammar", test_requires_a_compiled_grammar);
    ("DSL session status, inspection, and text editing", test_status_bindings_and_text_edit);
    ("DSL session prefix execution and mismatch", test_prefix_execution_and_mismatch);
    ("built-in session selection remains unchanged", test_builtin_model_creation_is_unchanged);
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
