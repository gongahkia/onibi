type t

val of_string : string -> (t, Zenbu_kernel.Error.t) result
val to_string : t -> string
val equal : t -> t -> bool
val compare : t -> t -> int
val owns : t -> string -> bool
