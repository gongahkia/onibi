type t = { terminal : Notty_unix.Term.t; mutable released : bool }

open Zenbu_view

let terminal_error exception_ = Printexc.to_string exception_

let create () =
  if not (Unix.isatty Unix.stdin) then Error "standard input is not a terminal"
  else if not (Unix.isatty Unix.stdout) then Error "standard output is not a terminal"
  else
    try
      (* [dispose] retains a process-exit fallback if initialization is only
         partially completed before an exception reaches the host. *)
      Ok
        {
          terminal =
            Notty_unix.Term.create ~dispose:true ~mouse:false ~bpaste:false ();
          released = false;
        }
    with exception_ -> Error (terminal_error exception_)

let release terminal =
  if not terminal.released then (
    terminal.released <- true;
    Notty_unix.Term.release terminal.terminal)

let with_terminal run =
  match create () with
  | Error _ as error -> error
  | Ok terminal ->
      Fun.protect ~finally:(fun () -> release terminal) (fun () -> Ok (run terminal))

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
  match key with
  | `ASCII character -> Event.Key { key = Event.Text (String.make 1 character); modifiers }
  | `Uchar uchar ->
      let buffer = Buffer.create 4 in
      Buffer.add_utf_8_uchar buffer uchar;
      Event.Key { key = Event.Text (Buffer.contents buffer); modifiers }
  | (#Notty.Unescape.special as value) -> (
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
  row |> List.map image_of_cell |> Notty.I.hcat |> Notty.I.hsnap ~align:`Left width

let draw terminal frame =
  let rows = Frame.rows frame |> List.map (image_of_row (Frame.width frame)) in
  let image = Notty.I.vcat rows |> Notty.I.vsnap ~align:`Top (Frame.height frame) in
  Notty_unix.Term.image terminal.terminal image;
  Notty_unix.Term.cursor terminal.terminal
    (Frame.cursor frame |> Option.map (fun cursor -> (cursor.Frame.column, cursor.row)))
