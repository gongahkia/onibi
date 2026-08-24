open Zenbu_kernel
open Zenbu_model_api
module App = Zenbu_app
module Frame = Zenbu_view.Frame
module Renderer = Zenbu_view.Renderer
module Syntax = Zenbu_syntax.Syntax
module Terminal = Zenbu_terminal

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

let expect_error = function Error _ -> () | Ok _ -> failf "expected an error"
let key text = Input_event.logical_text text |> must |> Input_event.key_press
let text_input text = Input_event.text_input text |> must
let named value = Input_event.key_press (Input_event.named_key value)

let ctrl text =
  Input_event.logical_text text
  |> must
  |> Input_event.key_press ~modifiers:[ Input_event.Control ]

let dimensions = Renderer.{ columns = 100; rows = 20 }

let contains text fragment =
  let text_length = String.length text in
  let fragment_length = String.length fragment in
  let rec loop offset =
    if offset + fragment_length > text_length then false
    else if String.sub text offset fragment_length = fragment then true
    else loop (offset + 1)
  in
  fragment_length = 0 || loop 0

let lines_contain lines fragment =
  List.exists (fun line -> contains line fragment) lines

let make_session ?(model = App.Session.Vim) ?language ?trace ?system_clipboard
    ?(config = Zenbu_scripting.Scripting.Disabled) ?(dimensions = dimensions)
    contents =
  App.Session.create ~model ?language ?trace ?system_clipboard ~contents ~config
    ~dimensions ()
  |> must

let continue = function
  | App.Session.Continue value -> value
  | App.Session.Exit _ -> failf "host command unexpectedly exited"

let primary_offsets session =
  let selections = App.Session.context session |> Editor_context.selections in
  let primary = List.nth selections.selections selections.primary_index in
  (primary.anchor_offset, primary.head_offset)

let selection_offsets session =
  App.Session.context session |> Editor_context.selections |> fun selections ->
  List.map
    (fun (selection : Editor_context.selection) ->
      (selection.anchor_offset, selection.head_offset))
    selections.selections

let invoke_palette_text_argument session command value =
  let session =
    App.Session.handle_host session App.Session.Open_palette |> continue
  in
  let session = App.Session.handle_input session (text_input command) in
  let session = App.Session.handle_input session (named Input_event.Enter) in
  expect
    (Model_status.id (App.Session.status session) = "host-command-argument")
    "command %s did not open its required text prompt" command;
  let session = App.Session.handle_input session (text_input value) in
  App.Session.handle_input session (named Input_event.Enter)

let invoke_palette_text_arguments session command values =
  let session =
    App.Session.handle_host session App.Session.Open_palette |> continue
  in
  let session = App.Session.handle_input session (text_input command) in
  let session = App.Session.handle_input session (named Input_event.Enter) in
  List.fold_left
    (fun session value ->
      expect
        (Model_status.id (App.Session.status session) = "host-command-argument")
        "command %s did not open its required text prompt" command;
      let session = App.Session.handle_input session (text_input value) in
      App.Session.handle_input session (named Input_event.Enter))
    session values

let invoke_command_line session line =
  let session =
    App.Session.handle_host session App.Session.Open_command_line |> continue
  in
  expect
    (Model_status.id (App.Session.status session) = "host-command-line")
    "command line did not open its dedicated prompt";
  let session = App.Session.handle_input session (text_input line) in
  App.Session.handle_input session (named Input_event.Enter)

let test_unicode_search_is_host_level_and_observable () =
  let trace = Trace.enabled ~capacity:64 |> must in
  let session = make_session ~trace "α beta α beta" in
  let session =
    App.Session.handle_host session App.Session.Start_search |> continue
  in
  expect
    (Model_status.input_mode (App.Session.status session)
    = Model_status.Text_entry)
    "search prompt did not declare text-entry input";
  let session = App.Session.handle_input session (text_input "α") in
  expect
    (App.Session.contents session = "α beta α beta")
    "search mutated the document";
  let search = App.Session.inspect session App.Session.Search in
  expect (lines_contain search "query: α") "search query is not inspectable";
  expect
    (lines_contain search "matches: 2")
    "Unicode search found the wrong number of matches";
  expect
    (lines_contain (App.Session.inspect session App.Session.Why) "host.search")
    "search selection provenance is absent from why";
  let session =
    App.Session.handle_host session App.Session.Search_next |> continue
  in
  let search = App.Session.inspect session App.Session.Search in
  expect
    (lines_contain search "current-match: 2")
    "search-next did not move to the second occurrence";
  let session =
    App.Session.handle_host session App.Session.Search_previous |> continue
  in
  let search = App.Session.inspect session App.Session.Search in
  expect
    (lines_contain search "current-match: 1")
    "search-previous did not wrap through the active query";
  let session = App.Session.handle_input session (named Input_event.Escape) in
  expect
    (lines_contain
       (App.Session.inspect session App.Session.Search)
       "active-query: none")
    "Escape did not cancel the active search";
  expect
    (primary_offsets session = (0, 0))
    "Escape did not restore the pre-search selection";
  let incremental = make_session "alpha beta alpha" in
  let incremental =
    App.Session.handle_host incremental App.Session.Start_search |> continue
  in
  let incremental = App.Session.handle_input incremental (text_input "a") in
  expect
    (lines_contain
       (App.Session.inspect incremental App.Session.Search)
       "matches: 5")
    "incremental single-character search did not update matches";
  let incremental = App.Session.handle_input incremental (text_input "lpha") in
  expect
    (lines_contain
       (App.Session.inspect incremental App.Session.Search)
       "matches: 2")
    "incremental multi-character search did not narrow matches";
  let incremental = App.Session.handle_input incremental (text_input "z") in
  expect
    (lines_contain
       (App.Session.inspect incremental App.Session.Search)
       "matches: 0")
    "a missing literal search did not report zero matches";
  let incremental =
    List.fold_left
      (fun current _ ->
        App.Session.handle_input current (named Input_event.Backspace))
      incremental [ (); () ]
  in
  expect
    (lines_contain
       (App.Session.inspect incremental App.Session.Search)
       "matches: 2")
    "search backspace did not recompute the active literal query";
  let changed = make_session "alpha beta alpha" in
  let changed =
    App.Session.handle_host changed App.Session.Start_search |> continue
  in
  let changed = App.Session.handle_input changed (text_input "alpha") in
  let changed = App.Session.handle_input changed (named Input_event.Enter) in
  let changed = App.Session.handle_input changed (key "G") in
  let changed = App.Session.handle_input changed (key "i") in
  let changed = App.Session.handle_input changed (text_input " alpha") in
  let changed = App.Session.handle_input changed (named Input_event.Escape) in
  expect
    (lines_contain
       (App.Session.inspect changed App.Session.Search)
       "matches: 3")
    "document changes did not refresh active search ranges";
  let long_document =
    List.init 40 (fun _ -> "filler") @ [ "needle" ] |> String.concat "\n"
  in
  let followed =
    make_session ~dimensions:Renderer.{ columns = 24; rows = 3 } long_document
  in
  let followed =
    App.Session.handle_host followed App.Session.Start_search |> continue
  in
  let followed = App.Session.handle_input followed (text_input "needle") in
  let followed, _ = App.Session.render followed in
  expect
    ((App.Session.viewport followed).Zenbu_view.Viewport.top_line > 0)
    "searching a distant match did not move the viewport to the selected result"

let test_regexp_search_is_incremental_and_utf8_safe () =
  let session = make_session "a12 β34 a5" in
  let session =
    App.Session.handle_host session App.Session.Start_regexp_search |> continue
  in
  expect
    (Model_status.input_mode (App.Session.status session)
    = Model_status.Text_entry)
    "regexp search did not declare text-entry input";
  let session = App.Session.handle_input session (text_input "a[0-9][0-9]*") in
  expect
    (primary_offsets session = (0, 3))
    "regexp search did not select its first non-empty match";
  let search = App.Session.inspect session App.Session.Search in
  expect
    (lines_contain search "kind: regexp" && lines_contain search "matches: 2")
    "regexp search is not distinguishable from literal search in inspection";
  let session =
    App.Session.handle_host session App.Session.Search_next |> continue
  in
  expect
    (primary_offsets session = (9, 11))
    "regexp search did not retain repeatable next-match navigation";
  let utf8 = make_session "β" in
  let utf8 =
    App.Session.handle_host utf8 App.Session.Start_regexp_search |> continue
  in
  let utf8 = App.Session.handle_input utf8 (text_input ".") in
  expect
    (primary_offsets utf8 = (0, 0)
    && lines_contain
         (App.Session.inspect utf8 App.Session.Search)
         "active-query: none")
    "a byte-oriented regexp match splitting UTF-8 changed the selection";
  let utf8 =
    App.Session.handle_input utf8 (named Input_event.Backspace)
    |> fun session -> App.Session.handle_input session (text_input "β")
  in
  expect
    (primary_offsets utf8 = (0, 2))
    "regexp search did not recover after rejecting a UTF-8-unsafe pattern";
  let zero_width = make_session "a" in
  let zero_width =
    App.Session.handle_host zero_width App.Session.Start_regexp_search
    |> continue
    |> fun session -> App.Session.handle_input session (text_input "a*")
  in
  expect
    (primary_offsets zero_width = (0, 0)
    && lines_contain
         (App.Session.inspect zero_width App.Session.Search)
         "active-query: none")
    "a zero-width regexp search was accepted as a selectable match"

