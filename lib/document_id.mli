type t

val of_string : string -> (t, Error.t) result
val to_string : t -> string
val equal : t -> t -> bool
val compare : t -> t -> int

