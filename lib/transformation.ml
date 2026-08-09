type t = Select | Delete | Replace_text of string

let name = function
  | Select -> "select"
  | Delete -> "delete"
  | Replace_text _ -> "replace-text"
