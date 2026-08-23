type t = {
  terminal : Notty_unix.Term.t;
  original_input : Unix.terminal_io option;
  mutable released : bool;
  mutable paste : Buffer.t option;
}

open Zenbu_view

let terminal_error exception_ = Printexc.to_string exception_

let restore_partial_terminal original_input =
  Option.iter
    (fun attributes ->
      try Unix.tcsetattr Unix.stdin Unix.TCSANOW attributes
      with Unix.Unix_error _ -> ())
    original_input;
  try
    output_string stdout
      "\027[?25h\027[?1000;1002;1005;1015;1006l\027[?2004l\027[?1049l";
    flush stdout
  with Sys_error _ -> ()

let create () =
  if not (Unix.isatty Unix.stdin) then Error "standard input is not a terminal"
  else if not (Unix.isatty Unix.stdout) then
    Error "standard output is not a terminal"
  else
    let original_input =
      try Some (Unix.tcgetattr Unix.stdin) with Unix.Unix_error _ -> None
    in
    try
      (* [dispose] keeps a process-exit cleanup fallback after successful
         creation; the exception path below handles partial creation. *)
      Ok
        {
          terminal =
            Notty_unix.Term.create ~dispose:true ~mouse:false ~bpaste:true ();
          original_input;
          released = false;
          paste = None;
        }
    with exception_ ->
      restore_partial_terminal original_input;
      Error (terminal_error exception_)

let release terminal =
  if not terminal.released then (
    terminal.released <- true;
    try Notty_unix.Term.release terminal.terminal
    with exception_ ->
      restore_partial_terminal terminal.original_input;
      raise exception_)

let with_terminal run =
  match create () with
  | Error _ as error -> error
  | Ok terminal ->
      Fun.protect
        ~finally:(fun () -> release terminal)
        (fun () -> Ok (run terminal))

let size terminal =
  let columns, rows = Notty_unix.Term.size terminal.terminal in
  (columns, rows)

let modifier = function
  | `Shift -> Event.Shift
  | `Ctrl -> Event.Control
  | `Meta -> Event.Meta

let map_special = function
  | `Escape -> Some Event.Escape
  | `Enter -> Some Event.Enter
  | `Tab -> Some Event.Tab
  | `Backspace -> Some Event.Backspace
  | `Delete -> Some Event.Delete
  | `Home -> Some Event.Home
  | `End -> Some Event.End
  | `Arrow `Up -> Some Event.Arrow_up
  | `Arrow `Down -> Some Event.Arrow_down
  | `Arrow `Left -> Some Event.Arrow_left
  | `Arrow `Right -> Some Event.Arrow_right
  | `Insert -> None
  | `Page _ -> None
  | `Function _ -> None

let key_of_notty (key, modifiers) =
  let modifiers = Event.normalize_modifiers (List.map modifier modifiers) in
  let normalize_control_character character =
    if List.mem Event.Control modifiers && character >= 'A' && character <= 'Z'
    then Char.lowercase_ascii character
    else character
  in
  match key with
  | `ASCII character ->
      Event.Key
        {
          key =
            Event.Text (String.make 1 (normalize_control_character character));
          modifiers;
        }
  | `Uchar uchar ->
      let buffer = Buffer.create 4 in
      Buffer.add_utf_8_uchar buffer uchar;
      Event.Key { key = Event.Text (Buffer.contents buffer); modifiers }
  | #Notty.Unescape.special as value -> (
      match map_special value with
      | Some key -> Event.Key { key; modifiers }
      | None -> Event.Unsupported "unsupported terminal special key")

let pasted_text = function
  | Event.Key { key = Event.Text text; modifiers = [] } -> Some text
  | Event.Key { key = Event.Enter; modifiers = [] } -> Some "\n"
  | Event.Key { key = Event.Tab; modifiers = [] } -> Some "\t"
  | Event.Key _ | Event.Resize _ | Event.Paste _ | Event.Wakeup | Event.End
  | Event.Unsupported _ ->
      None

let rec read ?wakeup ?(wakeups = []) terminal =
  let wakeups = Option.to_list wakeup @ wakeups |> List.sort_uniq compare in
  match wakeups with
  | [] -> read_ready terminal
  | _ ->
      let readable, _, _ = Unix.select (Unix.stdin :: wakeups) [] [] (-1.) in
      if List.exists (fun wakeup -> List.mem wakeup readable) wakeups then
        Event.Wakeup
      else read_ready terminal

and read_ready terminal =
  match Notty_unix.Term.event terminal.terminal with
  | `Paste `Start ->
      terminal.paste <- Some (Buffer.create 128);
      read_ready terminal
  | `Paste `End -> (
      match terminal.paste with
      | None -> Event.Unsupported "unexpected bracketed-paste terminator"
      | Some buffer ->
          terminal.paste <- None;
          Event.Paste (Buffer.contents buffer))
  | `Key key -> (
      let event = key_of_notty key in
      match terminal.paste with
      | None -> event
      | Some buffer ->
          Option.iter (Buffer.add_string buffer) (pasted_text event);
          read_ready terminal)
  | `Resize (columns, rows) -> Event.Resize { columns; rows }
  | `End -> Event.End
  | `Mouse _ -> Event.Unsupported "mouse input is not enabled"

let attribute = function
  | Frame.Plain -> Notty.A.empty
  | Frame.Primary_selection -> Notty.A.(bg blue ++ fg white)
  | Frame.Secondary_selection -> Notty.A.(st underline)
  | Frame.Status -> Notty.A.(bg lightblack ++ fg white)
  | Frame.Message -> Notty.A.(bg yellow ++ fg black)
  | Frame.Dim -> Notty.A.(fg lightblack)
  | Frame.Search_match -> Notty.A.(bg yellow ++ fg black)
  | Frame.Diagnostic_error -> Notty.A.(fg red ++ st underline)
  | Frame.Diagnostic_warning -> Notty.A.(fg yellow ++ st underline)
  | Frame.Diagnostic_information -> Notty.A.(fg cyan ++ st underline)
  | Frame.Diagnostic_hint -> Notty.A.(fg lightblack ++ st underline)
  | Frame.Syntax_keyword -> Notty.A.(fg cyan ++ st bold)
  | Frame.Syntax_string -> Notty.A.(fg green)
  | Frame.Syntax_number -> Notty.A.(fg magenta)
  | Frame.Syntax_comment -> Notty.A.(fg lightblack ++ st italic)
  | Frame.Syntax_type -> Notty.A.(fg blue ++ st bold)
  | Frame.Syntax_constructor -> Notty.A.(fg yellow)
  | Frame.Overlay -> Notty.A.(bg lightblack ++ fg white)

let image_of_cell cell =
  let image = Notty.I.string (attribute cell.Frame.style) cell.text in
  Notty.I.hsnap ~align:`Left cell.width image

let image_of_row width row =
  row |> List.map image_of_cell |> Notty.I.hcat
  |> Notty.I.hsnap ~align:`Left width

let draw terminal frame =
  let rows = Frame.rows frame |> List.map (image_of_row (Frame.width frame)) in
  let image =
    Notty.I.vcat rows |> Notty.I.vsnap ~align:`Top (Frame.height frame)
  in
  Notty_unix.Term.image terminal.terminal image;
  Notty_unix.Term.cursor terminal.terminal
    (Frame.cursor frame
    |> Option.map (fun cursor -> (cursor.Frame.column, cursor.row)))
