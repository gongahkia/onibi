type t = {
  terminal : Notty_unix.Term.t;
  original_input : Unix.terminal_io option;
  mutable released : bool;
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
            Notty_unix.Term.create ~dispose:true ~mouse:false ~bpaste:false ();
          original_input;
          released = false;
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

let read terminal =
  match Notty_unix.Term.event terminal.terminal with
  | `Resize (columns, rows) -> Event.Resize { columns; rows }
  | `End -> Event.End
  | `Key key -> key_of_notty key
  | `Mouse _ -> Event.Unsupported "mouse input is not enabled"
  | `Paste _ -> Event.Unsupported "bracketed paste is not enabled"

let attribute = function
  | Frame.Plain -> Notty.A.empty
  | Frame.Primary_selection -> Notty.A.(bg blue ++ fg white)
  | Frame.Secondary_selection -> Notty.A.(st underline)
  | Frame.Status -> Notty.A.(bg lightblack ++ fg white)
  | Frame.Message -> Notty.A.(bg yellow ++ fg black)
  | Frame.Dim -> Notty.A.(fg lightblack)

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
