open Zenbu_kernel
open Zenbu_model_api
module App = Zenbu_app
module Scripting = Zenbu_scripting.Scripting

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

let contains text fragment =
  let text_length = String.length text in
  let fragment_length = String.length fragment in
  let rec loop offset =
    if offset + fragment_length > text_length then false
    else if String.sub text offset fragment_length = fragment then true
    else loop (offset + 1)
  in
  fragment_length = 0 || loop 0

let read path =
  let channel = open_in_bin path in
  Fun.protect
    ~finally:(fun () -> close_in_noerr channel)
    (fun () -> really_input_string channel (in_channel_length channel))

let artifact path =
  let candidates =
    [
      path;
      Filename.concat ".." path;
      Filename.concat "../.." path;
      Filename.concat "../../.." path;
    ]
    @ Option.to_list
        (Option.map
           (fun root -> Filename.concat root path)
           (Sys.getenv_opt "DUNE_SOURCEROOT"))
  in
  match List.find_opt Sys.file_exists candidates with
  | Some path -> path
  | None -> failf "missing workload artifact %s" path

type outcome = Supported | Partial | Rejected

type expected = {
  contents : string;
  selections : (int * int) list;
  primary_selection : int;
  viewport_top_line : int;
  pane_count : int;
  status : string;
  trace : string list;
  provenance : string list;
}

type scenario = {
  id : string;
  editor : string;
  outcome : outcome;
  source : string;
  boundary : string;
  limitations : string list;
  capability_issue : string option;
  model : App.Session.model;
  adapter : string option;
  contents : string;
  inputs : Input_event.t list;
  dimensions : Zenbu_view.Renderer.dimensions;
  expected : expected;
}

let table = function
  | Otoml.TomlTable values | Otoml.TomlInlineTable values -> Some values
  | _ -> None

let field values name = List.assoc_opt name values

let string_field path values name =
  match field values name with
  | Some (Otoml.TomlString value) when String.length value > 0 -> value
  | _ -> failf "%s: %s must be a nonempty string" path name

let optional_string_field path values name =
  match field values name with
  | None -> None
  | Some (Otoml.TomlString value) when String.length value > 0 -> Some value
  | _ -> failf "%s: %s must be a nonempty string when present" path name

let integer_field path values name =
  match field values name with
  | Some (Otoml.TomlInteger value) -> value
  | _ -> failf "%s: %s must be an integer" path name

let strings_field ?(nonempty = true) path values name =
  match field values name with
  | Some (Otoml.TomlArray values) ->
      let rec collect result = function
        | [] -> List.rev result
        | Otoml.TomlString value :: rest when String.length value > 0 ->
            collect (value :: result) rest
        | _ -> failf "%s: %s must contain only nonempty strings" path name
      in
      let result = collect [] values in
      if nonempty && result = [] then failf "%s: %s must not be empty" path name;
      result
  | _ -> failf "%s: %s must be an array of nonempty strings" path name

let outcome_of_string path = function
  | "supported" -> Supported
  | "partial" -> Partial
  | "rejected" -> Rejected
  | value ->
      failf "%s: outcome must be supported, partial, or rejected (got %S)" path
        value

let model_of_string path = function
  | "vim" -> App.Session.Vim
  | "selection" -> App.Session.Selection
  | "direct" -> App.Session.Direct
  | value -> failf "%s: unsupported workload model %S" path value

let named_key = function
  | "PageUp" -> Some Input_event.Page_up
  | "PageDown" -> Some Input_event.Page_down
  | "ArrowUp" -> Some Input_event.Arrow_up
  | "ArrowDown" -> Some Input_event.Arrow_down
  | "ArrowLeft" -> Some Input_event.Arrow_left
  | "ArrowRight" -> Some Input_event.Arrow_right
  | "Escape" -> Some Input_event.Escape
  | "Enter" -> Some Input_event.Enter
  | "Backspace" -> Some Input_event.Backspace
  | _ -> None

let logical_key path ?(modifiers = []) text =
  match Input_event.logical_text text with
  | Ok key -> Input_event.key_press ~modifiers key
  | Error error ->
      failf "%s: invalid logical input %S: %s" path text (Error.to_string error)

let input_of_string path value =
  let named ?(modifiers = []) name =
    match named_key name with
    | Some key -> Input_event.key_press ~modifiers (Input_event.named_key key)
    | None -> failf "%s: unsupported named input %S" path name
  in
  match String.split_on_char '-' value with
  | [ "Ctrl"; text ] ->
      logical_key path ~modifiers:[ Input_event.Control ]
        (String.lowercase_ascii text)
  | [ "Shift"; name ] -> named ~modifiers:[ Input_event.Shift ] name
  | [ "Ctrl"; "Shift"; text ] ->
      logical_key path
        ~modifiers:[ Input_event.Control; Input_event.Shift ]
        (String.lowercase_ascii text)
  | [ value ] -> (
      match named_key value with
      | Some key -> Input_event.key_press (Input_event.named_key key)
      | None -> logical_key path value)
  | _ -> failf "%s: unsupported input syntax %S" path value

