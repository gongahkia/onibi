type modifier = Shift | Control | Alt | Meta

type key =
  | Text of string
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

type mouse_button = Primary | Middle | Secondary | Wheel_up | Wheel_down
type mouse_action = Press of mouse_button | Drag | Release

type t =
  | Key of { key : key; modifiers : modifier list }
  | Mouse of {
      action : mouse_action;
      column : int;
      row : int;
      modifiers : modifier list;
    }
  | Resize of { columns : int; rows : int }
  | Paste of string
  | Wakeup
  | End
  | Unsupported of string

val normalize_modifiers : modifier list -> modifier list
