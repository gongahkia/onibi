open Zenbu_kernel

type modifier = Shift | Control | Alt | Meta
type physical_key = string

type named_key =
  | Escape
  | Enter
  | Backspace
  | Tab
  | Delete
  | Arrow_up
  | Arrow_down
  | Arrow_left
  | Arrow_right
  | Home
  | End

type key = Logical_text of string | Named_key of named_key

type mouse_button = Primary | Middle | Secondary | Wheel_up | Wheel_down
type mouse_action = Press of mouse_button | Drag | Release

type t =
  | Key_press of {
      key : key;
      modifiers : modifier list;
      physical_key : physical_key option;
    }
  | Text_input of string
  | Mouse of {
      action : mouse_action;
      column : int;
      row : int;
      modifiers : modifier list;
    }

let modifier_rank = function Shift -> 0 | Control -> 1 | Alt -> 2 | Meta -> 3

let normalize_modifiers modifiers =
  List.sort_uniq
    (fun left right -> Int.compare (modifier_rank left) (modifier_rank right))
    modifiers

let valid_text text =
  match Text_buffer.of_utf8 text with
  | Error _ as error -> error
  | Ok _ when String.length text = 0 ->
      Error (Error.Invalid_input_event "text must not be empty")
  | Ok _ -> Ok text

let physical_key value =
  if String.length value = 0 then
    Error
      (Error.Invalid_input_event "physical key identifier must not be empty")
  else Ok value

let logical_text text =
  match valid_text text with
  | Error _ as error -> error
  | Ok text -> Ok (Logical_text text)

let named_key value = Named_key value

let key_press ?(modifiers = []) ?physical_key key =
  Key_press { key; modifiers = normalize_modifiers modifiers; physical_key }

let text_input text =
  match valid_text text with
  | Error _ as error -> error
  | Ok text -> Ok (Text_input text)

let mouse ?(modifiers = []) action ~column ~row =
  if column < 0 || row < 0 then
    Error
      (Error.Invalid_input_event "mouse coordinates must not be negative")
  else Ok (Mouse { action; column; row; modifiers = normalize_modifiers modifiers })

let modifiers = function
  | Key_press value -> value.modifiers
  | Text_input _ -> []
  | Mouse value -> value.modifiers

let key = function
  | Key_press value -> Some value.key
  | Text_input _ | Mouse _ -> None

let physical = function
  | Key_press value -> value.physical_key
  | Text_input _ | Mouse _ -> None

let text = function
  | Text_input value -> Some value
  | Key_press _ | Mouse _ -> None

let mouse_action = function Mouse value -> Some value.action | Key_press _ | Text_input _ -> None

let mouse_position = function
  | Mouse value -> Some (value.column, value.row)
  | Key_press _ | Text_input _ -> None

let modifier_to_string = function
  | Shift -> "Shift"
  | Control -> "Ctrl"
  | Alt -> "Alt"
  | Meta -> "Meta"

let named_key_to_string = function
  | Escape -> "Escape"
  | Enter -> "Enter"
  | Backspace -> "Backspace"
  | Tab -> "Tab"
  | Delete -> "Delete"
  | Arrow_up -> "ArrowUp"
  | Arrow_down -> "ArrowDown"
  | Arrow_left -> "ArrowLeft"
  | Arrow_right -> "ArrowRight"
  | Home -> "Home"
  | End -> "End"

let mouse_button_to_string = function
  | Primary -> "primary"
  | Middle -> "middle"
  | Secondary -> "secondary"
  | Wheel_up -> "wheel-up"
  | Wheel_down -> "wheel-down"

let mouse_action_to_string = function
  | Press button -> "press:" ^ mouse_button_to_string button
  | Drag -> "drag"
  | Release -> "release"

let to_string = function
  | Text_input text -> "text-input(" ^ String.escaped text ^ ")"
  | Key_press { key; modifiers; physical_key = _ } ->
      let prefix = String.concat "+" (List.map modifier_to_string modifiers) in
      let key =
        match key with
        | Logical_text text -> "text(" ^ String.escaped text ^ ")"
        | Named_key named -> named_key_to_string named
      in
      if String.length prefix = 0 then key else prefix ^ "+" ^ key
  | Mouse { action; column; row; modifiers } ->
      let prefix = String.concat "+" (List.map modifier_to_string modifiers) in
      let event =
        Printf.sprintf "mouse(%s@%d,%d)" (mouse_action_to_string action) column
          row
      in
      if String.length prefix = 0 then event else prefix ^ "+" ^ event
