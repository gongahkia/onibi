type t

val make : anchor_offset:int -> head_offset:int -> (t, Error.t) result
val anchor_offset : t -> int
val head_offset : t -> int
val equal : t -> t -> bool