let selection_of_string path value =
  match String.split_on_char ':' value with
  | [ anchor; head ] -> (
      match (int_of_string_opt anchor, int_of_string_opt head) with
      | Some anchor, Some head when anchor >= 0 && head >= 0 -> (anchor, head)
      | _ -> failf "%s: invalid nonnegative selection %S" path value)
  | _ -> failf "%s: selections must have anchor:head form (got %S)" path value

let expected_of_table path values =
  {
    contents = string_field path values "contents";
    selections =
      strings_field path values "selections"
      |> List.map (selection_of_string path);
    primary_selection = integer_field path values "primary_selection";
    viewport_top_line = integer_field path values "viewport_top_line";
    pane_count = integer_field path values "pane_count";
    status = string_field path values "status";
    trace = strings_field path values "trace";
    provenance = strings_field ~nonempty:false path values "provenance";
  }

let scenario_of_file path =
  let root =
    match Otoml.Parser.from_file_result path with
    | Ok document -> (
        match table document with
        | Some root -> root
        | None -> failf "%s: root must be a TOML table" path)
    | Error message -> failf "%s: %s" path message
  in
  let expected =
    match field root "expect" with
    | Some value -> (
        match table value with
        | Some values -> expected_of_table path values
        | None -> failf "%s: [expect] must be a TOML table" path)
    | None -> failf "%s: missing [expect] table" path
  in
  let columns = integer_field path root "columns" in
  let rows = integer_field path root "rows" in
  if columns <= 0 || rows <= 0 then
    failf "%s: columns and rows must be positive" path;
  if
    expected.primary_selection < 0
    || expected.primary_selection >= List.length expected.selections
  then failf "%s: primary_selection is outside selections" path;
  {
    id = string_field path root "id";
    editor = string_field path root "editor";
    outcome = outcome_of_string path (string_field path root "outcome");
    source = string_field path root "source";
    boundary = string_field path root "boundary";
    limitations = strings_field path root "limitations";
    capability_issue = optional_string_field path root "capability_issue";
    model = model_of_string path (string_field path root "model");
    adapter = optional_string_field path root "adapter";
    contents = string_field path root "contents";
    inputs = strings_field path root "inputs" |> List.map (input_of_string path);
    dimensions = Zenbu_view.Renderer.{ columns; rows };
    expected;
  }

let expected_source_prefix = function
  | "vim" -> "https://vimhelp.org/"
  | "helix" -> "https://docs.helix-editor.com/"
  | "kakoune" -> "https://github.com/mawww/kakoune/"
  | "micro" -> "https://github.com/micro-editor/micro/"
  | "emacs" -> "https://www.gnu.org/"
  | editor -> failf "unknown editor workload %S" editor

let valid_issue_url value =
  let prefix = "https://github.com/gongahkia/zenbu/issues/" in
  String.starts_with ~prefix value
  &&
  let suffix =
    String.sub value (String.length prefix)
      (String.length value - String.length prefix)
  in
  match int_of_string_opt suffix with
  | Some number -> number > 0
  | None -> false

let validate_metadata documentation scenario =
  expect
    (String.starts_with
       ~prefix:(expected_source_prefix scenario.editor)
       scenario.source)
    "%s: source %S is not an authoritative %s documentation URL" scenario.id
    scenario.source scenario.editor;
  expect
    (String.length scenario.boundary > 0)
    "%s: missing Zenbu boundary" scenario.id;
  expect
    (contains documentation scenario.id)
    "%s: docs/EDITOR_WORKLOAD_EVALUATION.md does not name the fixture"
    scenario.id;
  List.iter
    (fun limitation ->
      expect
        (contains documentation limitation)
        "%s: declared limitation is not present in the workload evaluation: %S"
        scenario.id limitation)
    scenario.limitations;
  match (scenario.outcome, scenario.capability_issue) with
  | Rejected, Some issue ->
      expect (valid_issue_url issue)
        "%s: rejected workload needs a Zenbu GitHub capability issue URL"
        scenario.id
  | Rejected, None ->
      failf "%s: rejected workload needs a linked atomic capability issue"
        scenario.id
  | (Supported | Partial), None -> ()
  | (Supported | Partial), Some _ ->
      failf "%s: only rejected workloads may declare capability_issue"
        scenario.id

let selection_offsets session =
  App.Session.context session |> Editor_context.selections |> fun selections ->
  List.map
    (fun (selection : Editor_context.selection) ->
      (selection.anchor_offset, selection.head_offset))
    selections.selections

let primary_selection session =
  App.Session.context session |> Editor_context.selections |> fun selections ->
  selections.primary_index

