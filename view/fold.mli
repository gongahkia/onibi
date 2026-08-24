(** Immutable, view-owned source-line folds. A fold keeps its header line
    visible and hides the following complete source lines. *)

type source = Manual | Syntax
type range
type projected_line

val source : range -> source
val start_offset : range -> int
val stop_offset : range -> int
val start_line : range -> int
val stop_line : range -> int
val source_line : projected_line -> Display.source_line
val fold : projected_line -> range option
val hidden_line_count : projected_line -> int

val create :
  source:source ->
  Display.source_line list ->
  start_offset:int ->
  stop_offset:int ->
  (range, string) result
(** Normalizes a byte range to complete source lines. The resulting range must
    cover a visible header and at least one following source line. *)

val validate : range list -> (unit, string) result
(** Rejects duplicate and partially overlapping folds. Strictly nested folds are
    valid; the outer fold hides nested headers while it is collapsed. *)

val project : range list -> Display.source_line list -> projected_line list

val index_for_offset :
  projected_line list -> Display.source_line list -> int -> int
(** Maps a source byte to its visible projected row. An offset in a hidden line
    maps to the enclosing fold header. *)