let test_replace_all_is_atomic_and_utf8_safe () =
  let literal =
    make_session "aaaa" |> fun session ->
    invoke_palette_text_arguments session "search.replace.literal" [ "aa"; "β" ]
  in
  expect
    (App.Session.contents literal = "ββ")
    "literal replace-all did not use leftmost non-overlapping matches";
  expect
    (lines_contain (App.Session.inspect literal App.Session.History) "v0 -> v1"
    && lines_contain (App.Session.inspect literal App.Session.History) "edits=2"
    )
    "literal replace-all did not create a history change";
  expect
    (not
       (lines_contain
          (App.Session.inspect literal App.Session.History)
          "v1 -> v2"))
    "literal replace-all split one user request across history changes";
  expect
    (lines_contain
       (App.Session.inspect literal App.Session.History)
       "host.search.replace"
    && lines_contain
         (App.Session.inspect literal App.Session.History)
         "search.matches"
    && lines_contain
         (App.Session.inspect literal App.Session.History)
         "replace-all")
    "literal replace-all did not retain host selector/transformation provenance";
  let restored = App.Session.handle_input literal (key "u") in
  expect
    (App.Session.contents restored = "aaaa")
    "one undo did not restore the entire literal replace-all operation";
  let regexp =
    make_session "a12 a5 β" |> fun session ->
    invoke_palette_text_arguments session "search.replace.regexp"
      [ "a[0-9][0-9]*"; "$1" ]
  in
  expect
    (App.Session.contents regexp = "$1 $1 β")
    "regexp replace-all did not use non-empty matches or literal replacement \
     text";
  let unsafe =
    make_session "β" |> fun session ->
    invoke_palette_text_arguments session "search.replace.regexp" [ "."; "x" ]
  in
  expect
    (App.Session.contents unsafe = "β"
    && not
         (lines_contain
            (App.Session.inspect unsafe App.Session.History)
            "v0 -> v1"))
    "UTF-8-unsafe regexp replacement mutated the document";
  let zero_width =
    make_session "a" |> fun session ->
    invoke_palette_text_arguments session "search.replace.regexp" [ "a*"; "x" ]
  in
  expect
    (App.Session.contents zero_width = "a"
    && not
         (lines_contain
            (App.Session.inspect zero_width App.Session.History)
            "v0 -> v1"))
    "zero-width regexp replacement mutated the document"

let test_query_replace_is_reviewed_and_atomic_per_decision () =
  let session =
    make_session "aa aa aa" |> fun session ->
    invoke_palette_text_arguments session "search.query-replace.literal"
      [ "aa"; "z" ]
  in
  expect
    (Model_status.id (App.Session.status session) = "host-query-replace")
    "query-replace did not enter its host-owned review state";
  let _, frame = App.Session.render session in
  let screen =
    Frame.rows frame |> List.map Frame.row_text |> String.concat "\n"
  in
  expect
    (contains screen "Query replace" && contains screen "remaining: 3")
    "query-replace did not render its current match and decision state";
  let skipped = App.Session.handle_input session (key "s") in
  expect
    (App.Session.contents skipped = "aa aa aa")
    "skipping a query-replace match changed the document";
  let replaced = App.Session.handle_input skipped (key "r") in
  expect
    (App.Session.contents replaced = "aa z aa")
    "query-replace did not replace its reviewed current match";
  let completed = App.Session.handle_input replaced (key "a") in
  expect
    (App.Session.contents completed = "aa z z")
    "query-replace did not replace all remaining planned matches";
  expect
    (lines_contain
       (App.Session.inspect completed App.Session.History)
       "host.search.query-replace"
    && lines_contain
         (App.Session.inspect completed App.Session.History)
         "query-replace.matches")
    "query-replace did not retain checked host provenance";
  let cancelled =
    make_session "a a" |> fun session ->
    invoke_palette_text_arguments session "search.query-replace.literal"
      [ "a"; "x" ]
    |> fun session -> App.Session.handle_input session (key "q")
  in
  expect
    (App.Session.contents cancelled = "a a")
    "quitting query-replace changed an unreviewed document";
  let unicode =
    make_session "β β" |> fun session ->
    invoke_palette_text_arguments session "search.query-replace.literal"
      [ "β"; "λ" ]
    |> fun session -> App.Session.handle_input session (key "a")
  in
  expect
    (App.Session.contents unicode = "λ λ")
    "query-replace did not preserve UTF-8 match boundaries";
  let invalid =
    make_session "a" |> fun session ->
    invoke_palette_text_arguments session "search.query-replace.regexp"
      [ "a*"; "x" ]
  in
  expect
    (App.Session.contents invalid = "a"
    && Model_status.id (App.Session.status invalid) <> "host-query-replace")
    "query-replace accepted a zero-width regexp match"

let test_vim_modal_search_requests () =
  let session = make_session "alpha beta alpha" in
  let session = App.Session.handle_input session (key "/") in
  expect
    (Model_status.input_mode (App.Session.status session)
    = Model_status.Text_entry)
    "Vim slash did not request the shared search prompt";
  let session = App.Session.handle_input session (text_input "alpha") in
  expect
    (primary_offsets session = (0, 5))
    "Vim forward search did not select the first matching range";
  let session = App.Session.handle_input session (named Input_event.Enter) in
  let session = App.Session.handle_input session (key "n") in
  expect
    (primary_offsets session = (11, 16))
    "Vim n did not request the next shared search match";
  let session = App.Session.handle_input session (key "N") in
  expect
    (primary_offsets session = (0, 5))
    "Vim N did not request the previous shared search match";
  let session = App.Session.handle_input session (key "n") in
  let session = App.Session.handle_input session (key "?") in
  let session = App.Session.handle_input session (text_input "alpha") in
  expect
    (primary_offsets session = (0, 5))
    "Vim backward search did not start from the current caret"

let test_syntax_spans_and_render_precedence () =
  let document =
    Document.create
      ~id:(Document_id.of_string "m10-syntax" |> must)
      ~contents:"let answer = 42 (* note *)\n" ()
    |> must
  in
  let language = Syntax.Language.find "ocaml" |> Option.get in
  let snapshot =
    Syntax.Service.create language |> fun service ->
    Syntax.Service.refresh service (Document.snapshot document) |> Result.get_ok
  in
  let spans = Syntax.Highlight.spans snapshot in
  expect
    (List.exists
       (fun span -> Syntax.Highlight.class_ span = Syntax.Highlight.Keyword)
       spans)
    "OCaml snapshot did not produce a keyword highlight span";
  expect
    (List.exists
       (fun span -> Syntax.Highlight.class_ span = Syntax.Highlight.Number)
       spans)
    "OCaml snapshot did not produce a number highlight span";
  let json_document =
    Document.create
      ~id:(Document_id.of_string "m10-json-syntax" |> must)
      ~contents:"{\"answer\": 42, \"enabled\": true}" ()
    |> must
  in
  let json_language = Syntax.Language.find "json" |> Option.get in
  let json_spans =
    Syntax.Service.create json_language |> fun service ->
    Syntax.Service.refresh service (Document.snapshot json_document)
    |> Result.get_ok |> Syntax.Highlight.spans
  in
  expect
    (List.exists
       (fun span -> Syntax.Highlight.class_ span = Syntax.Highlight.String)
       json_spans)
    "JSON snapshot did not produce a string highlight span";
  expect
    (List.exists
       (fun span -> Syntax.Highlight.class_ span = Syntax.Highlight.Number)
       json_spans)
    "JSON snapshot did not produce a number highlight span";
  let malformed_document =
    Document.create
      ~id:(Document_id.of_string "m10-malformed-syntax" |> must)
      ~contents:"let =" ()
    |> must
  in
  let malformed =
    Syntax.Service.create language |> fun service ->
    Syntax.Service.refresh service (Document.snapshot malformed_document)
    |> Result.get_ok
  in
  expect
    (Syntax.Snapshot.has_error malformed)
    "malformed OCaml did not retain syntax-error state for presentation";
  ignore (Syntax.Highlight.spans malformed);
  let plain = make_session "unclassified plain text" in
  expect
    (Editor_context.syntax (App.Session.context plain) = None)
    "an unknown-language buffer unexpectedly received syntax highlighting";
  let current = make_session ~language:"ocaml" "let value = 1\n" in
  let current = App.Session.handle_input current (key "i") in
  let current = App.Session.handle_input current (text_input "x") in
  let current = App.Session.handle_input current (named Input_event.Escape) in
  let context = App.Session.context current in
  let snapshot = Editor_context.syntax context |> Option.get in
  expect
    (Syntax.Snapshot.document_version snapshot
    = Editor_context.document_version context)
    "rendering would receive a stale syntax snapshot after a document edit";
  let selection = Selection_spec.make ~anchor_offset:0 ~head_offset:3 |> must in
  let selected_document =
    Document.create
      ~id:(Document_id.of_string "m10-render" |> must)
      ~contents:"let" ~initial_selections:[ selection ] ()
    |> must
  in
  let context =
    Editor_context.from_snapshot
      ~snapshot:(Document.snapshot selected_document)
      ~commands:[] ()
  in
  let rendered =
    Renderer.render_with_inspector ~inspector:None
      ~syntax_spans:
        [
          {
            Renderer.start_offset = 0;
            stop_offset = 3;
            class_ = Renderer.Keyword;
          };
        ]
      ~search_ranges:[ { Renderer.start_offset = 0; stop_offset = 3 } ]
      ~context
      ~status:(Model_status.create ~id:"test" ~label:"TEST" () |> must)
      ~filename:"test.ml" ~dirty:false ~message:None
      ~viewport:Zenbu_view.Viewport.origin
      ~dimensions:{ columns = 20; rows = 3 } ()
  in
  let first_cell = Frame.rows rendered.frame |> List.hd |> List.hd in
  expect
    (first_cell.Frame.style = Frame.Primary_selection)
    "selection did not take precedence over search and syntax highlighting";
  let unselected_context =
    let collapsed =
      Selection_spec.make ~anchor_offset:3 ~head_offset:3 |> must
    in
    Document.create
      ~id:(Document_id.of_string "m10-render-search" |> must)
      ~contents:"let" ~initial_selections:[ collapsed ] ()
    |> must |> Document.snapshot
    |> fun snapshot -> Editor_context.from_snapshot ~snapshot ~commands:[] ()
  in
  let rendered =
    Renderer.render_with_inspector ~inspector:None
      ~syntax_spans:
        [
          {
            Renderer.start_offset = 0;
            stop_offset = 3;
            class_ = Renderer.Keyword;
          };
        ]
      ~search_ranges:[ { Renderer.start_offset = 0; stop_offset = 3 } ]
      ~context:unselected_context
      ~status:(Model_status.create ~id:"test" ~label:"TEST" () |> must)
      ~filename:"test.ml" ~dirty:false ~message:None
      ~viewport:Zenbu_view.Viewport.origin
      ~dimensions:{ columns = 20; rows = 3 } ()
  in
  let first_cell = Frame.rows rendered.frame |> List.hd |> List.hd in
  expect
    (first_cell.Frame.style = Frame.Search_match)
    "search did not take precedence over syntax highlighting"

