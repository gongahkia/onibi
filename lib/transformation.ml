type t =
  | Select
  | Delete
  | Replace_text of string
  | Collapse_to_start
  | Collapse_to_end

let name = function
  | Select -> "select"
  | Delete -> "delete"
  | Replace_text _ -> "replace-text"
  | Collapse_to_start -> "collapse-to-start"
  | Collapse_to_end -> "collapse-to-end"
