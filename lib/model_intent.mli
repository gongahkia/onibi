type selector =
  | Current_selections
  | Document
  | Next_text_unit
  | Previous_text_unit

type transformation = Select | Delete | Replace_text of string
type t

val insert_text : string -> t
val delete_selected_ranges : t
val replace_selected_ranges : string -> t

val set_selections :
  selections:(int * int) list -> primary:int -> (t, Zenbu_kernel.Error.t) result

val apply : selector:selector -> transformation:transformation -> t
val identity : t -> string
val to_kernel : t -> Zenbu_kernel.Intent.t
