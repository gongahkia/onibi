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

type t =
  | Key of { key : key; modifiers : modifier list }
  | Resize of { columns : int; rows : int }
  | Paste of string
  | End
  | Unsupported of string

let normalize_modifiers modifiers =
  let rank = function Shift -> 0 | Control -> 1 | Alt -> 2 | Meta -> 3 in
  List.sort_uniq
    (fun left right -> Int.compare (rank left) (rank right))
    modifiers
