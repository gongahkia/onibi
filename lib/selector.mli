type t =
  | Current_selections
  | Document
  | Next_text_unit
  | Previous_text_unit
  | Next_word
  | Previous_word
  | Word_end
  | Current_word
  | Around_word
  | Current_line
  | Line_start
  | Line_end
  | First_nonblank
  | Document_start
  | Document_end
  | Next_line
  | Previous_line
  | All_occurrences

val resolve : Document_snapshot.t -> t -> (Selection_set.t, Error.t) result
val to_string : t -> string
val of_string : string -> (t, Error.t) result
val descriptors : unit -> Semantic_descriptor.t list