let write path contents =
  let channel = open_out_bin path in
  Fun.protect
    ~finally:(fun () -> close_out_noerr channel)
    (fun () -> output_string channel contents)

let vim_insert session text =
  let session = App.Session.handle_input session (key "i") in
  let session = App.Session.handle_input session (text_input text) in
  App.Session.handle_input session (named Input_event.Escape)

let session_for_file path contents =
  write path contents;
  App.Session.create ~model:App.Session.Vim ~file_path:path ~contents
    ~dimensions ()
  |> must

let test_explicit_startup_failures_remain_inspectable () =
  let missing_config = Filename.temp_file "zenbu-m10-missing-config" ".lua" in
  Sys.remove missing_config;
  let session =
    make_session ~config:(Zenbu_scripting.Scripting.Explicit missing_config)
      "alpha"
  in
  expect
    (Option.is_some (App.Session.configuration_error session))
    "an explicitly missing configuration was not retained as a load error";
  let root = Filename.temp_file "zenbu-m10-invalid-plugin" "" in
  Sys.remove root;
  Unix.mkdir root 0o700;
  let package = Filename.concat root "invalid" in
  Unix.mkdir package 0o700;
  let manifest = Filename.concat package "zenbu-plugin.toml" in
  Fun.protect
    ~finally:(fun () ->
      if Sys.file_exists manifest then Sys.remove manifest;
      Unix.rmdir package;
      Unix.rmdir root)
    (fun () ->
      write manifest "manifest_version = not-an-integer\n";
      let session =
        App.Session.create ~model:App.Session.Vim ~contents:"alpha"
          ~config:Zenbu_scripting.Scripting.Disabled
          ~plugins:(Zenbu_extension.Plugin_host.Directories [ root ])
          ~dimensions ()
        |> must
      in
      expect
        (App.Session.plugin_load_errors session <> [])
        "an invalid explicit plugin package was not retained as a load error")

let test_palette_discovers_all_active_command_providers () =
  let path = Filename.temp_file "zenbu-m10-palette" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.command {
  id = "user.palette-noop",
  title = "Palette no-op",
  description = "A command exposed through the generic palette.",
  run = function(_) return {} end,
}
|};
      let session =
        make_session ~config:(Zenbu_scripting.Scripting.Explicit path)
          ~dimensions:Renderer.{ columns = 100; rows = 60 }
          "alpha"
      in
      let session =
        App.Session.handle_host session App.Session.Open_palette |> continue
      in
      let _, initial_frame = App.Session.render session in
      let initial_rows =
        Frame.rows initial_frame |> List.map Frame.row_text
        |> String.concat "\n"
      in
      expect
        (contains initial_rows "editor.apply")
        "palette did not include the first builtin model-neutral command";
      let selection =
        App.Session.handle_input session (text_input "split-regex")
      in
      let _, selection_frame = App.Session.render selection in
      let selection_rows =
        Frame.rows selection_frame |> List.map Frame.row_text
        |> String.concat "\n"
      in
      expect
        (contains selection_rows "editor.selection.split-regex"
        && contains selection_rows "[zenbu.models]")
        "palette filtering did not retain a builtin selection command/provider";
      let navigated =
        List.init 17 Fun.id
        |> List.fold_left
             (fun session _ ->
               App.Session.handle_input session (named Input_event.Arrow_down))
             session
      in
      let _, navigated_frame = App.Session.render navigated in
      let navigated_rows =
        Frame.rows navigated_frame |> List.map Frame.row_text
        |> String.concat "\n"
      in
      expect
        (contains navigated_rows "> ")
        "palette navigation moved beyond the visible result window";
      let host = App.Session.handle_input session (text_input "search.next") in
      let _, host_frame = App.Session.render host in
      let host_rows =
        Frame.rows host_frame |> List.map Frame.row_text |> String.concat "\n"
      in
      expect
        (contains host_rows "search.next" && contains host_rows "[zenbu.app]")
        "palette substring filtering did not retain a host command/provider";
      let host = App.Session.handle_input host (named Input_event.Enter) in
      expect
        (Model_status.id (App.Session.status host) = "normal")
        "palette host-command invocation did not return to the active model";
      let session =
        App.Session.handle_host host App.Session.Open_palette |> continue
      in
      let session =
        App.Session.handle_input session (text_input "palette-noop")
      in
      let _, frame = App.Session.render session in
      let rows =
        Frame.rows frame |> List.map Frame.row_text |> String.concat "\n"
      in
      expect
        (contains rows "user.palette-noop")
        "palette did not discover a script-provided command";
      let session =
        App.Session.handle_input session (named Input_event.Enter)
      in
      expect
        (App.Session.contents session = "alpha")
        "palette command invocation unexpectedly changed the document";
      let session =
        App.Session.handle_host session App.Session.Open_palette |> continue
      in
      let session =
        App.Session.handle_input session (named Input_event.Escape)
      in
      expect
        (Model_status.id (App.Session.status session) = "normal")
        "palette dismissal did not restore active model input")

let test_command_argument_prompt_executes_typed_and_scripted_commands () =
  let session = make_session "alpha" in
  let session =
    App.Session.handle_host session App.Session.Open_palette |> continue
  in
  let session = App.Session.handle_input session (text_input "editor.apply") in
  let session = App.Session.handle_input session (named Input_event.Enter) in
  expect
    (Model_status.id (App.Session.status session) = "host-command-argument")
    "palette did not open a typed command-argument prompt";
  let session = App.Session.handle_input session (text_input "document") in
  let session = App.Session.handle_input session (named Input_event.Enter) in
  let session =
    App.Session.handle_input session (text_input "replace:replacement")
  in
  let session = App.Session.handle_input session (named Input_event.Enter) in
  expect
    (App.Session.contents session = "replacement")
    "typed selector/transformation arguments did not invoke editor.apply";
  let session =
    App.Session.handle_host session App.Session.Open_palette |> continue
  in
  let session = App.Session.handle_input session (text_input "editor.apply") in
  let session = App.Session.handle_input session (named Input_event.Enter) in
  let session =
    App.Session.handle_input session (text_input "not-a-selector")
  in
  let session = App.Session.handle_input session (named Input_event.Enter) in
  expect
    (Model_status.id (App.Session.status session) = "host-command-argument"
    && App.Session.contents session = "replacement")
    "invalid typed arguments escaped the prompt or mutated the document";
  let session = App.Session.handle_input session (named Input_event.Escape) in
  expect
    (Model_status.id (App.Session.status session) = "normal")
    "Escape did not cancel the command-argument prompt";
  let path = Filename.temp_file "zenbu-m10-command-arguments" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.command {
  id = "user.insert-argument",
  title = "Insert argument",
  description = "Insert a prompt-provided text argument.",
  parameters = {
    { name = "text", description = "Text to insert.", required = true, kind = "text" },
  },
  run = function(call)
    return {{ kind = "insert", text = call.arguments.text }}
  end,
}
zenbu.bind { input = "Ctrl-X Ctrl-T", command = "user.insert-argument" }
|};
      let session =
        make_session ~config:(Zenbu_scripting.Scripting.Explicit path) "alpha"
      in
      let session =
        App.Session.handle_host session App.Session.Open_palette |> continue
      in
      let session =
        App.Session.handle_input session (text_input "insert-argument")
      in
      let session =
        App.Session.handle_input session (named Input_event.Enter)
      in
      let session = App.Session.handle_input session (text_input "!") in
      let session =
        App.Session.handle_input session (named Input_event.Enter)
      in
      expect
        (App.Session.contents session = "!alpha")
        "Lua command did not receive its prompt-provided text argument";
      let session =
        make_session ~config:(Zenbu_scripting.Scripting.Explicit path) "beta"
      in
      let session = App.Session.handle_input session (ctrl "x") in
      let session = App.Session.handle_input session (ctrl "t") in
      expect
        (Model_status.id (App.Session.status session) = "host-command-argument")
        "a binding to a parameterized command did not open the argument prompt";
      let session = App.Session.handle_input session (text_input "?") in
      let session =
        App.Session.handle_input session (named Input_event.Enter)
      in
      expect
        (App.Session.contents session = "?beta")
        "a binding to a parameterized Lua command lost prompt arguments");
  let path = Filename.temp_file "zenbu-m10-palette-open" ".txt" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path "opened from command argument";
      let session = make_session "alpha" in
      let session =
        App.Session.handle_host session App.Session.Open_palette |> continue
      in
      let session =
        App.Session.handle_input session (text_input "workspace.buffer.open")
      in
      let session =
        App.Session.handle_input session (named Input_event.Enter)
      in
      let session = App.Session.handle_input session (text_input path) in
      let session =
        App.Session.handle_input session (named Input_event.Enter)
      in
      expect
        (App.Session.contents session = "opened from command argument"
        && App.Session.file_path session = Some path)
        "palette open-buffer did not consume its typed path argument")

