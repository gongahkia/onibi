type t = {
  terminal : Notty_unix.Term.t;
  original_input : Unix.terminal_io option;
  mutable released : bool;
  mutable paste : Buffer.t option;
  mutable theme : Zenbu_view.Theme.t;
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
          theme = Theme.default;
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

let set_theme terminal theme = terminal.theme <- theme

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

let notty_color = function
  | Theme.Default -> None
  | Theme.Ansi Theme.Black -> Some Notty.A.black
  | Theme.Ansi Theme.Red -> Some Notty.A.red
  | Theme.Ansi Theme.Green -> Some Notty.A.green
  | Theme.Ansi Theme.Yellow -> Some Notty.A.yellow
  | Theme.Ansi Theme.Blue -> Some Notty.A.blue
  | Theme.Ansi Theme.Magenta -> Some Notty.A.magenta
  | Theme.Ansi Theme.Cyan -> Some Notty.A.cyan
  | Theme.Ansi Theme.White -> Some Notty.A.white
  | Theme.Ansi Theme.Light_black -> Some Notty.A.lightblack
  | Theme.Ansi Theme.Light_red -> Some Notty.A.lightred
  | Theme.Ansi Theme.Light_green -> Some Notty.A.lightgreen
  | Theme.Ansi Theme.Light_yellow -> Some Notty.A.lightyellow
  | Theme.Ansi Theme.Light_blue -> Some Notty.A.lightblue
  | Theme.Ansi Theme.Light_magenta -> Some Notty.A.lightmagenta
  | Theme.Ansi Theme.Light_cyan -> Some Notty.A.lightcyan
  | Theme.Ansi Theme.Light_white -> Some Notty.A.lightwhite
  | Theme.Rgb (red, green, blue) ->
      Some (Notty.A.rgb_888 ~r:red ~g:green ~b:blue)

let notty_decoration = function
  | Theme.Bold -> Notty.A.bold
  | Theme.Italic -> Notty.A.italic
  | Theme.Underline -> Notty.A.underline

let attribute theme style =
  let attribute = Theme.attribute theme style in
  let value =
    match notty_color attribute.foreground with
    | None -> Notty.A.empty
    | Some color -> Notty.A.fg color
  in
  let value =
    match notty_color attribute.background with
    | None -> value
    | Some color -> Notty.A.(value ++ bg color)
  in
  List.fold_left
    (fun value decoration ->
      Notty.A.(value ++ st (notty_decoration decoration)))
    value attribute.decorations

let image_of_cell theme cell =
  let image = Notty.I.string (attribute theme cell.Frame.style) cell.text in
  Notty.I.hsnap ~align:`Left cell.width image

let image_of_row theme width row =
  row
  |> List.map (image_of_cell theme)
  |> Notty.I.hcat
  |> Notty.I.hsnap ~align:`Left width

let draw terminal frame =
  let rows =
    Frame.rows frame
    |> List.map (image_of_row terminal.theme (Frame.width frame))
  in
  let image =
    Notty.I.vcat rows |> Notty.I.vsnap ~align:`Top (Frame.height frame)
  in
  Notty_unix.Term.image terminal.terminal image;
  Notty_unix.Term.cursor terminal.terminal
    (Frame.cursor frame
    |> Option.map (fun cursor -> (cursor.Frame.column, cursor.row)))
