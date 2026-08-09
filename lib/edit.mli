type t

val insert : at:Anchor.t -> text:string -> (t, Error.t) result
val delete : Range.t -> t
val replace : Range.t -> text:string -> (t, Error.t) result
val range : t -> Range.t
val text : t -> string
val is_insertion : t -> bool

