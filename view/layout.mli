type orientation = Horizontal | Vertical
type dimension = Width | Height
type t
type rectangle = { x : int; y : int; width : int; height : int }

type error =
  | Unknown_pane of int
  | Cannot_close_last_pane
  | Cannot_resize_pane of int
  | Missing_frame of int
  | Frame_dimensions_mismatch of {
      pane : int;
      expected_width : int;
      expected_height : int;
      actual_width : int;
      actual_height : int;
    }

val single : int -> t
val panes : t -> int list
val split : t -> pane:int -> new_pane:int -> orientation -> (t, error) result
val close : t -> pane:int -> (t, error) result

val resize :
  t ->
  pane:int ->
  dimension:dimension ->
  delta:int ->
  width:int ->
  height:int ->
  (t, error) result

val balance : t -> t
val bounds : t -> width:int -> height:int -> (int * rectangle) list

val compose :
  t ->
  width:int ->
  height:int ->
  focused_pane:int ->
  frames:(int * Frame.t) list ->
  (Frame.t, error) result

val error_to_string : error -> string
