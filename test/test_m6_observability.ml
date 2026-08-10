open Zenbu_kernel
open Zenbu_model_api
open Zenbu_proof_models
open Zenbu_structural_model

exception Test_failure of string

let failf format = Printf.ksprintf (fun message -> raise (Test_failure message)) format

let expect condition format =
  Printf.ksprintf
    (fun message -> if not condition then raise (Test_failure message))
    format

let must = function Ok value -> value | Error error -> failf "%s" (Error.to_string error)

let document ?(selections = []) id contents =
  Document_id.of_string id |> must
  |> fun id -> Document.create ~id ~contents ~initial_selections:selections () |> must

let key text = Input_event.logical_text text |> must |> Input_event.key_press
let contains text fragment = String.contains text fragment.[0] && String.length text >= String.length fragment &&
  let rec loop index =
    if index + String.length fragment > String.length text then false
    else if String.sub text index (String.length fragment) = fragment then true
    else loop (index + 1)
  in
  loop 0

let registry () =
  Command_registry.register Command_registry.empty Semantic_commands.apply_command
  |> must

module Vim_runtime = Model_runtime.Make (Vim_model)
module Selection_runtime = Model_runtime.Make (Selection_model)
module Structural_runtime = Model_runtime.Make (Structural_model)

let trace () = Trace.enabled ~capacity:128 |> must
let profiler () = Profiler.enabled ~capacity:32 |> must

let test_trace_bounds_and_disabled () =
  let disabled = Trace.disabled () in
  Trace.emit_lazy disabled (fun () -> Trace_event.Input_received { execution_id = 1; input = "x" });
  expect (Trace.events disabled = []) "disabled trace recorded an event";
  let trace = Trace.enabled ~capacity:2 |> must in
  List.iter
    (fun execution_id ->
      Trace.emit_lazy trace (fun () ->
          Trace_event.Input_received { execution_id; input = string_of_int execution_id }))
    [ 1; 2; 3 ];
  let ids = Trace.events trace |> List.map Trace_event.execution_id in
  expect (ids = [ 2; 3 ]) "trace eviction did not retain newest event order"

let test_provenance_and_why () =
  let runtime =
    Vim_runtime.create ~commands:(registry ()) ~trace:(trace ()) ~profiler:(profiler ())
      ~document:(document "vim-observe" "alpha beta") ()
    |> must
  in
  let runtime, pending = Vim_runtime.handle_input runtime (key "d") |> must in
  expect (Vim_runtime.change_ids pending = []) "pending delete committed a transaction";
  let pending_why =
    Inspector.why (Vim_runtime.trace runtime)
      ~execution_id:(Vim_runtime.execution_id pending)
    |> Option.get
  in
  let pending_text = Inspector.format_why pending_why |> String.concat "\n" in
  expect (contains pending_text "normal -> operator-pending")
    "pending explanation omitted generic transition";
  let runtime, changed = Vim_runtime.handle_input runtime (key "w") |> must in
  expect (List.length (Vim_runtime.change_ids changed) = 1)
    "dw did not record one change";
  let history_change = History.current_change (Vim_runtime.history runtime) |> Option.get in
  let provenance =
    History.transaction history_change |> Transaction.metadata_of
    |> Transaction.provenance |> Option.get
  in
  let entries = Provenance.entries provenance in
  expect
    (List.exists (function Provenance.Model { id = "zenbu.vim-style"; _ } -> true | _ -> false) entries)
    "transaction provenance omitted the model";
  expect
    (List.exists (function Provenance.Command { id = "editor.apply"; _ } -> true | _ -> false) entries)
    "transaction provenance omitted command origin";
  expect
    (List.exists (function Provenance.Selector "next-word" -> true | _ -> false) entries)
    "transaction provenance omitted selector identity";
  expect
    (List.exists (function Provenance.Transformation "delete" -> true | _ -> false) entries)
    "transaction provenance omitted transformation identity";
  let why =
    Inspector.why (Vim_runtime.trace runtime)
      ~execution_id:(Vim_runtime.execution_id changed)
    |> Option.get
  in
  let text = Inspector.format_why why |> String.concat "\n" in
  expect (contains text "selector: next-word") "why omitted selector";
  expect (contains text "transformation: delete") "why omitted transformation";
  expect (contains text "committed:") "why omitted transaction"

let test_input_rules_and_selection_atomicity () =
  let runtime =
    Vim_runtime.create ~commands:(registry ()) ~document:(document "rules" "alpha") ()
    |> must
  in
  let runtime, _ = Vim_runtime.handle_input runtime (key "d") |> must in
  let rules = Vim_runtime.input_rules runtime in
  expect
    (List.exists (fun rule -> Input_rule.pattern rule = Input_rule.Exact "w") rules)
    "pending Vim state omitted w rule";
  let runtime =
    Selection_runtime.create ~commands:(registry ()) ~trace:(trace ())
      ~document:(document "selection-observe" "foo bar foo baz foo") ()
    |> must
  in
  let runtime, _ = Selection_runtime.handle_input runtime (key "W") |> must in
  let runtime, _ = Selection_runtime.handle_input runtime (key "*") |> must in
  let runtime, deleted = Selection_runtime.handle_input runtime (key "d") |> must in
  let change = History.current_change (Selection_runtime.history runtime) |> Option.get |> Inspector.change in
  expect (Inspector.change_edit_count change = 3)
    "multi-selection delete did not preserve three atomic edits";
  let why =
    Inspector.why (Selection_runtime.trace runtime)
      ~execution_id:(Selection_runtime.execution_id deleted)
    |> Option.get |> Inspector.format_why |> String.concat "\n"
  in
  expect (contains why "(3 selections)")
    "multi-selection why omitted resolved target count"