let test_named_buffers_are_listed_and_promptable () =
  let session = make_session "alpha" in
  let session =
    App.Session.handle_host session App.Session.New_buffer |> continue
  in
  let session =
    invoke_palette_text_argument session "workspace.buffer.rename" "*scratch*"
  in
  expect
    (App.Session.filename session = "*scratch*")
    "a typed buffer rename did not update the focused buffer label";
  let _, frame = App.Session.render session in
  expect
    ( Frame.rows frame |> List.map Frame.row_text |> String.concat "\n"
    |> fun screen -> contains screen "*scratch*" )
    "a named buffer did not render its label in terminal chrome";
  expect
    (App.Session.contents session = "")
    "renaming a buffer changed its contents";
  let listed =
    App.Session.handle_host session App.Session.List_buffers |> continue
  in
  let buffers = App.Session.inspect listed App.Session.Buffers in
  expect
    (lines_contain buffers "0: [No Name]"
    && lines_contain buffers "1: *scratch* (current)")
    "the named workspace buffers were not inspectable";
  let switched =
    invoke_palette_text_argument listed "workspace.buffer.switch" "0"
  in
  expect
    (App.Session.contents switched = "alpha"
    && App.Session.filename switched = "[No Name]")
    "a typed buffer switch did not restore the requested buffer";
  let scratch =
    invoke_palette_text_argument switched "workspace.buffer.switch" "1"
  in
  let dirty_scratch =
    scratch |> fun session ->
    App.Session.handle_input session (key "i") |> fun session ->
    App.Session.handle_input session (text_input "x") |> fun session ->
    App.Session.handle_input session (named Input_event.Escape)
  in
  let refused =
    App.Session.handle_host dirty_scratch App.Session.Close_buffer |> continue
  in
  expect
    (App.Session.buffer_count refused = 2 && App.Session.contents refused = "x")
    "safe buffer close discarded a dirty buffer";
  let closed =
    App.Session.handle_host refused App.Session.Force_close_buffer |> continue
  in
  expect
    (App.Session.buffer_count closed = 1
    && App.Session.contents closed = "alpha")
    "force-close did not discard the focused buffer and restore another buffer";
  let rejected = App.Session.rename_buffer closed ~name:"\n" in
  expect
    (App.Session.filename rejected = "[No Name]")
    "an invalid buffer name replaced the active display label";
  let final =
    App.Session.handle_host rejected App.Session.Close_buffer |> continue
  in
  expect
    (App.Session.buffer_count final = 1
    && App.Session.contents final = ""
    && App.Session.filename final = "[No Name]")
    "closing the final clean buffer did not create a fresh scratch buffer";
  expect
    (List.exists
       (fun descriptor ->
         Command_descriptor.id descriptor
         |> Command_id.to_string
         |> String.equal "workspace.buffer.rename")
       (App.Session.host_command_descriptors ()))
    "buffer renaming is not discoverable through the host palette"

let test_buffer_close_retargets_views_and_is_bindable () =
  let session = make_session ~model:App.Session.Direct "base" in
  let session =
    App.Session.handle_host session App.Session.New_buffer |> continue
  in
  let session =
    App.Session.handle_host session App.Session.Split_vertical |> continue
  in
  let closed =
    App.Session.handle_host session App.Session.Close_buffer |> continue
  in
  expect
    (App.Session.buffer_count closed = 1
    && App.Session.pane_count closed = 2
    && App.Session.contents closed = "base")
    "closing a shared buffer did not retarget the focused view to its \
     replacement";
  let other_view =
    App.Session.handle_host closed App.Session.Focus_next_pane |> continue
  in
  expect
    (App.Session.contents other_view = "base")
    "closing a shared buffer left another pane pointing at a removed buffer";
  let path = Filename.temp_file "zenbu-m10-close-buffer-binding" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.bind {
  input = "Ctrl-K",
  command = "workspace.buffer.close",
  scope = "model:zenbu.direct:direct",
}
|};
      let session =
        make_session ~model:App.Session.Direct
          ~config:(Zenbu_scripting.Scripting.Explicit path) "bound-base"
      in
      let session =
        App.Session.handle_host session App.Session.New_buffer |> continue
      in
      let closed = App.Session.handle_input session (ctrl "k") in
      expect
        (App.Session.buffer_count closed = 1
        && App.Session.contents closed = "bound-base")
        "a trusted binding could not request safe buffer close")

let test_selection_commands_are_promptable_and_bindable () =
  let path = Filename.temp_file "zenbu-m10-selection-commands" ".lua" in
  let trace = Trace.enabled ~capacity:64 |> must in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.bind {
  input = "S",
  command = "editor.selection.split-regex",
  scope = "model:zenbu.selection-first:select",
}
zenbu.command {
  id = "user.select-rotation-ranges",
  run = function(_)
    return {{
      kind = "set-selections",
      selections = {
        {anchor = 0, head = 1}, {anchor = 2, head = 3},
        {anchor = 4, head = 5}, {anchor = 6, head = 7},
      },
      primary = 1,
    }}
  end,
}
zenbu.bind {
  input = "M",
  command = "user.select-rotation-ranges",
  scope = "model:zenbu.selection-first:select",
}
zenbu.bind {
  input = "T",
  command = "editor.selection.rotate-contents-forward",
  scope = "model:zenbu.selection-first:select",
}
|};
      let session =
        make_session ~model:App.Session.Selection ~trace
          ~config:(Zenbu_scripting.Scripting.Explicit path) "red, green, blue"
      in
      let session = App.Session.handle_input session (key "L") in
      let session = App.Session.handle_input session (key "S") in
      expect
        (Model_status.id (App.Session.status session) = "host-command-argument")
        "a bound selection command did not request its regex parameter";
      let session = App.Session.handle_input session (text_input ", *") in
      let session =
        App.Session.handle_input session (named Input_event.Enter)
      in
      expect
        (selection_offsets session = [ (0, 3); (5, 10); (12, 16) ])
        "a prompt-bound selection command did not make the expected selections";
      expect
        (lines_contain
           (App.Session.inspect session App.Session.Why)
           "editor.selection.split-regex")
        "selection command invocation was absent from provenance";
      let grouped =
        make_session ~model:App.Session.Selection ~trace
          ~config:(Zenbu_scripting.Scripting.Explicit path) "a b c d"
      in
      let grouped = App.Session.handle_input grouped (key "M") in
      expect
        (selection_offsets grouped = [ (0, 1); (2, 3); (4, 5); (6, 7) ])
        "the script fixture did not establish grouped rotation selections";
      let grouped = App.Session.handle_input grouped (key "T") in
      expect
        (Model_status.id (App.Session.status grouped) = "host-command-argument")
        "a bound optional grouped-rotation command did not open its prompt";
      let grouped = App.Session.handle_input grouped (text_input "2") in
      let grouped =
        App.Session.handle_input grouped (named Input_event.Enter)
      in
      expect
        (App.Session.contents grouped = "b a d c")
        "the prompt-provided grouped rotation count did not rotate \
         independently")

let test_keyboard_macros_replay_through_the_session_dispatcher () =
  let path = Filename.temp_file "zenbu-m10-macros" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.bind { input = "Q", command = "editor.macro.record" }
zenbu.bind { input = "q", command = "editor.macro.replay" }
|};
      let session =
        make_session ~config:(Zenbu_scripting.Scripting.Explicit path) "alpha"
      in
      let session = App.Session.handle_input session (key "Q") in
      let session = App.Session.handle_input session (key "i") in
      let session = App.Session.handle_input session (text_input "界") in
      let session =
        App.Session.handle_input session (named Input_event.Escape)
      in
      let session = App.Session.handle_input session (key "Q") in
      expect
        (App.Session.contents session = "界alpha")
        "recording a keyboard macro changed its first execution";
      let macro = App.Session.inspect session App.Session.Macros in
      expect
        (lines_contain macro "recording: no"
        && lines_contain macro "recorded-inputs: 3"
        && lines_contain macro "text(i)")
        "recorded keyboard macro was not inspectable";
      let session = App.Session.handle_input session (key "q") in
      expect
        (App.Session.contents session = "界界alpha")
        "keyboard macro replay did not reuse ordinary model input";
      let macro = App.Session.inspect session App.Session.Macros in
      expect
        (lines_contain macro "recorded-inputs: 3"
        && lines_contain macro "replaying: false")
        "macro replay changed the recorded macro or left replay state active";
      let session = App.Session.handle_input session (key "q") in
      expect
        (App.Session.contents session = "界界界alpha")
        "a recorded macro could not be replayed repeatedly";
      let invoke_named_macro ?count session command register =
        let session =
          App.Session.handle_host session App.Session.Open_palette |> continue
        in
        let session = App.Session.handle_input session (text_input command) in
        let session =
          App.Session.handle_input session (named Input_event.Enter)
        in
        expect
          (Model_status.id (App.Session.status session)
          = "host-command-argument")
          "named macro command did not open its register prompt";
        let session = App.Session.handle_input session (text_input register) in
        let session =
          App.Session.handle_input session (named Input_event.Enter)
        in
        match count with
        | None -> session
        | Some count ->
            let session = App.Session.handle_input session (text_input count) in
            App.Session.handle_input session (named Input_event.Enter)
      in
      let session = invoke_named_macro session "editor.macro.record" "a" in
      let session = App.Session.handle_input session (key "i") in
      let session = App.Session.handle_input session (text_input "!") in
      let session =
        App.Session.handle_input session (named Input_event.Escape)
      in
      let session = invoke_named_macro session "editor.macro.record" "a" in
      expect
        (App.Session.contents session = "界界界!alpha")
        "named macro recording did not execute ordinary input once";
      let macros = App.Session.inspect session App.Session.Macros in
      expect
        (lines_contain macros "last-recorded-register: a"
        && lines_contain macros "recorded-inputs: 3"
        && lines_contain macros "register-count: 2"
        && lines_contain macros "@ (3), a (3)")
        "named macro storage did not preserve the default and named registers";
      let session =
        invoke_named_macro ~count:"2" session "editor.macro.replay" "a"
      in
      expect
        (App.Session.contents session = "界界界!!!alpha")
        "named macro replay count did not use the requested register";
      let invalid =
        invoke_named_macro ~count:"0" session "editor.macro.replay" "a"
      in
      expect
        (App.Session.contents invalid = "界界界!!!alpha"
        && Model_status.id (App.Session.status invalid)
           = "host-command-argument")
        "an invalid macro replay count escaped the prompt or changed the \
         document";
      let session =
        App.Session.handle_input invalid (named Input_event.Escape)
      in
      let macros = App.Session.inspect session App.Session.Macros in
      expect
        (lines_contain macros "last-recorded-register: a"
        && lines_contain macros "replaying: false"
        && lines_contain macros "text(i)")
        "named macro replay corrupted the stored named register";
      let vim = make_session "beta" in
      let vim = App.Session.handle_input vim (key "q") in
      expect
        (Model_status.id (App.Session.status vim) = "macro-recording-prefix")
        "Vim q did not wait for a macro register";
      let vim = App.Session.handle_input vim (key "a") in
      let vim = App.Session.handle_input vim (key "i") in
      let vim = App.Session.handle_input vim (text_input "!") in
      let vim = App.Session.handle_input vim (named Input_event.Escape) in
      let vim = App.Session.handle_input vim (key "q") in
      expect
        (App.Session.contents vim = "!beta"
        && Model_status.id (App.Session.status vim) = "normal")
        "Vim q did not stop the active named macro recording";
      let macros = App.Session.inspect vim App.Session.Macros in
      expect
        (lines_contain macros "last-recorded-register: a"
        && lines_contain macros "recorded-inputs: 3")
        "Vim q recorded macro-control input or stored the wrong register";
      let vim = App.Session.handle_input vim (key "@") in
      expect
        (Model_status.id (App.Session.status vim) = "macro-replay-prefix")
        "Vim @ did not wait for a macro register";
      let vim = App.Session.handle_input vim (key "a") in
      expect
        (App.Session.contents vim = "!!beta")
        "Vim @a did not replay the named host macro";
      let vim = App.Session.handle_input vim (key "2") in
      let vim = App.Session.handle_input vim (key "@") in
      let vim = App.Session.handle_input vim (key "a") in
      expect
        (App.Session.contents vim = "!!!!beta")
        "a Vim count did not repeat @a through the bounded macro service";
      let macros = App.Session.inspect vim App.Session.Macros in
      expect
        (lines_contain macros "maximum-replay-count: 1024"
        && lines_contain macros "maximum-replay-events: 65536")
        "macro replay limits were not inspectable";
      let no_macro = make_session "untouched" in
      let no_macro =
        App.Session.handle_host no_macro App.Session.Replay_macro |> continue
      in
      expect
        (App.Session.contents no_macro = "untouched")
        "replaying without a macro mutated the document";
      let bounded =
        make_session ~config:(Zenbu_scripting.Scripting.Explicit path) ""
      in
      let bounded = App.Session.handle_input bounded (key "Q") in
      let bounded =
        List.init 1025 Fun.id
        |> List.fold_left
             (fun session _ -> App.Session.handle_input session (key "h"))
             bounded
      in
      let macro = App.Session.inspect bounded App.Session.Macros in
      expect
        (lines_contain macro "recording: no"
        && lines_contain macro "recorded-inputs: none")
        "macro recording did not stop without retaining an over-limit macro")

