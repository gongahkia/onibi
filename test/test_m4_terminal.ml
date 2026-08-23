open Zenbu_kernel
open Zenbu_model_api
module App = Zenbu_app
module Display = Zenbu_view.Display
module Frame = Zenbu_view.Frame
module Layout = Zenbu_view.Layout
module Presentation = Zenbu_view.Presentation
module Renderer = Zenbu_view.Renderer
module Theme = Zenbu_view.Theme
module Terminal = Zenbu_terminal

exception Test_failure of string

let failf format =
  Printf.ksprintf (fun message -> raise (Test_failure message)) format

let expect condition format =
  Printf.ksprintf
    (fun message -> if not condition then raise (Test_failure message))
    format

let expect_string ~expected ~actual =
  expect (String.equal expected actual) "expected %S, got %S" expected actual

let contains ~substring text =
  let substring_length = String.length substring in
  let text_length = String.length text in
  let rec loop index =
    if index + substring_length > text_length then false
    else if String.sub text index substring_length = substring then true
    else loop (index + 1)
  in
  loop 0

let must = function
  | Ok value -> value
  | Error error -> failf "%s" (Error.to_string error)

let layout_must = function
  | Ok value -> value
  | Error error -> failf "%s" (Layout.error_to_string error)

let key text = Input_event.key_press (Input_event.logical_text text |> must)
let text_input text = Input_event.text_input text |> must
let named key = Input_event.key_press (Input_event.named_key key)
let pointer action ~column ~row = Input_event.mouse action ~column ~row |> must

let status input_mode =
  Model_status.create ~id:"test" ~label:"TEST" ~input_mode () |> must

let test_input_decoder_is_model_neutral () =
  let command =
    Terminal.Input_decoder.decode ~input_mode:Model_status.Key_commands
      (Terminal.Event.Key { key = Terminal.Event.Text "d"; modifiers = [] })
    |> must
  in
  expect
    (command = Some (key "d"))
    "command status did not preserve a printable key as Key_press";
  let committed =
    Terminal.Input_decoder.decode ~input_mode:Model_status.Text_entry
      (Terminal.Event.Key { key = Terminal.Event.Text "界"; modifiers = [] })
    |> must
  in
  expect
    (committed = Some (text_input "界"))
    "text-entry status did not produce committed Unicode text";
  let control =
    Terminal.Input_decoder.decode ~input_mode:Model_status.Text_entry
      (Terminal.Event.Key
         {
           key = Terminal.Event.Text "r";
           modifiers = [ Terminal.Event.Control ];
         })
    |> must
  in
  expect
    (Input_event.modifiers (Option.get control) = [ Input_event.Control ])
    "control modifier was lost";
  let tab =
    Terminal.Input_decoder.decode ~input_mode:Model_status.Key_commands
      (Terminal.Event.Key { key = Terminal.Event.Tab; modifiers = [] })
    |> must
  in
  expect
    (Input_event.key (Option.get tab)
    = Some (Input_event.Named_key Input_event.Tab))
    "named terminal keys must remain logical key presses";
  let page_down =
    Terminal.Input_decoder.decode ~input_mode:Model_status.Key_commands
      (Terminal.Event.Key { key = Terminal.Event.Page_down; modifiers = [] })
    |> must
  in
  expect
    (Input_event.key (Option.get page_down)
    = Some (Input_event.Named_key Input_event.Page_down))
    "PageDown did not remain a named logical key press";
  let mouse =
    Terminal.Input_decoder.decode ~input_mode:Model_status.Key_commands
      (Terminal.Event.Mouse
         {
           action = Terminal.Event.Press Terminal.Event.Primary;
           column = 4;
           row = 2;
           modifiers = [ Terminal.Event.Shift ];
         })
    |> must
  in
  expect
    (Input_event.mouse_action (Option.get mouse)
    = Some (Input_event.Press Input_event.Primary))
    "terminal mouse press did not remain a typed pointer event";
  expect
    (Input_event.mouse_position (Option.get mouse) = Some (4, 2)
    && Input_event.modifiers (Option.get mouse) = [ Input_event.Shift ])
    "terminal mouse coordinates or modifiers were changed during decoding"

