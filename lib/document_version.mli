type t

val initial : t
val of_int : int -> (t, Error.t) result
val to_int : t -> int
val successor : t -> t
val compare : t -> t -> int
val equal : t -> t -> bool

