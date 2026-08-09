open Zenbu_kernel
open Zenbu_model_api
module App = Zenbu_app
module Display = Zenbu_view.Display
module Frame = Zenbu_view.Frame
module Renderer = Zenbu_view.Renderer
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

let must = function
  | Ok value -> value
  | Error error -> failf "%s" (Error.to_string error)

let key text = Input_event.key_press (Input_event.logical_text text |> must)
let text_input text = Input_event.text_input text |> must

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
    "named terminal keys must remain logical key presses"

let test_display_coordinates () =
  let line = List.hd (Display.lines "é\t界\r") in
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
        "control rendering lost its display coordinates"
  | _ -> failf "unexpected grapheme segmentation"

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
  let tiny =
    Renderer.render ~context
      ~status:(status Model_status.Key_commands)
      ~filename:"tiny" ~dirty:false ~message:None
      ~viewport:Zenbu_view.Viewport.origin ~dimensions:{ columns = 5; rows = 1 }
  in
  expect
    (List.length (Frame.rows tiny.frame) = 1)
    "tiny terminals need a safe fallback frame"

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
        "selection-first model did not render its semantic selection")

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
    ("display coordinates", test_display_coordinates);
    ( "renderer selections viewport tiny",
      test_renderer_selection_viewport_and_tiny_terminal );
    ("session file dirty and model host", test_session_file_dirty_and_models);
  ]
  |> List.iter (fun (name, test) -> run name test)
