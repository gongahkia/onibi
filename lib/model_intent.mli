type selector =
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

type transformation =
  | Select
  | Delete
  | Replace_text of string
  | Collapse_to_start
  | Collapse_to_end

type t

val insert_text : string -> t
val delete_selected_ranges : t
val replace_selected_ranges : string -> t
val replace_selection_contents : string list -> t

val set_selections :
  selections:(int * int) list -> primary:int -> (t, Zenbu_kernel.Error.t) result

val apply : selector:selector -> transformation:transformation -> t
val selector_to_kernel : selector -> Zenbu_kernel.Selector.t
val transformation_to_kernel : transformation -> Zenbu_kernel.Transformation.t
val selector_of_string : string -> (selector, Zenbu_kernel.Error.t) result

val transformation_of_string :
  string -> (transformation, Zenbu_kernel.Error.t) result

val identity : t -> string
val is_textual : t -> bool
val semantic_components : t -> string option * string option
val to_kernel : t -> Zenbu_kernel.Intent.t
