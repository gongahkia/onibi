type t = { top_line : int; left_column : int; follow_cursor : bool }

val origin : t
val follow : t -> t
val scroll : t -> lines:int -> maximum_top_line:int -> t

val reconcile :
  ?scroll_margin:int ->
  t ->
  line:int ->
  column:int ->
  width:int ->
  height:int ->
  t
(** Keeps the cursor in the visible rows, reserving as much of the requested
    vertical margin as the current height can accommodate. *)
