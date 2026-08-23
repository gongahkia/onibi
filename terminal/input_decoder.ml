open Zenbu_model_api

let input_modifiers modifiers =
  List.map
    (function
      | Event.Shift -> Input_event.Shift
      | Event.Control -> Input_event.Control
      | Event.Alt -> Input_event.Alt
      | Event.Meta -> Input_event.Meta)
    modifiers

let mouse_button = function
  | Event.Primary -> Input_event.Primary
  | Event.Middle -> Input_event.Middle
  | Event.Secondary -> Input_event.Secondary
  | Event.Wheel_up -> Input_event.Wheel_up
  | Event.Wheel_down -> Input_event.Wheel_down

let mouse_action = function
  | Event.Press button -> Input_event.Press (mouse_button button)
  | Event.Drag -> Input_event.Drag
  | Event.Release -> Input_event.Release

let named_key = function
  | Event.Escape -> Some Input_event.Escape
  | Event.Enter -> Some Input_event.Enter
  | Event.Backspace -> Some Input_event.Backspace
  | Event.Tab -> Some Input_event.Tab
  | Event.Delete -> Some Input_event.Delete
  | Event.Arrow_up -> Some Input_event.Arrow_up
  | Event.Arrow_down -> Some Input_event.Arrow_down
  | Event.Arrow_left -> Some Input_event.Arrow_left
  | Event.Arrow_right -> Some Input_event.Arrow_right
  | Event.Home -> Some Input_event.Home
  | Event.End -> Some Input_event.End
  | Event.Text _ -> None

let decode ~input_mode = function
  | Event.Key { key = Event.Text text; modifiers = [] }
    when input_mode = Model_status.Text_entry ->
      Input_event.text_input text |> Result.map Option.some
  | Event.Key { key = Event.Text text; modifiers } ->
      Input_event.logical_text text
      |> Result.map (fun key ->
          Some
            (Input_event.key_press ~modifiers:(input_modifiers modifiers) key))
  | Event.Key { key; modifiers } -> (
      match named_key key with
      | None -> Ok None
      | Some key ->
          Ok
            (Some
               (Input_event.key_press
                  ~modifiers:(input_modifiers modifiers)
                  (Input_event.named_key key))))
  | Event.Mouse { action; column; row; modifiers } ->
      Input_event.mouse
        ~modifiers:(input_modifiers modifiers)
        (mouse_action action) ~column ~row
      |> Result.map Option.some
  | Event.Paste text when input_mode = Model_status.Text_entry ->
      Input_event.text_input text |> Result.map Option.some
  | Event.Paste _ -> Ok None
  | Event.Resize _ | Event.Wakeup | Event.End | Event.Unsupported _ -> Ok None