let test_binding_sequence_parser () =
  let sequence =
    Input_event.binding_sequence_of_string "Ctrl-X Ctrl-Shift-K Alt-Enter Space"
    |> must
  in
  expect
    (Input_event.binding_sequence_to_string sequence
    = "Ctrl+text(x) Shift+Ctrl+text(K) Alt+Enter text( )")
    "binding sequence parsing did not preserve modifiers, named keys, and Space";
  let case_sensitive = Input_event.binding_sequence_of_string "Q q" |> must in
  expect
    (Input_event.binding_sequence_to_string case_sensitive = "text(Q) text(q)")
    "plain binding tokens did not preserve letter case";
  let pages =
    Input_event.binding_sequence_of_string "PageUp PageDown" |> must
  in
  expect
    (Input_event.binding_sequence_to_string pages = "PageUp PageDown")
    "binding sequence parsing did not preserve page-navigation keys";
  expect
    (match Input_event.binding_sequence_of_string "Ctrl-X  Ctrl-K" with
    | Error _ -> true
    | Ok _ -> false)
    "binding sequence parsing accepted an ambiguous empty token";
  expect
    (match Input_event.binding_sequence_of_string "Ctrl-Ctrl-X" with
    | Error _ -> true
    | Ok _ -> false)
    "binding sequence parsing accepted duplicate modifiers";
  expect
    (match Input_event.binding_sequence_of_string "Hyper-X" with
    | Error _ -> true
    | Ok _ -> false)
    "binding sequence parsing accepted an unknown modifier";
  let patterns =
    Input_event.binding_pattern_sequence_of_string "Ctrl-X <text>" |> must
  in
  expect
    (Input_event.binding_pattern_sequence_to_string patterns
    = "Ctrl+text(x) <text>")
    "binding pattern parsing did not preserve a committed-text wildcard";
  expect
    (match patterns with
    | [ Input_event.Exact_event _; Input_event.Any_text_input ] -> true
    | _ -> false)
    "binding pattern parser did not expose the typed committed-text wildcard";
  expect
    (Input_event.binding_pattern_matches Input_event.Any_text_input
       (text_input "界")
    && not
         (Input_event.binding_pattern_matches Input_event.Any_text_input
            (key "x")))
    "committed-text wildcard did not distinguish text entry from logical keys"

let test_display_coordinates () =
  let line = List.hd (Display.lines "é\t界\r") in
  let tab, wide, carriage_return =
    match line.graphemes with
    | [ combining; tab; wide; carriage_return ] ->
        expect
          (combining.start_offset = 0 && combining.stop_offset = 3)
          "combining grapheme was split at a byte boundary";
        expect
          (combining.width = 1 && combining.column = 0)
          "combining grapheme has the wrong display width";
        expect
          (tab.column = 1 && tab.width = 3)
          "tab did not expand relative to its display column";
        expect
          (wide.column = 4 && wide.width = 2)
          "wide Unicode character did not occupy two display columns";
        expect_string ~expected:"^M" ~actual:carriage_return.text;
        expect
          (carriage_return.column = 6 && carriage_return.width = 2)
          "control rendering lost its display coordinates";
        (tab, wide, carriage_return)
    | _ -> failf "unexpected grapheme segmentation"
  in
  expect
    (Display.offset_at_column line 2 = tab.start_offset
    && Display.offset_at_column line 5 = wide.start_offset
    && Display.offset_at_column line 8 = carriage_return.stop_offset)
    "display-column to document-offset mapping split a grapheme"

let document_context ?(selections = [ (0, 0) ]) ?(primary = 0) contents =
  let id = Document_id.of_string "m4-view" |> must in
  let selections =
    List.map
      (fun (anchor_offset, head_offset) ->
        Selection_spec.make ~anchor_offset ~head_offset |> must)
      selections
  in
  let document =
    Document.create ~id ~contents ~initial_selections:selections ~primary ()
    |> must
  in
  Editor_context.from_snapshot
    ~snapshot:(Document.snapshot document)
    ~commands:[] ()

let styles frame =
  Frame.rows frame |> List.concat |> List.map (fun cell -> cell.Frame.style)

let frame ~width ~height ~text ?cursor () =
  Frame.create ~width ~height
    ~rows:(List.init height (fun _ -> [ Frame.cell ~width text ]))
    ~cursor

let theme_fixture name =
  match
    [ "fixtures/" ^ name; "test/fixtures/" ^ name ]
    |> List.map (Filename.concat (Sys.getcwd ()))
    |> List.find_opt Sys.file_exists
  with
  | Some path -> path
  | None -> failf "missing theme fixture %s" name

let test_theme_contract () =
  expect
    (List.map Theme.name (Theme.builtins ()) = [ "default"; "dark"; "light" ])
    "built-in theme names are not stable";
  expect
    (Theme.attribute Theme.default Frame.Status
    = {
        Theme.foreground = Theme.Ansi Theme.White;
        background = Theme.Ansi Theme.Light_black;
        decorations = [];
      })
    "the default theme no longer preserves the terminal status palette";
  let path = theme_fixture "m4_theme.toml" in
  let custom =
    match Theme.load path with
    | Ok theme -> theme
    | Error reason -> failf "%s" reason
  in
  expect_string ~expected:"m4-custom" ~actual:(Theme.name custom);
  expect
    (Theme.attribute custom Frame.Plain
    = {
        Theme.foreground = Theme.Rgb (17, 34, 51);
        background = Theme.Rgb (250, 250, 250);
        decorations = [];
      })
    "a custom theme did not parse true-colour plain text";
  expect
    (Theme.attribute custom Frame.Syntax_keyword
    = {
        Theme.foreground = Theme.Rgb (171, 205, 239);
        background = Theme.Default;
        decorations = [ Theme.Italic ];
      })
    "a custom theme did not preserve and override syntax decorations";
  let invalid = theme_fixture "m4_theme_invalid.toml" in
  expect
    (Result.is_error (Theme.load invalid))
    "an unknown theme role was accepted"