let test_locations_rebase_across_buffers_and_reject_stale_history () =
  let session = make_session "abcdef" in
  let session = App.Session.handle_input session (key "l") in
  let session =
    invoke_palette_text_argument session "editor.location.set" "origin"
  in
  expect
    (lines_contain
       (App.Session.inspect session App.Session.Locations)
       "origin buffer=0 version=1 primary=0 selections=1:1 state=active")
    "setting a location did not capture the active selection set";
  let session = App.Session.handle_input session (key "i") in
  let session = App.Session.handle_input session (text_input "!") in
  let session = App.Session.handle_input session (named Input_event.Escape) in
  expect
    (lines_contain
       (App.Session.inspect session App.Session.Locations)
       "origin buffer=0 version=2 primary=0 selections=2:2 state=active")
    "a location did not rebase through a preceding insertion";
  let session =
    App.Session.handle_host session App.Session.New_buffer |> continue
  in
  expect
    (App.Session.buffer_count session = 2 && App.Session.contents session = "")
    "new-buffer did not activate an independent target buffer";
  let session =
    invoke_palette_text_argument session "editor.location.jump" "origin"
  in
  expect
    (App.Session.contents session = "a!bcdef"
    && primary_offsets session = (2, 2))
    "jumping a rebased location did not activate its buffer and selection";
  expect
    (lines_contain
       (App.Session.inspect session App.Session.Locations)
       "origin buffer=0 version=3 primary=0 selections=2:2 state=active")
    "location inspection did not advance through the selection-only jump";
  let trace = Trace.enabled ~capacity:32 |> must in
  let vim = make_session ~trace "omega" in
  let vim = App.Session.handle_input vim (key "l") in
  let vim = App.Session.handle_input vim (key "m") in
  expect
    (Model_status.id (App.Session.status vim) = "location-set-prefix")
    "Vim m did not wait for a location name";
  let vim = App.Session.handle_input vim (key "a") in
  let vim = App.Session.handle_input vim (key "i") in
  let vim = App.Session.handle_input vim (text_input "!") in
  let vim = App.Session.handle_input vim (named Input_event.Escape) in
  let vim = App.Session.handle_input vim (key "`") in
  expect
    (Model_status.id (App.Session.status vim) = "location-jump-prefix")
    "Vim backtick did not wait for a location name";
  let vim = App.Session.handle_input vim (key "a") in
  expect
    (App.Session.contents vim = "o!mega" && primary_offsets vim = (2, 2))
    "Vim ma and backtick-a did not use the host location service";
  expect
    (lines_contain (App.Session.inspect vim App.Session.Why) "location.jump:a")
    "Vim location jump did not retain host provenance";
  let jump_vim = make_session "abcdef" in
  let jump_vim = App.Session.handle_input jump_vim (key "l") in
  let jump_vim = App.Session.handle_input jump_vim (key "m") in
  let jump_vim = App.Session.handle_input jump_vim (key "a") in
  let jump_vim = App.Session.handle_input jump_vim (key "l") in
  let jump_vim = App.Session.handle_input jump_vim (key "`") in
  let jump_vim = App.Session.handle_input jump_vim (key "a") in
  expect
    (primary_offsets jump_vim = (1, 1)
    && lines_contain
         (App.Session.inspect jump_vim App.Session.Jumps)
         "backward-count: 1")
    "jumping to a Vim mark did not create a backward history entry";
  let jump_vim = App.Session.handle_input jump_vim (ctrl "o") in
  expect
    (primary_offsets jump_vim = (2, 2)
    && lines_contain
         (App.Session.inspect jump_vim App.Session.Jumps)
         "forward-count: 1")
    "Vim Ctrl-o did not traverse backward through jump history";
  let jump_vim = App.Session.handle_input jump_vim (ctrl "i") in
  expect
    (primary_offsets jump_vim = (1, 1))
    "Vim Ctrl-i did not traverse forward through jump history";
  let pushed = make_session "push" in
  let pushed = App.Session.handle_input pushed (key "l") in
  let pushed =
    App.Session.handle_host pushed App.Session.Push_jump |> continue
  in
  let pushed = App.Session.handle_input pushed (key "l") in
  let pushed =
    App.Session.handle_host pushed App.Session.Jump_backward |> continue
  in
  expect
    (primary_offsets pushed = (1, 1))
    "the generic jump-history push/backward commands did not restore a \
     selection";
  let bounded = make_session "bounded" in
  let bounded =
    List.init 101 Fun.id
    |> List.fold_left
         (fun session _ ->
           App.Session.handle_host session App.Session.Push_jump |> continue)
         bounded
  in
  expect
    (lines_contain
       (App.Session.inspect bounded App.Session.Jumps)
       "backward-count: 100")
    "jump history did not enforce its bounded entry limit";
  let stale_jump = make_session "alpha" in
  let stale_jump = App.Session.handle_input stale_jump (key "i") in
  let stale_jump = App.Session.handle_input stale_jump (text_input "!") in
  let stale_jump =
    App.Session.handle_input stale_jump (named Input_event.Escape)
  in
  let stale_jump =
    App.Session.handle_host stale_jump App.Session.Push_jump |> continue
  in
  let stale_jump = App.Session.handle_input stale_jump (key "u") in
  let selection_before_stale_jump = primary_offsets stale_jump in
  let stale_jump =
    App.Session.handle_host stale_jump App.Session.Jump_backward |> continue
  in
  expect
    (App.Session.contents stale_jump = "alpha"
    && primary_offsets stale_jump = selection_before_stale_jump
    && lines_contain
         (App.Session.inspect stale_jump App.Session.Jumps)
         "backward-count: 0")
    "a stale jump-history entry was restored instead of discarded";
  let stale = make_session "alpha" in
  let stale = App.Session.handle_input stale (key "i") in
  let stale = App.Session.handle_input stale (text_input "!") in
  let stale = App.Session.handle_input stale (named Input_event.Escape) in
  let stale =
    invoke_palette_text_argument stale "editor.location.set" "branch"
  in
  let stale = App.Session.handle_input stale (key "u") in
  let selection_before_jump = primary_offsets stale in
  let stale =
    invoke_palette_text_argument stale "editor.location.jump" "branch"
  in
  expect
    (App.Session.contents stale = "alpha"
    && primary_offsets stale = selection_before_jump
    && lines_contain
         (App.Session.inspect stale App.Session.Locations)
         "branch buffer=0 version=1 primary=0 selections=1:1 state=stale")
    "a location from an unreachable history branch was restored instead of \
     rejected";
  let selection = make_session ~model:App.Session.Selection "selection" in
  let selection =
    invoke_palette_text_argument selection "editor.location.set"
      "selection-origin"
  in
  let selection =
    App.Session.handle_host selection App.Session.New_buffer |> continue
  in
  let selection =
    invoke_palette_text_argument selection "editor.location.jump"
      "selection-origin"
  in
  expect
    (App.Session.model selection = App.Session.Selection
    && App.Session.contents selection = "selection"
    && primary_offsets selection = (0, 0))
    "locations were not usable by the selection-first workload";
  let selection_jump = make_session ~model:App.Session.Selection "abcdef" in
  let selection_jump = App.Session.handle_input selection_jump (key "l") in
  let selection_jump = App.Session.handle_input selection_jump (ctrl "s") in
  let selection_jump = App.Session.handle_input selection_jump (key "l") in
  let selection_jump = App.Session.handle_input selection_jump (ctrl "o") in
  expect
    (primary_offsets selection_jump = (0, 1)
    && lines_contain
         (App.Session.inspect selection_jump App.Session.Jumps)
         "forward-count: 1")
    "selection-first Ctrl-s and Ctrl-o did not save and restore a jump";
  let selection_jump = App.Session.handle_input selection_jump (ctrl "i") in
  expect
    (primary_offsets selection_jump = (1, 2))
    "selection-first Ctrl-i did not traverse forward through jump history"

