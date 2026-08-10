type style =
  | Plain
  | Primary_selection
  | Secondary_selection
  | Status
  | Message
  | Dim
  | Search_match
  | Syntax_keyword
  | Syntax_string
  | Syntax_number
  | Syntax_comment
  | Syntax_type
  | Syntax_constructor
  | Overlay

type cell = { text : string; width : int; style : style }
type row = cell list
type cursor = { column : int; row : int }
type t

val create :
  width:int -> height:int -> rows:row list -> cursor:cursor option -> t

val cell : ?style:style -> width:int -> string -> cell
val row_text : row -> string
val width : t -> int
val height : t -> int
val rows : t -> row list
val cursor : t -> cursor option