let test_presentation_profile_contract () =
  expect
    (List.map Presentation.name (Presentation.builtins ())
    = [ "default"; "numbered"; "relative"; "minimal"; "bare"; "buffered" ])
    "built-in presentation profile names are not stable";
  let path = theme_fixture "m4_presentation.toml" in
  let profile =
    match Presentation.load path with
    | Ok profile -> profile
    | Error reason -> failf "%s" reason
  in
  expect_string ~expected:"m4-relative-minimal"
    ~actual:(Presentation.name profile);
  expect
    (Presentation.line_numbers profile = Presentation.Relative
    && Presentation.status_line profile = Presentation.Minimal
    && Presentation.buffer_line profile = Presentation.Hidden_buffer_line)
    "a custom presentation profile did not preserve both renderer policies";
  let buffered = theme_fixture "m4_presentation_buffered.toml" in
  let buffered =
    match Presentation.load buffered with
    | Ok profile -> profile
    | Error reason -> failf "%s" reason
  in
  expect
    (Presentation.buffer_line buffered = Presentation.Visible)
    "a custom presentation profile did not preserve its buffer-line policy";
  let invalid = theme_fixture "m4_presentation_invalid.toml" in
  expect
    (Result.is_error (Presentation.load invalid))
    "an invalid presentation line-number mode was accepted"

let test_layout_composition () =
  let layout =
    Layout.single 0 |> fun layout ->
    Layout.split layout ~pane:0 ~new_pane:1 Layout.Vertical |> layout_must
  in
  expect
    (Layout.bounds layout ~width:11 ~height:4
    = [
        (0, Layout.{ x = 0; y = 0; width = 5; height = 4 });
        (1, Layout.{ x = 6; y = 0; width = 5; height = 4 });
      ])
    "vertical layout allocated incorrect pane bounds";
  let composed =
    Layout.compose layout ~width:11 ~height:4 ~focused_pane:1
      ~frames:
        [
          (0, frame ~width:5 ~height:4 ~text:"aaaaa" ());
          ( 1,
            frame ~width:5 ~height:4 ~text:"bbbbb"
              ~cursor:Frame.{ column = 0; row = 0 }
              () );
        ]
    |> layout_must
  in
  expect_string ~expected:"aaaaa│bbbbb"
    ~actual:(Frame.rows composed |> List.hd |> Frame.row_text);
  expect
    (Frame.cursor composed = Some Frame.{ column = 6; row = 0 })
    "focused pane cursor was not translated through the divider";
  let layout = Layout.close layout ~pane:0 |> layout_must in
  expect (Layout.panes layout = [ 1 ]) "closing a pane retained a stale leaf";
  expect
    (match Layout.close layout ~pane:1 with
    | Error Layout.Cannot_close_last_pane -> true
    | Ok _ | Error _ -> false)
    "the last pane was unexpectedly closable"

let test_renderer_selection_viewport_and_tiny_terminal () =
  let context =
    document_context ~selections:[ (0, 2); (2, 3) ] ~primary:0 "éx\nline two"
  in
  let rendered =
    Renderer.render ~context
      ~status:(status Model_status.Key_commands)
      ~filename:"unicode.txt" ~dirty:true ~message:(Some "saved later")
      ~viewport:Zenbu_view.Viewport.origin
      ~dimensions:{ columns = 12; rows = 3 }
  in
  expect
    (List.mem Frame.Primary_selection (styles rendered.frame)
    && List.mem Frame.Secondary_selection (styles rendered.frame))
    "primary and secondary selections were not rendered distinctly";
  let deep_context =
    document_context ~selections:[ (16, 16) ] "a\na\na\na\na\na\na\na\na"
  in
  let deep =
    Renderer.render ~context:deep_context
      ~status:(status Model_status.Key_commands)
      ~filename:"deep" ~dirty:false ~message:None
      ~viewport:Zenbu_view.Viewport.origin ~dimensions:{ columns = 3; rows = 3 }
  in
  expect
    (deep.viewport.top_line > 0)
    "viewport did not follow an off-screen primary selection";
  let horizontal_context = document_context ~selections:[ (8, 8) ] "abcdefgh" in
  let horizontal =
    Renderer.render ~context:horizontal_context
      ~status:(status Model_status.Key_commands)
      ~filename:"wide" ~dirty:false ~message:None
      ~viewport:Zenbu_view.Viewport.origin ~dimensions:{ columns = 3; rows = 3 }
  in
  expect
    (horizontal.viewport.left_column > 0)
    "viewport did not follow a horizontally off-screen primary selection";
  let tiny =
    Renderer.render ~context
      ~status:(status Model_status.Key_commands)
      ~filename:"tiny" ~dirty:false ~message:None
      ~viewport:Zenbu_view.Viewport.origin ~dimensions:{ columns = 5; rows = 1 }
  in
  expect
    (List.length (Frame.rows tiny.frame) = 1)
    "tiny terminals need a safe fallback frame"

