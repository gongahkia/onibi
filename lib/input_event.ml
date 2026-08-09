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

type t =
  | Key_press of {
      key : key;
      modifiers : modifier list;
      physical_key : physical_key option;
    }
  | Text_input of string

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
    Error (Error.Invalid_input_event "physical key identifier must not be empty")
  else Ok value

let logical_text text =
  match valid_text text with Error _ as error -> error | Ok text -> Ok (Logical_text text)

let named_key value = Named_key value

let key_press ?(modifiers = []) ?physical_key key =
  Key_press { key; modifiers = normalize_modifiers modifiers; physical_key }

let text_input text =
  match valid_text text with Error _ as error -> error | Ok text -> Ok (Text_input text)

let modifiers = function Key_press value -> value.modifiers | Text_input _ -> []
let key = function Key_press value -> Some value.key | Text_input _ -> None
let physical = function Key_press value -> value.physical_key | Text_input _ -> None
let text = function Text_input value -> Some value | Key_press _ -> None

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

let to_string = function
  | Text_input text -> "text-input(" ^ String.escaped text ^ ")"
  | Key_press { key; modifiers; physical_key = _ } ->
      let prefix =
        String.concat "+" (List.map modifier_to_string modifiers)
      in
      let key =
        match key with
        | Logical_text text -> "text(" ^ String.escaped text ^ ")"
        | Named_key named -> named_key_to_string named
      in
      if String.length prefix = 0 then key else prefix ^ "+" ^ key
