type t = { top_line : int; left_column : int; follow_cursor : bool }

val origin : t
val follow : t -> t
val scroll : t -> lines:int -> maximum_top_line:int -> t
val reconcile : t -> line:int -> column:int -> width:int -> height:int -> t