let test_renderer_presentation_profiles () =
  let context = document_context ~selections:[ (5, 5) ] "zero\none\ntwo" in
  let relative_minimal =
    match Presentation.load (theme_fixture "m4_presentation.toml") with
    | Ok profile -> profile
    | Error reason -> failf "%s" reason
  in
  let relative =
    Renderer.render_with_presentation ~presentation:relative_minimal ~context
      ~status:(status Model_status.Key_commands)
      ~filename:"sample" ~dirty:false ~message:None
      ~viewport:Zenbu_view.Viewport.origin ~dimensions:{ columns = 8; rows = 3 }
  in
  let rows = Frame.rows relative.frame in
  expect
    (List.map Frame.row_text rows = [ "1 zero  "; "0 one   "; "TEST  sa" ])
    "relative presentation did not reserve a stable gutter and minimal status \
     row";
  expect
    (Frame.cursor relative.frame = Some Frame.{ column = 2; row = 1 })
    "a line-number gutter did not shift the terminal cursor";
  let bare =
    Renderer.render_with_presentation ~presentation:Presentation.bare ~context
      ~status:(status Model_status.Key_commands)
      ~filename:"sample" ~dirty:false ~message:None
      ~viewport:Zenbu_view.Viewport.origin ~dimensions:{ columns = 8; rows = 3 }
  in
  expect
    (List.length (Frame.rows bare.frame) = 3
    && not (List.mem Frame.Status (styles bare.frame)))
    "hidden status presentation did not allocate the whole frame to source rows";
  let narrow =
    Renderer.render_with_presentation ~presentation:Presentation.numbered
      ~context
      ~status:(status Model_status.Key_commands)
      ~filename:"sample" ~dirty:false ~message:None
      ~viewport:Zenbu_view.Viewport.origin ~dimensions:{ columns = 1; rows = 2 }
  in
  expect
    (Frame.width narrow.frame = 1
    && List.length (Frame.rows narrow.frame) = 2
    && Frame.cursor narrow.frame = None)
    "a gutter wider than the canvas did not degrade to a safe cursorless frame"

let primary_selection session =
  let selections = Editor_context.selections (App.Session.context session) in
  List.nth selections.selections selections.primary_index

let test_session_presentation_profile () =
  let presentation =
    match Presentation.load (theme_fixture "m4_presentation.toml") with
    | Ok profile -> profile
    | Error reason -> failf "%s" reason
  in
  let session =
    App.Session.create ~model:App.Session.Direct ~presentation
      ~contents:"zero\none"
      ~dimensions:Renderer.{ columns = 8; rows = 3 }
      ()
    |> must
  in
  let _, frame = App.Session.render session in
  expect
    (Frame.rows frame |> List.hd |> Frame.row_text = "0 zero  ")
    "the Session did not pass its presentation profile to pane rendering";
  let switched = App.Session.set_presentation session ~profile:"numbered" in
  let _, frame = App.Session.render switched in
  expect
    (Frame.rows frame |> List.hd |> Frame.row_text = "1 zero  ")
    "runtime presentation switching did not update the terminal chrome";
  expect
    (App.Session.contents switched = "zero\none"
    && primary_selection switched = primary_selection session)
    "runtime presentation switching changed semantic editor state";
  let rejected =
    App.Session.set_presentation switched ~profile:"not-a-presentation"
  in
  let _, frame = App.Session.render rejected in
  expect
    (Frame.rows frame |> List.hd |> Frame.row_text = "1 zero  ")
    "an invalid runtime presentation replaced the active profile";
  expect
    (List.exists
       (fun descriptor ->
         Command_descriptor.id descriptor
         |> Command_id.to_string
         |> String.equal "view.presentation.switch")
       (App.Session.host_command_descriptors ()))
    "runtime presentation switching is not discoverable through the host \
     palette";
  let themed = App.Session.set_theme rejected ~theme:"dark" in
  expect
    (Theme.name (App.Session.theme themed) = "dark")
    "runtime theme switching did not retain the selected terminal theme";
  expect
    (App.Session.contents themed = "zero\none"
    && primary_selection themed = primary_selection session)
    "runtime theme switching changed semantic editor state";
  let rejected_theme = App.Session.set_theme themed ~theme:"not-a-theme" in
  expect
    (Theme.name (App.Session.theme rejected_theme) = "dark")
    "an invalid runtime theme replaced the active theme";
  expect
    (List.exists
       (fun descriptor ->
         Command_descriptor.id descriptor
         |> Command_id.to_string
         |> String.equal "view.theme.switch")
       (App.Session.host_command_descriptors ()))
    "runtime theme switching is not discoverable through the host palette"

