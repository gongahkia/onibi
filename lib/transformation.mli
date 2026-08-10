type t =
  | Select
  | Delete
  | Replace_text of string
  | Collapse_to_start
  | Collapse_to_end

val name : t -> string
val descriptors : unit -> Semantic_descriptor.t list
