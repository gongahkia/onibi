type t

val of_string : string -> (t, Zenbu_kernel.Error.t) result
val to_string : t -> string
val compare : t -> t -> int
