type style =
  | Plain
  | Primary_selection
  | Secondary_selection
  | Status
  | Message
  | Dim

type cell = { text : string; width : int; style : style }
type row = cell list

type cursor = { column : int; row : int }

type t = { width : int; height : int; rows : row list; cursor : cursor option }

let create ~width ~height ~rows ~cursor =
  { width = max 0 width; height = max 0 height; rows; cursor }

let cell ?(style = Plain) ~width text = { text; width = max 0 width; style }
let row_text row = String.concat "" (List.map (fun cell -> cell.text) row)
let width frame = frame.width
let height frame = frame.height
let rows frame = frame.rows
let cursor frame = frame.cursor
