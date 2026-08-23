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
    Error (Error.Invalid_input_event "mouse coordinates must not be negative")
  else
    Ok
      (Mouse { action; column; row; modifiers = normalize_modifiers modifiers })

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

let mouse_action = function
  | Mouse value -> Some value.action
  | Key_press _ | Text_input _ -> None

let mouse_position = function
  | Mouse value -> Some (value.column, value.row)
  | Key_press _ | Text_input _ -> None

let modifier_of_binding_name = function
  | "Shift" -> Some Shift
  | "Ctrl" | "Control" -> Some Control
  | "Alt" -> Some Alt
  | "Meta" -> Some Meta
  | _ -> None

let named_key_of_binding_name = function
  | "Escape" -> Some Escape
  | "Enter" -> Some Enter
  | "Backspace" -> Some Backspace
  | "Tab" -> Some Tab
  | "Delete" -> Some Delete
  | "ArrowUp" -> Some Arrow_up
  | "ArrowDown" -> Some Arrow_down
  | "ArrowLeft" -> Some Arrow_left
  | "ArrowRight" -> Some Arrow_right
  | "Home" -> Some Home
  | "End" -> Some End
  | _ -> None

let binding_text_alias = function
  | "Space" -> Some " "
  | "Minus" -> Some "-"
  | "Plus" -> Some "+"
  | "Comma" -> Some ","
  | "Period" -> Some "."
  | "Slash" -> Some "/"
  | _ -> None

let binding_error token message =
  Error
    (Error.Invalid_input_event
       (Printf.sprintf "invalid binding token %S: %s" token message))

let binding_event_of_string token =
  if String.length token = 0 then binding_error token "token must not be empty"
  else
    let parts = String.split_on_char '-' token in
    match List.rev parts with
    | [] -> binding_error token "token must not be empty"
    | key_name :: modifiers ->
        if String.length key_name = 0 then
          binding_error token "key name must not be empty"
        else
          let rec collect_modifiers result = function
            | [] -> Ok (normalize_modifiers result)
            | name :: rest -> (
                match modifier_of_binding_name name with
                | None -> binding_error token ("unknown modifier " ^ name)
                | Some modifier when List.mem modifier result ->
                    binding_error token ("duplicate modifier " ^ name)
                | Some modifier -> collect_modifiers (modifier :: result) rest)
          in
          Result.bind (collect_modifiers [] modifiers) (fun modifiers ->
              let key =
                match named_key_of_binding_name key_name with
                | Some named -> Ok (named_key named)
                | None -> (
                    match binding_text_alias key_name with
                    | Some text -> logical_text text
                    | None ->
                        let text =
                          if List.mem Shift modifiers then key_name
                          else String.lowercase_ascii key_name
                        in
                        logical_text text)
              in
              Result.map (fun key -> key_press ~modifiers key) key)

let binding_sequence_of_string value =
  let tokens = String.split_on_char ' ' value in
  if value = "" then
    Error (Error.Invalid_input_event "binding sequence must not be empty")
  else if List.exists (fun token -> String.length token = 0) tokens then
    Error
      (Error.Invalid_input_event
         "binding sequence tokens must be separated by one ASCII space")
  else if List.length tokens > 16 then
    Error
      (Error.Invalid_input_event
         "binding sequences may contain at most 16 input events")
  else
    let rec collect result = function
      | [] -> Ok (List.rev result)
      | token :: rest ->
          Result.bind (binding_event_of_string token) (fun event ->
              collect (event :: result) rest)
    in
    collect [] tokens

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
        Printf.sprintf "mouse(%s@%d,%d)"
          (mouse_action_to_string action)
          column row
      in
      if String.length prefix = 0 then event else prefix ^ "+" ^ event

let binding_sequence_to_string events =
  events |> List.map to_string |> String.concat " "
