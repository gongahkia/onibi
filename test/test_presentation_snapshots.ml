open Zenbu_kernel
open Zenbu_model_api
module App = Zenbu_app
module Frame = Zenbu_view.Frame
module Presentation = Zenbu_view.Presentation
module Renderer = Zenbu_view.Renderer
module Scripting = Zenbu_scripting.Scripting
module Theme = Zenbu_view.Theme

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
  | None -> failf "missing presentation artifact %s" path

let key text = Input_event.logical_text text |> must |> Input_event.key_press

let ctrl text =
  Input_event.logical_text text
  |> must
  |> Input_event.key_press ~modifiers:[ Input_event.Control ]

let shift_arrow_right =
  Input_event.key_press ~modifiers:[ Input_event.Shift ]
    (Input_event.named_key Input_event.Arrow_right)

let host session command =
  match App.Session.handle_host session command with
  | App.Session.Continue session -> session
  | App.Session.Exit _ -> failf "snapshot setup unexpectedly exited"

type rendered = {
  frame : Frame.t;
  theme : Theme.t;
  presentation : Presentation.t;
  focused_pane : int option;
}

type fixture = {
  id : string;
  classification : string;
  review_reason : string;
  render : unit -> rendered;
}

let document_context ~selections ~primary contents =
  let id = Document_id.of_string "presentation-snapshot" |> must in
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

let diagnostic_unicode () =
  let context =
    document_context ~selections:[ (0, 3) ] ~primary:0 "界= 1\nwarn"
  in
  let rendered =
    Renderer.render_with_inspector ~inspector:None
      ~presentation:Presentation.relative
      ~diagnostic_ranges:
        [ Renderer.{ start_offset = 3; stop_offset = 4; kind = Error } ]
      ~context
      ~status:(Model_status.create ~id:"snapshot" ~label:"SNAPSHOT" () |> must)
      ~filename:"diagnostic.zenbu" ~dirty:true
      ~message:(Some "semantic-frame fixture")
      ~viewport:Zenbu_view.Viewport.origin
      ~dimensions:Renderer.{ columns = 24; rows = 4 }
      ()
  in
  {
    frame = rendered.frame;
    theme = Theme.dark;
    presentation = Presentation.relative;
    focused_pane = None;
  }

let with_session session run =
  Fun.protect
    ~finally:(fun () -> App.Session.close session)
    (fun () ->
      let session, frame = run session in
      {
        frame;
        theme = App.Session.theme session;
        focused_pane = Some (App.Session.focused_pane session);
        presentation =
          (match App.Session.pane_presentation session ~pane:0 with
          | Some name -> Presentation.find_builtin name |> Option.get
          | None -> failf "snapshot session has no focused-pane presentation");
      })

let helix_relative_page () =
  let session =
    App.Session.create ~model:App.Session.Selection ~theme:Theme.dark
      ~presentation:Presentation.relative
      ~config:(Scripting.Explicit (artifact "examples/helix-adapter.lua"))
      ~contents:"zero\none\ntwo\nthree\nfour\nfive\nsix\nseven"
      ~dimensions:Renderer.{ columns = 24; rows = 5 }
      ()
    |> must
  in
  with_session session (fun session ->
      let session =
        App.Session.handle_input session
          (Input_event.key_press (Input_event.named_key Input_event.Page_down))
      in
      expect
        ((App.Session.viewport session).top_line = 4
        && not (App.Session.viewport session).follow_cursor)
        "Helix-style page adapter did not retain its explicit viewport";
      let followed = App.Session.handle_input session (key "l") in
      expect (App.Session.viewport followed).follow_cursor
        "a caret movement did not restore cursor-following after page movement";
      let session, frame = App.Session.render session in
      expect
        ((App.Session.viewport session).top_line = 4)
        "rendering reset the Helix-style adapter viewport";
      (session, frame))

let micro_buffered_split () =
  let session =
    App.Session.create ~model:App.Session.Direct ~theme:Theme.dark
      ~presentation:Presentation.buffered
      ~config:(Scripting.Explicit (artifact "examples/micro-adapter.lua"))
      ~contents:"界 micro\nstatus"
      ~dimensions:Renderer.{ columns = 26; rows = 7 }
      ()
    |> must
  in
  with_session session (fun session ->
      let session = host session App.Session.Split_vertical in
      let session = App.Session.handle_input session (ctrl "w") in
      let session = App.Session.handle_input session shift_arrow_right in
      let session = App.Session.set_theme session ~theme:"not-a-theme" in
      expect
        (String.equal (Theme.name (App.Session.theme session)) "dark")
        "theme fallback replaced the selected dark theme";
      App.Session.render session)

let emacs_numbered_split () =
  let session =
    App.Session.create ~model:App.Session.Direct ~theme:Theme.light
      ~presentation:Presentation.numbered
      ~config:(Scripting.Explicit (artifact "examples/emacs-adapter.lua"))
      ~contents:"alpha\nbeta"
      ~dimensions:Renderer.{ columns = 26; rows = 8 }
      ()
    |> must
  in
  with_session session (fun session ->
      let session = App.Session.handle_input session (ctrl "x") in
      let session = App.Session.handle_input session (key "2") in
      let session = App.Session.handle_input session shift_arrow_right in
      let session = App.Session.handle_input session (ctrl "w") in
      let session = App.Session.handle_input session (ctrl "y") in
      App.Session.render session)

let numbered_tiny () =
  let session =
    App.Session.create ~model:App.Session.Direct ~theme:Theme.default
      ~presentation:Presentation.numbered ~contents:"界"
      ~dimensions:Renderer.{ columns = 1; rows = 2 }
      ()
    |> must
  in
  with_session session App.Session.render

