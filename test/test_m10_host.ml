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

let make_session ?(model = App.Session.Vim) ?language ?trace
    ?(config = Zenbu_scripting.Scripting.Disabled) ?(dimensions = dimensions)
    contents =
  App.Session.create ~model ?language ?trace ~contents ~config ~dimensions ()
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

let test_selection_commands_are_promptable_and_bindable () =
  let path = Filename.temp_file "zenbu-m10-selection-commands" ".lua" in
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
|};
      let session =
        make_session ~model:App.Session.Selection
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
        "selection command invocation was absent from provenance")

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
    ("Vim modal search requests", test_vim_modal_search_requests);
    ( "syntax spans and render precedence",
      test_syntax_spans_and_render_precedence );
    ( "explicit startup failures remain inspectable",
      test_explicit_startup_failures_remain_inspectable );
    ( "generic palette discovers all active command providers",
      test_palette_discovers_all_active_command_providers );
    ( "command argument prompts execute typed and scripted commands",
      test_command_argument_prompt_executes_typed_and_scripted_commands );
    ( "selection commands are promptable and bindable",
      test_selection_commands_are_promptable_and_bindable );
    ( "keyboard macros replay through the session dispatcher",
      test_keyboard_macros_replay_through_the_session_dispatcher );
    ( "save-as and model switch",
      test_save_as_and_model_switch_preserve_semantics );
    ("save-as overwrite and failure", test_save_as_overwrite_and_write_failure);
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