let test_direct_model_micro_and_emacs_baseline () =
  let direct = make_session ~model:App.Session.Direct "abc" in
  expect
    (Model_status.id (App.Session.status direct) = "direct")
    "the direct model did not expose always-inserting status";
  let direct = App.Session.handle_input direct (text_input "X") in
  expect
    (App.Session.contents direct = "Xabc" && primary_offsets direct = (1, 1))
    "direct text input did not insert and advance the caret";
  let direct = App.Session.handle_input direct (named Input_event.Backspace) in
  expect
    (App.Session.contents direct = "abc")
    "direct Backspace did not delete the preceding text unit";
  let direct = App.Session.handle_input direct (ctrl "z") in
  expect
    (App.Session.contents direct = "Xabc")
    "direct Ctrl-z did not undo the shared transaction";
  let direct = App.Session.handle_input direct (ctrl "y") in
  expect
    (App.Session.contents direct = "abc")
    "direct Ctrl-y did not redo the shared transaction";
  let direct =
    App.Session.handle_input direct (named Input_event.Arrow_right)
  in
  expect
    (primary_offsets direct = (1, 1))
    "direct ArrowRight did not move the caret through shared selectors";
  let direct = App.Session.handle_input direct (named Input_event.Arrow_left) in
  expect
    (primary_offsets direct = (0, 0))
    "direct ArrowLeft did not move the caret through shared selectors";
  let direct =
    App.Session.handle_input direct
      (Input_event.key_press ~modifiers:[ Input_event.Shift ]
         (Input_event.named_key Input_event.Arrow_right))
  in
  expect
    (primary_offsets direct = (0, 1))
    "direct Shift-ArrowRight did not extend the selection";
  let direct = App.Session.handle_input direct (ctrl "w") in
  expect
    (App.Session.contents direct = "bc" && primary_offsets direct = (0, 0))
    "direct Ctrl-w did not cut the non-empty selection";
  let direct =
    App.Session.handle_host direct App.Session.Kill_ring_yank |> continue
  in
  expect
    (App.Session.contents direct = "abc")
    "the host kill-ring yank did not restore the latest direct-model cut";
  let direct = App.Session.handle_input direct (ctrl "x") in
  expect
    (Model_status.id (App.Session.status direct) = "control-x-prefix")
    "direct Ctrl-x did not enter the Emacs-style prefix state";
  let cancelled = App.Session.handle_input direct (ctrl "g") in
  expect
    (Model_status.id (App.Session.status cancelled) = "direct")
    "direct Ctrl-x Ctrl-g did not cancel the Emacs-style prefix state";
  let direct = App.Session.handle_input direct (ctrl "s") in
  expect
    (Model_status.id (App.Session.status direct) = "host-save-as"
    && App.Session.contents direct = "abc")
    "direct Ctrl-x Ctrl-s did not request a non-mutating host save";
  let micro_save = make_session ~model:App.Session.Direct "micro" in
  let micro_save = App.Session.handle_input micro_save (ctrl "s") in
  expect
    (Model_status.id (App.Session.status micro_save) = "host-save-as"
    && App.Session.contents micro_save = "micro")
    "direct Ctrl-s did not request the Micro-style host save";
  let emacs_windows = make_session ~model:App.Session.Direct "windows" in
  let emacs_windows = App.Session.handle_input emacs_windows (ctrl "x") in
  let emacs_windows = App.Session.handle_input emacs_windows (key "2") in
  expect
    (App.Session.pane_count emacs_windows = 2)
    "direct Ctrl-x 2 did not request a stacked workspace split";
  let focused_before = App.Session.focused_pane emacs_windows in
  let emacs_windows = App.Session.handle_input emacs_windows (ctrl "x") in
  let emacs_windows = App.Session.handle_input emacs_windows (key "o") in
  expect
    (App.Session.focused_pane emacs_windows <> focused_before)
    "direct Ctrl-x o did not request the next workspace view";
  let emacs_windows = App.Session.handle_input emacs_windows (ctrl "x") in
  let emacs_windows = App.Session.handle_input emacs_windows (key "3") in
  expect
    (App.Session.pane_count emacs_windows = 3)
    "direct Ctrl-x 3 did not request a side-by-side workspace split";
  let emacs_windows = App.Session.handle_input emacs_windows (ctrl "x") in
  let emacs_windows = App.Session.handle_input emacs_windows (key "0") in
  expect
    (App.Session.pane_count emacs_windows = 2)
    "direct Ctrl-x 0 did not request closing the selected workspace view";
  let emacs_windows = App.Session.handle_input emacs_windows (ctrl "x") in
  let emacs_windows = App.Session.handle_input emacs_windows (key "1") in
  expect
    (App.Session.pane_count emacs_windows = 1)
    "direct Ctrl-x 1 did not request keeping only the selected workspace view";
  let emacs_buffers = make_session ~model:App.Session.Direct "base" in
  let emacs_buffers =
    App.Session.handle_host emacs_buffers App.Session.New_buffer |> continue
  in
  let emacs_buffers = App.Session.handle_input emacs_buffers (ctrl "x") in
  let emacs_buffers = App.Session.handle_input emacs_buffers (key "k") in
  expect
    (App.Session.buffer_count emacs_buffers = 1
    && App.Session.contents emacs_buffers = "base")
    "direct Ctrl-x k did not close the focused clean buffer";
  let emacs_open = make_session ~model:App.Session.Direct "open" in
  let emacs_open = App.Session.handle_input emacs_open (ctrl "x") in
  let emacs_open = App.Session.handle_input emacs_open (ctrl "f") in
  expect
    (Model_status.id (App.Session.status emacs_open) = "host-open-buffer")
    "direct Ctrl-x Ctrl-f did not request the host open-buffer prompt";
  let divider_column frame =
    let rec find_cell column = function
      | [] -> None
      | (cell : Frame.cell) :: cells ->
          if String.equal cell.text "│" then Some column
          else find_cell (column + cell.width) cells
    in
    Frame.rows frame |> List.find_map (find_cell 0)
  in
  let divider_row frame =
    let rec loop row = function
      | [] -> None
      | cells :: rows ->
          if
            List.exists
              (fun (cell : Frame.cell) -> contains cell.text "─")
              cells
          then Some row
          else loop (row + 1) rows
    in
    Frame.rows frame |> loop 0
  in
  let emacs_resize =
    make_session ~model:App.Session.Direct
      ~dimensions:Renderer.{ columns = 11; rows = 7 }
      "resize"
  in
  let emacs_resize = App.Session.handle_input emacs_resize (ctrl "x") in
  let emacs_resize = App.Session.handle_input emacs_resize (key "3") in
  let emacs_resize, frame = App.Session.render emacs_resize in
  let initial_width =
    match divider_column frame with
    | Some column -> column
    | None -> failf "direct Ctrl-x 3 rendered no vertical divider"
  in
  let emacs_resize = App.Session.handle_input emacs_resize (ctrl "x") in
  let emacs_resize = App.Session.handle_input emacs_resize (key "}") in
  let emacs_resize, frame = App.Session.render emacs_resize in
  expect
    (divider_column frame = Some (initial_width - 1))
    "direct Ctrl-x } did not grow the focused right view";
  let emacs_resize = App.Session.handle_input emacs_resize (ctrl "x") in
  let emacs_resize = App.Session.handle_input emacs_resize (key "+") in
  let emacs_resize, frame = App.Session.render emacs_resize in
  expect
    (divider_column frame = Some initial_width)
    "direct Ctrl-x + did not balance split views";
  let emacs_resize = App.Session.handle_input emacs_resize (ctrl "x") in
  let emacs_resize = App.Session.handle_input emacs_resize (key "{") in
  let emacs_resize, frame = App.Session.render emacs_resize in
  expect
    (divider_column frame = Some (initial_width + 1))
    "direct Ctrl-x { did not shrink the focused right view";
  let emacs_resize = App.Session.handle_input emacs_resize (ctrl "x") in
  let emacs_resize = App.Session.handle_input emacs_resize (key "+") in
  let emacs_resize = App.Session.handle_input emacs_resize (ctrl "x") in
  let emacs_resize = App.Session.handle_input emacs_resize (key "2") in
  let emacs_resize, frame = App.Session.render emacs_resize in
  let initial_height =
    match divider_row frame with
    | Some row -> row
    | None -> failf "direct Ctrl-x 2 rendered no horizontal divider"
  in
  let emacs_resize = App.Session.handle_input emacs_resize (ctrl "x") in
  let emacs_resize = App.Session.handle_input emacs_resize (key "^") in
  let _, frame = App.Session.render emacs_resize in
  expect
    (divider_row frame = Some (initial_height - 1))
    "direct Ctrl-x ^ did not grow the focused bottom view"