let fixtures =
  [
    {
      id = "zenbu-owned-relative-unicode-diagnostics";
      classification = "zenbu-owned profile";
      review_reason =
        "initial semantic frame for unicode selection and diagnostic styling";
      render = diagnostic_unicode;
    };
    {
      id = "helix-style-relative-page-adapter";
      classification = "Helix-style adapter";
      review_reason =
        "initial adapter-owned page movement with Zenbu relative chrome";
      render = helix_relative_page;
    };
    {
      id = "micro-style-buffered-split-adapter";
      classification = "Micro-style adapter";
      review_reason =
        "initial buffered split frame, focus routing, and retained theme \
         fallback";
      render = micro_buffered_split;
    };
    {
      id = "emacs-style-numbered-split-adapter";
      classification = "Emacs-style adapter";
      review_reason =
        "initial numbered horizontal split and bounded kill/yank adapter frame";
      render = emacs_numbered_split;
    };
    {
      id = "zenbu-owned-numbered-tiny";
      classification = "zenbu-owned profile";
      review_reason = "initial safe cursorless frame for a gutter-only terminal";
      render = numbered_tiny;
    };
  ]

let ansi_name = function
  | Theme.Black -> "black"
  | Theme.Red -> "red"
  | Theme.Green -> "green"
  | Theme.Yellow -> "yellow"
  | Theme.Blue -> "blue"
  | Theme.Magenta -> "magenta"
  | Theme.Cyan -> "cyan"
  | Theme.White -> "white"
  | Theme.Light_black -> "light-black"
  | Theme.Light_red -> "light-red"
  | Theme.Light_green -> "light-green"
  | Theme.Light_yellow -> "light-yellow"
  | Theme.Light_blue -> "light-blue"
  | Theme.Light_magenta -> "light-magenta"
  | Theme.Light_cyan -> "light-cyan"
  | Theme.Light_white -> "light-white"

let color_name = function
  | Theme.Default -> "default"
  | Theme.Ansi value -> "ansi:" ^ ansi_name value
  | Theme.Rgb (red, green, blue) ->
      Printf.sprintf "rgb:%02x%02x%02x" red green blue

let decoration_name = function
  | Theme.Bold -> "bold"
  | Theme.Italic -> "italic"
  | Theme.Underline -> "underline"

let attribute_name (attribute : Theme.attribute) =
  Printf.sprintf "fg=%s bg=%s decorations=%s"
    (color_name attribute.foreground)
    (color_name attribute.background)
    (attribute.decorations |> List.map decoration_name |> String.concat ",")

let escaped_text text =
  let buffer = Buffer.create (String.length text + 2) in
  Buffer.add_char buffer '"';
  String.iter
    (function
      | '"' -> Buffer.add_string buffer "\\\""
      | '\\' -> Buffer.add_string buffer "\\\\"
      | '\n' -> Buffer.add_string buffer "\\n"
      | '\r' -> Buffer.add_string buffer "\\r"
      | '\t' -> Buffer.add_string buffer "\\t"
      | value -> Buffer.add_char buffer value)
    text;
  Buffer.add_char buffer '"';
  Buffer.contents buffer

let used_styles frame =
  Frame.rows frame |> List.concat
  |> List.map (fun cell -> cell.Frame.style)
  |> List.sort_uniq (fun left right ->
      String.compare (Theme.style_name left) (Theme.style_name right))

let cell_text (cell : Frame.cell) =
  Printf.sprintf "%s width=%d text=%s"
    (Theme.style_name cell.style)
    cell.width (escaped_text cell.text)

let frame_snapshot fixture rendered =
  let frame = rendered.frame in
  let cursor =
    match Frame.cursor frame with
    | None -> "none"
    | Some cursor -> Printf.sprintf "%d:%d" cursor.column cursor.row
  in
  let palette =
    used_styles frame
    |> List.map (fun style ->
        Printf.sprintf "palette %s: %s" (Theme.style_name style)
          (Theme.attribute rendered.theme style |> attribute_name))
  in
  let focused_pane =
    match rendered.focused_pane with
    | None -> "none"
    | Some pane -> string_of_int pane
  in
  let rows =
    Frame.rows frame
    |> List.mapi (fun index row ->
        Printf.sprintf "row %d: %s" index
          (row |> List.map cell_text |> String.concat " | "))
  in
  String.concat "\n"
    ([
       "zenbu-frame-snapshot-v1";
       "fixture: " ^ fixture.id;
       "classification: " ^ fixture.classification;
       "review-reason: " ^ fixture.review_reason;
       "profile: " ^ Presentation.name rendered.presentation;
       "theme: " ^ Theme.name rendered.theme;
       "focused-pane: " ^ focused_pane;
       Printf.sprintf "frame: %dx%d" (Frame.width frame) (Frame.height frame);
       "cursor: " ^ cursor;
     ]
    @ palette @ rows)
  ^ "\n"

let snapshot_path fixture =
  artifact ("test/fixtures/presentation/" ^ fixture.id ^ ".snapshot")

let run fixture =
  expect
    (String.length (String.trim fixture.review_reason) > 0)
    "%s: snapshot updates require an explicit review reason" fixture.id;
  let actual = fixture.render () |> frame_snapshot fixture in
  let expected = snapshot_path fixture |> read in
  if not (String.equal expected actual) then
    failf "%s: semantic frame snapshot changed\n--- expected\n%s--- actual\n%s"
      fixture.id expected actual

let () =
  try
    List.iter
      (fun fixture ->
        run fixture;
        Printf.printf "ok: %s\n" fixture.id)
      fixtures
  with Test_failure message ->
    Printf.eprintf "FAILED: %s\n" message;
    exit 1