let test_pointer_selection_and_scroll () =
  let dimensions = Renderer.{ columns = 12; rows = 5 } in
  let session =
    App.Session.create ~model:App.Session.Vim
      ~contents:"ab\n界x\none\ntwo\nthree\nfour" ~dimensions ()
    |> must
  in
  let session =
    App.Session.handle_pointer session
      (pointer (Input_event.Press Input_event.Primary) ~column:1 ~row:0)
  in
  let primary = primary_selection session in
  expect
    (primary.anchor_offset = 1 && primary.head_offset = 1)
    "a primary mouse press did not place the caret at the clicked grapheme";
  let session =
    App.Session.handle_pointer session
      (pointer Input_event.Drag ~column:2 ~row:1)
  in
  let session =
    App.Session.handle_pointer session
      (pointer Input_event.Release ~column:2 ~row:1)
  in
  let primary = primary_selection session in
  expect
    (primary.anchor_offset = 1 && primary.head_offset = 6)
    "a mouse drag did not create a grapheme-safe selection";
  let session =
    App.Session.handle_pointer session
      (pointer (Input_event.Press Input_event.Primary) ~column:3 ~row:4)
  in
  let primary_after_status = primary_selection session in
  expect
    (primary_after_status = primary)
    "a status-row click changed the document selection";
  let session =
    App.Session.handle_pointer session
      (pointer (Input_event.Press Input_event.Wheel_down) ~column:0 ~row:0)
  in
  expect
    ((App.Session.viewport session).top_line = 2
    && not (App.Session.viewport session).follow_cursor)
    "mouse-wheel scrolling did not retain an explicit viewport position";
  let session, _ = App.Session.render session in
  expect
    ((App.Session.viewport session).top_line = 2)
    "rendering immediately discarded an explicit mouse scroll";
  let session = App.Session.handle_input session (key "l") in
  expect (App.Session.viewport session).follow_cursor
    "keyboard navigation did not restore cursor-following after mouse scroll";
  let session =
    match App.Session.handle_host session App.Session.Split_vertical with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "mouse split unexpectedly exited"
  in
  let session =
    App.Session.handle_pointer session
      (pointer (Input_event.Press Input_event.Primary) ~column:8 ~row:0)
  in
  expect
    (App.Session.focused_pane session = 1)
    "a mouse press did not focus the pane under the pointer";
  let numbered =
    App.Session.create ~model:App.Session.Direct ~contents:"abcd"
      ~presentation:Presentation.numbered
      ~dimensions:Renderer.{ columns = 8; rows = 3 }
      ()
    |> must
    |> fun session ->
    App.Session.handle_pointer session
      (pointer (Input_event.Press Input_event.Primary) ~column:3 ~row:0)
  in
  expect
    ((primary_selection numbered).head_offset = 1)
    "a line-number gutter was not excluded from mouse source coordinates";
  let bare =
    App.Session.create ~model:App.Session.Direct ~contents:"a\nb\nc"
      ~presentation:Presentation.bare
      ~dimensions:Renderer.{ columns = 8; rows = 3 }
      ()
    |> must
    |> fun session ->
    App.Session.handle_pointer session
      (pointer (Input_event.Press Input_event.Primary) ~column:0 ~row:2)
  in
  expect
    ((primary_selection bare).head_offset = 4)
    "a status-free presentation rejected a click on its final source row"

let temporary_file () = Filename.temp_file "zenbu-m4-" ".txt"
let remove path = try Unix.unlink path with Unix.Unix_error _ -> ()