let test_micro_adapter_can_request_host_workspace_actions () =
  let path = Filename.temp_file "zenbu-m10-micro-adapter" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.bind {
  input = "Ctrl-e",
  command = "editor.command-palette",
  scope = "model:zenbu.direct:direct",
}
zenbu.bind {
  input = "Ctrl-w",
  command = "workspace.pane.next",
  scope = "model:zenbu.direct:direct",
}
zenbu.bind {
  input = "Ctrl-x",
  command = "editor.kill-ring.cut",
  scope = "model:zenbu.direct:direct",
}
zenbu.bind {
  input = "Ctrl-y",
  command = "editor.kill-ring.yank",
  scope = "model:zenbu.direct:direct",
}
zenbu.bind {
  input = "Ctrl-c",
  command = "editor.clipboard.copy",
  scope = "model:zenbu.direct:direct",
}
zenbu.bind {
  input = "Ctrl-v",
  command = "editor.clipboard.paste",
  scope = "model:zenbu.direct:direct",
}
|};
      let system_contents = ref "" in
      let writes = ref 0 in
      let system_clipboard =
        App.System_clipboard.create ~name:"test"
          ~read:(fun () -> Ok !system_contents)
          ~write:(fun contents ->
            incr writes;
            system_contents := contents;
            Ok ())
      in
      let session =
        make_session ~model:App.Session.Direct
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~system_clipboard
          "micro"
      in
      let session = App.Session.handle_input session (ctrl "e") in
      expect
        (Model_status.id (App.Session.status session) = "host-palette")
        "a trusted adapter could not request the command palette";
      let session =
        App.Session.handle_input session (named Input_event.Escape)
      in
      let session =
        App.Session.handle_host session App.Session.Split_vertical |> continue
      in
      let focused_before = App.Session.focused_pane session in
      let session = App.Session.handle_input session (ctrl "w") in
      expect
        (App.Session.focused_pane session <> focused_before)
        "a trusted adapter could not request the next workspace view";
      expect
        (App.Session.contents session = "micro")
        "adapter workspace actions unexpectedly mutated document contents";
      let session =
        App.Session.handle_input session
          (Input_event.key_press ~modifiers:[ Input_event.Shift ]
             (Input_event.named_key Input_event.Arrow_right))
      in
      let session = App.Session.handle_input session (ctrl "x") in
      expect
        (App.Session.contents session = "icro")
        "the Micro-style Ctrl-x adapter did not cut the selected text";
      let session =
        App.Session.handle_host session App.Session.New_buffer |> continue
      in
      let session = App.Session.handle_input session (ctrl "y") in
      expect
        (App.Session.contents session = "m")
        "the adapter yank did not read the shared kill history across buffers";
      let session =
        App.Session.handle_input session
          (Input_event.key_press ~modifiers:[ Input_event.Shift ]
             (Input_event.named_key Input_event.Arrow_left))
      in
      let session = App.Session.handle_input session (ctrl "c") in
      expect
        (!writes = 1
        && String.equal !system_contents "m"
        && App.Session.contents session = "m")
        "the Micro-style Ctrl-c adapter did not copy the selection externally";
      system_contents := "external";
      let session = App.Session.handle_input session (ctrl "v") in
      expect
        (App.Session.contents session = "external")
        "the Micro-style Ctrl-v adapter did not paste external clipboard text";
      let unavailable = App.System_clipboard.unavailable "fixture disabled" in
      let unavailable_session =
        make_session ~model:App.Session.Direct
          ~config:(Zenbu_scripting.Scripting.Explicit path)
          ~system_clipboard:unavailable "copy"
      in
      let unavailable_session =
        App.Session.handle_input unavailable_session
          (Input_event.key_press ~modifiers:[ Input_event.Shift ]
             (Input_event.named_key Input_event.Arrow_right))
      in
      let unavailable_session =
        App.Session.handle_input unavailable_session (ctrl "c")
      in
      expect
        (App.Session.contents unavailable_session = "copy")
        "failed system clipboard copy changed the document";
      expect
        (Editor_context.clipboard_entry
           (App.Session.context unavailable_session)
           ~slot:Clipboard.unnamed
        = None)
        "failed system clipboard copy changed the ordinary clipboard slot")

let test_selection_adapter_can_request_viewport_actions () =
  let path = Filename.temp_file "zenbu-m10-selection-viewport" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.bind {
  input = "PageUp",
  command = "view.page.up",
  scope = "model:zenbu.selection-first:select",
}
zenbu.bind {
  input = "PageDown",
  command = "view.page.down",
  scope = "model:zenbu.selection-first:select",
}
zenbu.bind {
  input = "Ctrl-u",
  command = "view.page.up",
  scope = "model:zenbu.selection-first:select",
}
zenbu.bind {
  input = "Ctrl-d",
  command = "view.page.down",
  scope = "model:zenbu.selection-first:select",
}
zenbu.bind {
  input = "z z",
  command = "view.center",
  scope = "model:zenbu.selection-first:select",
}
|};
      let contents = "zero\none\ntwo\nthree\nfour\nfive\nsix\nseven" in
      let dimensions = Zenbu_view.Renderer.{ columns = 20; rows = 5 } in
      let session =
        make_session ~model:App.Session.Selection
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions contents
      in
      let selection = primary_offsets session in
      let session =
        App.Session.handle_input session (named Input_event.Page_down)
      in
      expect
        ((App.Session.viewport session).top_line = 4
        && App.Session.contents session = contents
        && primary_offsets session = selection)
        "a selection-editor adapter could not request nonsemantic page-down";
      let session = App.Session.handle_input session (key "z") in
      expect
        ((App.Session.viewport session).top_line = 4
        && App.Session.contents session = contents)
        "the center-view adapter prefix changed the viewport or document early";
      let session = App.Session.handle_input session (key "z") in
      expect
        ((App.Session.viewport session).top_line = 0
        && App.Session.contents session = contents
        && primary_offsets session = selection)
        "a selection-editor adapter could not center the viewport safely";
      let session = App.Session.handle_input session (ctrl "d") in
      let session = App.Session.handle_input session (ctrl "u") in
      expect
        ((App.Session.viewport session).top_line = 0
        && App.Session.contents session = contents
        && primary_offsets session = selection)
        "adapter page bindings did not preserve model semantic state")

let test_adapter_can_request_regexp_search () =
  let path = Filename.temp_file "zenbu-m10-regexp-search" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.bind {
  input = "Ctrl-r",
  command = "search.regexp",
  scope = "model:zenbu.direct:direct",
}
|};
      let session =
        make_session ~model:App.Session.Direct
          ~config:(Zenbu_scripting.Scripting.Explicit path) "a12 a5"
      in
      let session = App.Session.handle_input session (ctrl "r") in
      expect
        (Model_status.id (App.Session.status session) = "host-search")
        "a trusted adapter could not request the bounded regexp prompt";
      let session =
        App.Session.handle_input session (text_input "a[0-9][0-9]*")
      in
      expect
        (primary_offsets session = (0, 3)
        && lines_contain
             (App.Session.inspect session App.Session.Search)
             "kind: regexp")
        "an adapter-requested regexp search lost the host search contract")

let test_system_clipboard_provider_boundaries () =
  let writes = ref 0 in
  let provider =
    App.System_clipboard.create ~name:"test"
      ~read:(fun () -> Ok (String.make 1 (Char.chr 255)))
      ~write:(fun _ ->
        incr writes;
        Ok ())
  in
  expect_error (App.System_clipboard.read provider);
  expect_error
    (App.System_clipboard.write provider
       (String.make (App.System_clipboard.maximum_bytes + 1) 'x'));
  expect (!writes = 0) "oversized system clipboard write reached the provider";
  expect_error
    (App.System_clipboard.read
       (App.System_clipboard.unavailable "fixture unavailable"))

let test_save_as_and_model_switch_preserve_semantics () =
  let path = Filename.temp_file "zenbu-m10-save-as" ".txt" in
  Sys.remove path;
  Fun.protect
    ~finally:(fun () -> if Sys.file_exists path then Sys.remove path)
    (fun () ->
      let session = make_session "alpha" in
      let session = App.Session.handle_input session (key "i") in
      let session = App.Session.handle_input session (text_input "!") in
      let session =
        App.Session.handle_input session (named Input_event.Escape)
      in
      let history_before_save =
        App.Session.inspect session App.Session.History |> List.length
      in
      let session =
        App.Session.handle_host session App.Session.Save_as |> continue
      in
      let session = App.Session.handle_input session (text_input path) in
      let session =
        App.Session.handle_input session (named Input_event.Enter)
      in
      expect
        (App.Session.file_path session = Some path)
        "save-as did not update the active path";
      expect
        (not (App.Session.dirty session))
        "save-as did not establish a clean state";
      expect
        (App.File_io.read path |> Result.get_ok = "!alpha")
        "save-as did not atomically persist the current contents";
      expect
        (App.Session.inspect session App.Session.History
        |> List.length = history_before_save)
        "save-as changed semantic history";
      let session = App.Session.handle_input session (key "i") in
      let session = App.Session.handle_input session (text_input "?") in
      let session =
        App.Session.handle_input session (named Input_event.Escape)
      in
      let expected_after_normal_save = App.Session.contents session in
      let session =
        App.Session.handle_host session App.Session.Save |> continue
      in
      expect
        ((not (App.Session.dirty session))
        && App.File_io.read path |> Result.get_ok = expected_after_normal_save)
        "normal save did not reuse the save-as path or reset dirty state";
      let contents_before_model_switch = App.Session.contents session in
      let session = App.Session.handle_input session (key "d") in
      let session =
        App.Session.handle_host session App.Session.Switch_model |> continue
      in
      let session = App.Session.handle_input session (key "2") in
      expect
        (App.Session.model session = App.Session.Selection)
        "model picker did not activate selection-first editing";
      expect
        (App.Session.contents session = contents_before_model_switch)
        "model switch discarded document contents";
      let session = App.Session.handle_input session (key "u") in
      expect
        (App.Session.contents session = "!alpha")
        "model switch did not preserve semantic undo history")

