(** Shared source-to-display projection for folds and bounded decorations. *)

type source_row
type virtual_row
type row = Source of source_row | Virtual of virtual_row

val project :
  contents:string ->
  document_id:string ->
  document_version:int ->
  folds:Fold.range list ->
  decorations:Decoration.response list ->
  Display.source_line list ->
  row list * Decoration.collection

val source_line : source_row -> Display.source_line
val fold : source_row -> Fold.range option
val inline : source_row -> Decoration.resolved list
val virtual_anchor : virtual_row -> Display.source_line
val virtual_decoration : virtual_row -> Decoration.resolved

val row_source_line : row -> Display.source_line option
val row_anchor_line : row -> Display.source_line
val last_source_line : row list -> Display.source_line option

val index_for_offset :
  row list -> Display.source_line list -> int -> int