let test_session_file_dirty_and_models () =
  let path = temporary_file () in
  Fun.protect
    ~finally:(fun () -> remove path)
    (fun () ->
      let dimensions = { Renderer.columns = 20; rows = 4 } in
      let session =
        App.Session.create ~model:App.Session.Vim ~file_path:path
          ~contents:"abc" ~dimensions ()
        |> must
      in
      expect (not (App.Session.dirty session)) "newly loaded file is dirty";
      let session = App.Session.handle_input session (key "i") in
      expect
        (Model_status.input_mode (App.Session.status session)
        = Model_status.Text_entry)
        "Vim insert did not declare committed-text input";
      let session = App.Session.handle_input session (text_input "界") in
      let session =
        App.Session.handle_input session
          (Input_event.key_press (Input_event.named_key Input_event.Escape))
      in
      expect_string ~expected:"界abc" ~actual:(App.Session.contents session);
      expect
        (App.Session.dirty session)
        "editing did not mark the session dirty";
      expect
        (match App.Session.handle_host session App.Session.Quit with
        | App.Session.Continue _ -> true
        | App.Session.Exit _ -> false)
        "dirty quit did not require confirmation";
      let session =
        match App.Session.handle_host session App.Session.Save with
        | App.Session.Continue session -> session
        | App.Session.Exit _ -> failf "save unexpectedly exited"
      in
      expect
        (not (App.Session.dirty session))
        "save did not establish clean state";
      expect_string ~expected:"界abc"
        ~actual:(App.File_io.read path |> Result.get_ok);
      let session = App.Session.handle_input session (key "i") in
      let session = App.Session.handle_input session (text_input "x") in
      let session =
        App.Session.handle_input session
          (Input_event.key_press (Input_event.named_key Input_event.Escape))
      in
      let session = App.Session.handle_input session (key "u") in
      expect
        (not (App.Session.dirty session))
        "undoing to the saved document version did not become clean";
      expect
        (match App.Session.handle_host session App.Session.Quit with
        | App.Session.Exit _ -> true
        | App.Session.Continue _ -> false)
        "clean quit did not exit";
      let selection =
        App.Session.create ~model:App.Session.Selection ~contents:"alpha beta"
          ~dimensions ()
        |> must
        |> fun session -> App.Session.handle_input session (key "i")
      in
      expect
        (Model_status.input_mode (App.Session.status selection)
        = Model_status.Text_entry)
        "selection-first insert did not declare committed-text input";
      let selection =
        App.Session.handle_input selection
          (Input_event.key_press (Input_event.named_key Input_event.Escape))
      in
      let selection = App.Session.handle_input selection (key "W") in
      expect
        (not (App.Session.dirty selection))
        "selection-only history must not mark file contents dirty";
      let _, frame = App.Session.render selection in
      expect
        (List.mem Frame.Primary_selection (styles frame))
        "selection-first model did not render its semantic selection";
      let structural =
        App.Session.create ~model:App.Session.Structural ~language:"ocaml"
          ~contents:"let alpha = 1\nlet beta = 2\n" ~dimensions ()
        |> must
        |> fun session -> App.Session.handle_input session (key "f")
      in
      expect
        (String.starts_with ~prefix:"STRUCT |"
           (Model_status.label (App.Session.status structural)))
        "structural model did not consume the generic syntax context";
      let _, frame = App.Session.render structural in
      expect
        (List.mem Frame.Primary_selection (styles frame))
        "structural selection did not render through the existing view")

let test_session_workspace_views () =
  let dimensions = Renderer.{ columns = 24; rows = 6 } in
  let session =
    App.Session.create ~model:App.Session.Vim ~contents:"alpha\nbeta"
      ~dimensions ()
    |> must
  in
  let session =
    match App.Session.handle_host session App.Session.Split_vertical with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "split unexpectedly exited"
  in
  expect
    (App.Session.pane_count session = 2)
    "vertical split did not add a pane";
  expect
    (App.Session.focused_pane session = 1)
    "newly split pane was not focused";
  let session, composed = App.Session.render session in
  expect
    (Frame.width composed = 24 && Frame.height composed = 6)
    "workspace composition changed the terminal dimensions";
  expect
    (List.exists
       (fun row -> contains ~substring:"│" (Frame.row_text row))
       (Frame.rows composed))
    "vertical split did not render a divider";
  let session =
    match App.Session.handle_host session App.Session.Focus_next_pane with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "focus-next unexpectedly exited"
  in
  expect
    (App.Session.focused_pane session = 0)
    "focus-next did not cycle through layout order";
  let session =
    match App.Session.handle_host session App.Session.Only_pane with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "only-pane unexpectedly exited"
  in
  expect (App.Session.pane_count session = 1) "only-pane retained a split";
  let session =
    match App.Session.handle_host session App.Session.Split_horizontal with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "horizontal split unexpectedly exited"
  in
  let session, composed = App.Session.render session in
  expect
    (List.exists
       (fun row -> contains ~substring:"─" (Frame.row_text row))
       (Frame.rows composed))
    "horizontal split did not render a divider";
  let session =
    match App.Session.handle_host session App.Session.Close_pane with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "close-pane unexpectedly exited"
  in
  expect
    (App.Session.pane_count session = 1)
    "close-pane did not remove the focused view";
  expect
    (App.Session.focused_pane session = 0)
    "close-pane did not retain the remaining view";
  let session =
    match App.Session.handle_host session App.Session.New_buffer with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "new-buffer unexpectedly exited"
  in
  expect
    (App.Session.buffer_count session = 2
    && App.Session.focused_buffer session = 1)
    "new-buffer did not create and focus an independent buffer";
  let session =
    App.Session.handle_input session (key "i") |> fun session ->
    App.Session.handle_input session (text_input "delta") |> fun session ->
    App.Session.handle_input session
      (Input_event.key_press (Input_event.named_key Input_event.Escape))
  in
  expect_string ~expected:"delta" ~actual:(App.Session.contents session);
  let session =
    match App.Session.handle_host session App.Session.Next_buffer with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "next-buffer unexpectedly exited"
  in
  expect_string ~expected:"alpha\nbeta" ~actual:(App.Session.contents session);
  let session =
    match App.Session.handle_host session App.Session.Split_vertical with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "split for buffer routing unexpectedly exited"
  in
  let session =
    match App.Session.handle_host session App.Session.Next_buffer with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "next-buffer in split unexpectedly exited"
  in
  expect_string ~expected:"delta" ~actual:(App.Session.contents session);
  let session =
    match App.Session.handle_host session App.Session.Focus_next_pane with
    | App.Session.Continue session -> session
    | App.Session.Exit _ -> failf "focus buffer view unexpectedly exited"
  in
  expect_string ~expected:"alpha\nbeta" ~actual:(App.Session.contents session);
  let _, frame = App.Session.render session in
  let screen =
    Frame.rows frame |> List.map Frame.row_text |> String.concat "\n"
  in
  expect
    (contains ~substring:"alpha" screen && contains ~substring:"delta" screen)
    "separate buffers were not rendered in their assigned split views"