let test_save_as_overwrite_and_write_failure () =
  let path = Filename.temp_file "zenbu-m10-overwrite" ".txt" in
  let blocker = Filename.temp_file "zenbu-m10-blocker" ".txt" in
  Fun.protect
    ~finally:(fun () ->
      List.iter
        (fun candidate ->
          if Sys.file_exists candidate then Sys.remove candidate)
        [ path; blocker ])
    (fun () ->
      write path "old contents";
      let session = make_session "replacement" in
      let session =
        App.Session.handle_host session App.Session.Save_as |> continue
      in
      let session = App.Session.handle_input session (text_input path) in
      let _session =
        App.Session.handle_input session (named Input_event.Enter)
      in
      expect
        (App.File_io.read path |> Result.get_ok = "replacement")
        "save-as did not explicitly replace an existing destination";
      let failed = make_session "unsaved" in
      let dirty_before_failure = App.Session.dirty failed in
      let failed =
        App.Session.handle_host failed App.Session.Save_as |> continue
      in
      let failed =
        App.Session.handle_input failed
          (text_input (Filename.concat blocker "child"))
      in
      let failed = App.Session.handle_input failed (named Input_event.Enter) in
      expect
        (App.Session.file_path failed = None
        && App.Session.dirty failed = dirty_before_failure)
        "a save-as write failure changed path or dirty state";
      expect
        (lines_contain
           (App.Session.inspect failed App.Session.Scripts)
           "cannot save")
        "save-as write failure did not retain a concise diagnostic")

let test_normal_save_refuses_external_changes () =
  let path = Filename.temp_file "zenbu-m10-save-conflict" ".txt" in
  let save_as_path = Filename.temp_file "zenbu-m10-save-as-conflict" ".txt" in
  Sys.remove save_as_path;
  Fun.protect
    ~finally:(fun () ->
      List.iter
        (fun candidate ->
          if Sys.file_exists candidate then Sys.remove candidate)
        [ path; save_as_path ])
    (fun () ->
      let session = vim_insert (session_for_file path "alpha") "!" in
      write path "external";
      let session =
        App.Session.handle_host session App.Session.Save |> continue
      in
      expect (App.Session.dirty session) "a conflicted save cleared dirty state";
      expect
        (App.File_io.read path |> Result.get_ok = "external")
        "a conflicted save overwrote the external contents";
      expect
        (lines_contain
           (App.Session.inspect session App.Session.Scripts)
           "target contents changed")
        "an in-place external change did not report a save conflict";
      let session =
        App.Session.handle_host session App.Session.Save_as |> continue
      in
      let session =
        App.Session.handle_input session (text_input save_as_path)
      in
      let session =
        App.Session.handle_input session (named Input_event.Enter)
      in
      expect
        (App.File_io.read save_as_path |> Result.get_ok = "!alpha")
        "save-as did not explicitly replace the externally changed target";
      let session = vim_insert session "?" in
      let session =
        App.Session.handle_host session App.Session.Save |> continue
      in
      expect
        ((not (App.Session.dirty session))
        && App.File_io.read save_as_path |> Result.get_ok = "!?alpha")
        "save-as did not establish a new normal-save baseline")

let test_normal_save_refuses_replaced_or_missing_target () =
  let replacement_path = Filename.temp_file "zenbu-m10-save-replaced" ".txt" in
  let missing_path = Filename.temp_file "zenbu-m10-save-missing" ".txt" in
  Fun.protect
    ~finally:(fun () ->
      List.iter
        (fun candidate ->
          if Sys.file_exists candidate then Sys.remove candidate)
        [ replacement_path; missing_path ])
    (fun () ->
      let replaced =
        vim_insert (session_for_file replacement_path "alpha") "!"
      in
      (match
         App.File_io.save_atomic ~path:replacement_path ~contents:"alpha"
       with
      | Ok () -> ()
      | Error error -> failf "%s" (App.File_io.to_string error));
      let replaced =
        App.Session.handle_host replaced App.Session.Save |> continue
      in
      expect
        (App.Session.dirty replaced)
        "a replaced target cleared dirty state";
      expect
        (lines_contain
           (App.Session.inspect replaced App.Session.Scripts)
           "target was replaced")
        "an atomically replaced target did not report a save conflict";
      let missing = vim_insert (session_for_file missing_path "alpha") "!" in
      Sys.remove missing_path;
      let missing =
        App.Session.handle_host missing App.Session.Save |> continue
      in
      expect (App.Session.dirty missing) "a missing target cleared dirty state";
      expect
        (lines_contain
           (App.Session.inspect missing App.Session.Scripts)
           "cannot verify the current target")
        "a deleted target did not report a save conflict")

let switch session key_name =
  let session =
    App.Session.handle_host session App.Session.Switch_model |> continue
  in
  App.Session.handle_input session (key key_name)

let test_model_switches_preserve_shared_state_and_reset_grammar () =
  let original = "alpha beta\n" in
  let session = make_session ~language:"ocaml" original in
  let session = App.Session.handle_input session (key "y") in
  let session = App.Session.handle_input session (key "w") in
  let before_switch = primary_offsets session in
  let session = App.Session.handle_input session (key "d") in
  expect
    (Model_status.id (App.Session.status session) = "operator-pending")
    "Vim setup did not enter a pending operator state";
  let session = switch session "2" in
  expect
    (App.Session.model session = App.Session.Selection
    && App.Session.contents session = original)
    "Vim-to-selection switch changed document or selected the wrong model";
  expect
    (Model_status.id (App.Session.status session) <> "operator-pending")
    "Vim pending grammar leaked into selection-first mode";
  expect
    (primary_offsets session = before_switch)
    "Vim-to-selection switch changed shared selections";
  let session = switch session "3" in
  expect
    (App.Session.model session = App.Session.Structural
    && Editor_context.syntax (App.Session.context session) <> None)
    "selection-to-structural switch discarded syntax service or selected wrong \
     model";
  let session = switch session "1" in
  expect
    (App.Session.model session = App.Session.Vim
    && App.Session.contents session = original)
    "structural-to-Vim switch discarded shared semantic state";
  let session = App.Session.handle_input session (key "p") in
  expect
    (App.Session.contents session <> original)
    "model switches discarded the shared unnamed clipboard entry";
  let plain = make_session "plain text" in
  let plain = switch plain "3" in
  expect
    (App.Session.model plain = App.Session.Structural
    && Editor_context.syntax (App.Session.context plain) = None)
    "switching to structural mode without syntax was rejected instead of \
     preserving NO SYNTAX behavior"

let test_bracketed_paste_decodes_as_one_text_input () =
  let pasted =
    Terminal.Input_decoder.decode ~input_mode:Model_status.Text_entry
      (Terminal.Event.Paste "one\ntwo界")
    |> must
  in
  expect
    (pasted = Some (text_input "one\ntwo界"))
    "bracketed paste did not become one committed Unicode text input";
  expect
    (Terminal.Input_decoder.decode ~input_mode:Model_status.Key_commands
       (Terminal.Event.Paste "ignored")
    |> must = None)
    "bracketed paste bypassed model-declared text-entry mode";
  let session = make_session "alpha" in
  let session = App.Session.handle_input session (key "i") in
  let pasted =
    Terminal.Input_decoder.decode
      ~input_mode:(Model_status.input_mode (App.Session.status session))
      (Terminal.Event.Paste "dw\n界")
    |> must |> Option.get
  in
  let session = App.Session.handle_input session pasted in
  let session = App.Session.handle_input session (named Input_event.Escape) in
  expect
    (App.Session.contents session = "dw\n界alpha")
    "bracketed paste was interpreted as command grammar instead of committed \
     text";
  let session = App.Session.handle_input session (key "u") in
  expect
    (App.Session.contents session = "alpha")
    "one bracketed paste was not undoable as one committed text action"

let tests =
  [
    ( "Unicode host search and inspection",
      test_unicode_search_is_host_level_and_observable );
    ( "regexp search is incremental and UTF-8-safe",
      test_regexp_search_is_incremental_and_utf8_safe );
    ( "replace-all is atomic and UTF-8-safe",
      test_replace_all_is_atomic_and_utf8_safe );
    ( "query-replace is reviewed and atomic per decision",
      test_query_replace_is_reviewed_and_atomic_per_decision );
    ("Vim modal search requests", test_vim_modal_search_requests);
    ( "syntax spans and render precedence",
      test_syntax_spans_and_render_precedence );
    ( "explicit startup failures remain inspectable",
      test_explicit_startup_failures_remain_inspectable );
    ( "generic palette discovers all active command providers",
      test_palette_discovers_all_active_command_providers );
    ( "command argument prompts execute typed and scripted commands",
      test_command_argument_prompt_executes_typed_and_scripted_commands );
    ( "named buffers are listed and promptable",
      test_named_buffers_are_listed_and_promptable );
    ( "buffer close retargets views and is bindable",
      test_buffer_close_retargets_views_and_is_bindable );
    ( "selection commands are promptable and bindable",
      test_selection_commands_are_promptable_and_bindable );
    ( "keyboard macros replay through the session dispatcher",
      test_keyboard_macros_replay_through_the_session_dispatcher );
    ( "persistent locations rebase and reject stale branches",
      test_locations_rebase_across_buffers_and_reject_stale_history );
    ( "direct model supports Micro and Emacs editing baseline",
      test_direct_model_micro_and_emacs_baseline );
    ( "Micro adapter bindings can request host workspace actions",
      test_micro_adapter_can_request_host_workspace_actions );
    ( "selection adapter bindings can request viewport actions",
      test_selection_adapter_can_request_viewport_actions );
    ( "adapters can request regexp search",
      test_adapter_can_request_regexp_search );
    ( "system clipboard provider rejects invalid data and unavailable backends",
      test_system_clipboard_provider_boundaries );
    ( "save-as and model switch",
      test_save_as_and_model_switch_preserve_semantics );
    ("save-as overwrite and failure", test_save_as_overwrite_and_write_failure);
    ( "normal save refuses external changes",
      test_normal_save_refuses_external_changes );
    ( "normal save refuses replaced or missing targets",
      test_normal_save_refuses_replaced_or_missing_target );
    ( "live model switching preserves shared state",
      test_model_switches_preserve_shared_state_and_reset_grammar );
    ("bracketed paste decoding", test_bracketed_paste_decodes_as_one_text_input);
  ]

let () =
  List.iter
    (fun (name, test) ->
      try
        test ();
        Printf.printf "ok: %s\n" name
      with Test_failure message ->
        Printf.eprintf "FAILED: %s: %s\n" name message;
        exit 1)
    tests
