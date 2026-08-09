type change
type t

val create : Document.t -> t
val current : t -> Document.t
val commit : t -> Transaction.t -> (t, Error.t) result

val apply_intent :
  source:Transaction.source ->
  ?description:string ->
  t ->
  Intent.t ->
  (t, Error.t) result

val undo : t -> (t, Error.t) result
val redo : ?change_id:int -> t -> (t, Error.t) result
val current_change : t -> change option
val lineage : t -> change list
val change_id : change -> int
val transaction : change -> Transaction.t
val before : change -> Document.t
val after : change -> Document.t