let host_session session command =
  match App.Session.handle_host session command with
  | App.Session.Continue session -> session
  | App.Session.Exit _ -> failf "workspace command unexpectedly exited"

let test_session_buffer_line_presentation () =
  let dimensions = Renderer.{ columns = 24; rows = 5 } in
  let session =
    App.Session.create ~model:App.Session.Direct ~contents:"abcd" ~dimensions ()
    |> must
    |> fun session ->
    App.Session.rename_buffer session ~name:"alpha" |> fun session ->
    host_session session App.Session.New_buffer |> fun session ->
    App.Session.rename_buffer session ~name:"beta" |> fun session ->
    App.Session.switch_buffer session ~buffer_id:0 |> fun session ->
    App.Session.set_presentation session ~profile:"buffered"
  in
  let session, frame = App.Session.render session in
  let rows = Frame.rows frame in
  expect
    (Frame.height frame = 5
    && String.starts_with ~prefix:"[0:alpha] 1:beta "
         (Frame.row_text (List.hd rows)))
    "the buffered presentation did not render stable buffer labels";
  expect
    (Frame.cursor frame = Some Frame.{ column = 0; row = 1 })
    "the buffer line did not translate the document cursor";
  let before = primary_selection session in
  let after_bar =
    App.Session.handle_pointer session
      (pointer (Input_event.Press Input_event.Primary) ~column:0 ~row:0)
  in
  expect
    (primary_selection after_bar = before)
    "clicking the noninteractive buffer line changed a selection";
  let after_document =
    App.Session.handle_pointer after_bar
      (pointer (Input_event.Press Input_event.Primary) ~column:2 ~row:1)
  in
  expect
    ((primary_selection after_document).head_offset = 2)
    "the buffer line did not shift document pointer coordinates";
  let unbuffered =
    App.Session.set_presentation after_document ~profile:"default"
  in
  let _, unbuffered_frame = App.Session.render unbuffered in
  expect
    (String.starts_with ~prefix:"abcd"
       (Frame.rows unbuffered_frame |> List.hd |> Frame.row_text))
    "disabling the buffer line did not return the top row to the document"

let test_keyboard_viewport_commands () =
  let dimensions = Renderer.{ columns = 20; rows = 5 } in
  let contents = "zero\none\ntwo\nthree\nfour\nfive\nsix\nseven" in
  let session =
    App.Session.create ~model:App.Session.Direct ~contents ~dimensions ()
    |> must
  in
  let original_selection = primary_selection session in
  let session = host_session session App.Session.View_page_down in
  expect
    ((App.Session.viewport session).top_line = 4)
    "a page-down host command did not use the rendered source-row height";
  expect
    (not (App.Session.viewport session).follow_cursor)
    "page navigation did not retain an explicit viewport position";
  expect
    (primary_selection session = original_selection
    && App.Session.contents session = contents)
    "page navigation changed semantic document state";
  let session = host_session session App.Session.View_scroll_up in
  expect
    ((App.Session.viewport session).top_line = 3)
    "a line-scroll host command did not move one source row";
  let session = host_session session App.Session.View_page_up in
  expect
    ((App.Session.viewport session).top_line = 0)
    "a page-up host command did not clamp at the top of the buffer";
  let session =
    session |> fun session ->
    App.Session.handle_input session (named Input_event.Arrow_down)
    |> fun session ->
    App.Session.handle_input session (named Input_event.Arrow_down)
    |> fun session ->
    App.Session.handle_input session (named Input_event.Arrow_down)
    |> fun session ->
    App.Session.handle_input session (named Input_event.Arrow_down)
  in
  let selection_before_center = primary_selection session in
  let session = host_session session App.Session.View_center in
  expect
    ((App.Session.viewport session).top_line = 2)
    "center-view did not center the primary selection in the source canvas";
  expect
    (primary_selection session = selection_before_center)
    "center-view changed the primary selection";
  let bare =
    App.Session.create ~model:App.Session.Direct ~presentation:Presentation.bare
      ~contents ~dimensions ()
    |> must
  in
  let bare = host_session bare App.Session.View_page_down in
  expect
    ((App.Session.viewport bare).top_line = 3)
    "page navigation did not account for a presentation with no status row"