let test_structural_provenance_and_syntax_status () =
  let language = Zenbu_syntax.Syntax.Language.find "ocaml" |> Option.get in
  let service = Zenbu_syntax.Syntax.Service.create language in
  let runtime =
    Structural_runtime.create ~commands:(registry ()) ~syntax_service:service
      ~trace:(trace ()) ~document:(document "structural-observe" "let alpha = 1\n") ()
    |> must
  in
  let runtime, focused = Structural_runtime.handle_input runtime (key "f") |> must in
  let change = History.current_change (Structural_runtime.history runtime) |> Option.get in
  let provenance =
    History.transaction change |> Transaction.metadata_of
    |> Transaction.provenance |> Option.get |> Provenance.entries
  in
  expect
    (List.exists (function Provenance.Selector "syntax.focus" -> true | _ -> false) provenance)
    "structural focus omitted syntax selector provenance";
  let why =
    Inspector.why (Structural_runtime.trace runtime)
      ~execution_id:(Structural_runtime.execution_id focused)
    |> Option.get |> Inspector.format_why |> String.concat "\n"
  in
  expect (contains why "syntax: ocaml") "structural why omitted syntax refresh";
  let status = Inspector.syntax_service service in
  expect (Inspector.syntax_service_cached_version status = Some 1)
    "syntax service status omitted cached document version";
  expect (Option.is_some (Inspector.syntax_service_last_strategy status))
    "syntax service status omitted strategy"

let test_history_view_and_descriptor_registry () =
  let history = History.create (document "history-observe" "abcdef") in
  let history =
    History.apply_intent ~source:Transaction.Test history (Intent.Insert_text "x")
    |> must
  in
  let lineage_change = History.current_change history |> Option.get in
  let history = History.undo history |> must in
  let history =
    History.apply_intent ~source:Transaction.Test history (Intent.Delete_selected_ranges)
    |> must
  in
  let view = Inspector.history ~saved_version:1 history in
  expect (List.length (Inspector.history_nodes view) = 3)
    "history view omitted a branch node";
  expect (Option.is_some (Inspector.find_change view (History.change_id lineage_change)))
    "history view could not find retained branch change";
  let descriptor = List.hd (Selector.descriptors ()) in
  let registry = Semantic_registry.register Semantic_registry.empty descriptor |> must in
  expect
    (Result.is_error (Semantic_registry.register registry descriptor))
    "semantic descriptor registry accepted a duplicate id"

let test_profiler_is_bounded_and_nonsemantic () =
  let disabled = Profiler.disabled () in
  ignore (Profiler.measure disabled Profiler.Model_handle (fun () -> 1));
  expect (Profiler.aggregates disabled = []) "disabled profiler retained samples";
  let profiler = Profiler.enabled ~capacity:2 |> must in
  for _ = 1 to 3 do
    ignore (Profiler.measure profiler Profiler.Model_handle (fun () -> 1))
  done;
  let aggregate = Profiler.aggregates profiler |> List.hd in
  expect (Profiler.aggregate_count aggregate = 2)
    "profile capacity did not evict oldest samples";
  expect (Profiler.aggregate_max_seconds aggregate >= 0.)
    "profile reported a negative duration";
  let with_profile =
    Vim_runtime.create ~commands:(registry ()) ~profiler
      ~document:(document "profiled" "alpha") () |> must
    |> fun runtime -> Vim_runtime.handle_input runtime (key "i") |> must |> fst
    |> fun runtime -> Vim_runtime.handle_input runtime (Input_event.text_input "X" |> must) |> must |> fst
  in
  let without_profile =
    Vim_runtime.create ~commands:(registry ()) ~document:(document "plain" "alpha") ()
    |> must
    |> fun runtime -> Vim_runtime.handle_input runtime (key "i") |> must |> fst
    |> fun runtime -> Vim_runtime.handle_input runtime (Input_event.text_input "X" |> must) |> must |> fst
  in
  expect
    (Editor_context.contents (Vim_runtime.context with_profile)
    = Editor_context.contents (Vim_runtime.context without_profile))
    "profiling changed semantic document output"

let run name test =
  try
    test ();
    Printf.printf "ok - %s\n" name
  with
  | Test_failure message ->
      Printf.eprintf "not ok - %s: %s\n" name message;
      exit 1
  | exception_ ->
      Printf.eprintf "not ok - %s: unexpected %s\n" name (Printexc.to_string exception_);
      exit 1

let () =
  [
    ("trace bounds and disabled", test_trace_bounds_and_disabled);
    ("provenance and why", test_provenance_and_why);
    ("input rules and selection atomicity", test_input_rules_and_selection_atomicity);
    ("structural provenance and syntax status", test_structural_provenance_and_syntax_status);
    ("history view and descriptor registry", test_history_view_and_descriptor_registry);
    ("profiler bounded and nonsemantic", test_profiler_is_bounded_and_nonsemantic);
  ]
  |> List.iter (fun (name, test) -> run name test)
