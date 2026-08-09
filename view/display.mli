(** Document offsets remain UTF-8 byte offsets.  This module maps valid source
    text to grapheme-aware terminal columns without changing the source. *)

type grapheme = {
  start_offset : int;
  stop_offset : int;
  text : string;
  column : int;
  width : int;
}

type line = {
  number : int;
  start_offset : int;
  stop_offset : int;
  end_offset : int;
  graphemes : grapheme list;
  width : int;
}

type source_line = {
  number : int;
  start_offset : int;
  stop_offset : int;
  end_offset : int;
}

val tab_width : int
val source_lines : string -> source_line list
val layout : string -> source_line -> line
val lines : string -> line list
val source_line_at : source_line list -> int -> source_line
val column_at : line -> int -> int
val locate : line list -> int -> line * int
val visible_graphemes : line -> left_column:int -> width:int -> grapheme list
val text_width : string -> int
