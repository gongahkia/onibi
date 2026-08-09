type t = { top_line : int; left_column : int }

val origin : t
val reconcile : t -> line:int -> column:int -> width:int -> height:int -> t