let test_workspace_view_positions () =
  let dimensions = Renderer.{ columns = 100; rows = 6 } in
  let session =
    App.Session.create ~model:App.Session.Direct ~contents:"abcd" ~dimensions ()
    |> must
  in
  let session =
    session |> fun session ->
    App.Session.handle_input session (named Input_event.Arrow_right)
    |> fun session ->
    App.Session.handle_input session (named Input_event.Arrow_right)
    |> fun session -> host_session session App.Session.Split_vertical
  in
  let session =
    session |> fun session ->
    App.Session.handle_input session (named Input_event.Arrow_right)
    |> fun session ->
    App.Session.handle_input session (named Input_event.Arrow_right)
  in
  let session, frame = App.Session.render session in
  let screen =
    Frame.rows frame |> List.map Frame.row_text |> String.concat "\n"
  in
  expect
    (contains ~substring:"1:3" screen && contains ~substring:"1:5" screen)
    "split panes did not render their independently saved caret positions";
  let session =
    host_session session App.Session.Focus_next_pane |> fun session ->
    App.Session.handle_input session (text_input "X")
  in
  expect_string ~expected:"abXcd" ~actual:(App.Session.contents session);
  let session =
    host_session session App.Session.Focus_next_pane |> fun session ->
    App.Session.handle_input session (text_input "Y")
  in
  expect_string ~expected:"abXcdY" ~actual:(App.Session.contents session);
  expect
    (App.Session.pane_count session = 2)
    "view-position restoration changed the split layout"

let test_session_open_buffer_prompt () =
  let path = temporary_file () in
  Fun.protect
    ~finally:(fun () -> remove path)
    (fun () ->
      (match App.File_io.save_atomic ~path ~contents:"opened buffer" with
      | Ok () -> ()
      | Error error -> failf "%s" (App.File_io.to_string error));
      let dimensions = Renderer.{ columns = 24; rows = 6 } in
      let session =
        App.Session.create ~model:App.Session.Vim ~contents:"original"
          ~dimensions ()
        |> must
      in
      let session =
        match App.Session.handle_host session App.Session.Open_buffer with
        | App.Session.Continue session -> session
        | App.Session.Exit _ -> failf "open-buffer unexpectedly exited"
      in
      expect
        (Model_status.input_mode (App.Session.status session)
        = Model_status.Text_entry)
        "open-buffer did not enter a text prompt";
      let session =
        App.Session.handle_input session (text_input path) |> fun session ->
        App.Session.handle_input session
          (Input_event.key_press (Input_event.named_key Input_event.Enter))
      in
      expect_string ~expected:"opened buffer"
        ~actual:(App.Session.contents session);
      expect
        (App.Session.buffer_count session = 2)
        "open-buffer did not retain the original buffer";
      expect
        (App.Session.file_path session = Some path)
        "open-buffer did not retain the file path";
      let session =
        match App.Session.handle_host session App.Session.Open_buffer with
        | App.Session.Continue session -> session
        | App.Session.Exit _ -> failf "duplicate open unexpectedly exited"
      in
      let session =
        App.Session.handle_input session (text_input path) |> fun session ->
        App.Session.handle_input session
          (Input_event.key_press (Input_event.named_key Input_event.Enter))
      in
      expect
        (App.Session.buffer_count session = 2)
        "opening an existing path duplicated its buffer")

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
    ("terminal input disposition", test_input_decoder_is_model_neutral);
    ("binding sequence parser", test_binding_sequence_parser);
    ("theme contract", test_theme_contract);
    ("presentation profile contract", test_presentation_profile_contract);
    ("display coordinates", test_display_coordinates);
    ("pointer selection and scroll", test_pointer_selection_and_scroll);
    ( "renderer selections viewport tiny",
      test_renderer_selection_viewport_and_tiny_terminal );
    ("renderer presentation profiles", test_renderer_presentation_profiles);
    ("session presentation profile", test_session_presentation_profile);
    ("session buffer-line presentation", test_session_buffer_line_presentation);
    ("pure pane layout composition", test_layout_composition);
    ("session file dirty and model host", test_session_file_dirty_and_models);
    ("session workspace views", test_session_workspace_views);
    ("keyboard viewport commands", test_keyboard_viewport_commands);
    ("workspace view positions", test_workspace_view_positions);
    ("session open-buffer prompt", test_session_open_buffer_prompt);
  ]
  |> List.iter (fun (name, test) -> run name test)