let configuration scenario =
  match scenario.adapter with
  | None -> Scripting.Disabled
  | Some adapter -> Scripting.Explicit (artifact adapter)

let trace_contains trace expected =
  match String.split_on_char ':' expected with
  | [ "binding"; command_id ] ->
      List.exists
        (function
          | Trace_event.Binding_resolved { command_id = actual; _ } ->
              String.equal actual command_id
          | _ -> false)
        (Trace.events trace)
  | [ "command"; command_id ] ->
      List.exists
        (function
          | Trace_event.Command_invoked { command_id = actual; _ } ->
              String.equal actual command_id
          | _ -> false)
        (Trace.events trace)
  | [ "transformation"; transformation_id ] ->
      List.exists
        (function
          | Trace_event.Transformation_applied { transformation_id = actual; _ }
            ->
              String.equal actual transformation_id
          | _ -> false)
        (Trace.events trace)
  | [ "input"; "colon" ] ->
      List.exists
        (function
          | Trace_event.Input_received { input = actual; _ } ->
              String.equal actual "text(:)"
          | _ -> false)
        (Trace.events trace)
  | [ "transaction"; "committed" ] ->
      List.exists
        (function Trace_event.Transaction_committed _ -> true | _ -> false)
        (Trace.events trace)
  | _ -> failf "unsupported trace expectation %S" expected

let run scenario =
  let trace = Trace.enabled ~capacity:128 |> must in
  let session =
    App.Session.create ~model:scenario.model ~contents:scenario.contents ~trace
      ~config:(configuration scenario) ~dimensions:scenario.dimensions ()
    |> must
  in
  Fun.protect
    ~finally:(fun () -> App.Session.close session)
    (fun () ->
      let session =
        List.fold_left App.Session.handle_input session scenario.inputs
      in
      let expected = scenario.expected in
      expect
        (String.equal (App.Session.contents session) expected.contents)
        "%s: contents differ\nexpected: %S\nactual:   %S" scenario.id
        expected.contents
        (App.Session.contents session);
      expect
        (selection_offsets session = expected.selections)
        "%s: selections differ" scenario.id;
      expect
        (primary_selection session = expected.primary_selection)
        "%s: primary selection differs" scenario.id;
      expect
        ((App.Session.viewport session).top_line = expected.viewport_top_line)
        "%s: viewport top line expected %d, got %d" scenario.id
        expected.viewport_top_line (App.Session.viewport session).top_line;
      expect
        (App.Session.pane_count session = expected.pane_count)
        "%s: pane count expected %d, got %d" scenario.id expected.pane_count
        (App.Session.pane_count session);
      expect
        (String.equal
           (Model_status.id (App.Session.status session))
           expected.status)
        "%s: status expected %s, got %s" scenario.id expected.status
        (Model_status.id (App.Session.status session));
      List.iter
        (fun event ->
          expect
            (trace_contains trace event)
            "%s: command trace does not contain %S" scenario.id event)
        expected.trace;
      let why =
        App.Session.inspect session App.Session.Why |> String.concat "\n"
      in
      List.iter
        (fun fragment ->
          expect (contains why fragment)
            "%s: trace/provenance does not contain %S\n%s" scenario.id fragment
            why)
        expected.provenance)

let workload_files () =
  let directory = artifact "test/fixtures/workloads" in
  Sys.readdir directory |> Array.to_list
  |> List.filter (fun name -> Filename.check_suffix name ".toml")
  |> List.sort String.compare
  |> List.map (Filename.concat directory)

let expect_coverage scenarios =
  let editors = List.map (fun scenario -> scenario.editor) scenarios in
  List.iter
    (fun editor ->
      expect (List.mem editor editors) "missing %s-style workload fixture"
        editor)
    [ "vim"; "helix"; "kakoune"; "micro"; "emacs" ];
  let outcomes = List.map (fun scenario -> scenario.outcome) scenarios in
  List.iter
    (fun outcome ->
      expect
        (List.mem outcome outcomes)
        "fixture suite does not record every supported, partial, and rejected \
         outcome")
    [ Supported; Partial; Rejected ]

let () =
  try
    let documentation = artifact "docs/EDITOR_WORKLOAD_EVALUATION.md" |> read in
    let scenarios = workload_files () |> List.map scenario_of_file in
    expect (scenarios <> []) "workload fixture directory is empty";
    expect_coverage scenarios;
    List.iter
      (fun scenario ->
        validate_metadata documentation scenario;
        run scenario;
        Printf.printf "ok: %s (%s)\n" scenario.id
          (match scenario.outcome with
          | Supported -> "supported"
          | Partial -> "partial"
          | Rejected -> "rejected"))
      scenarios
  with Test_failure message ->
    Printf.eprintf "FAILED: %s\n" message;
    exit 1
