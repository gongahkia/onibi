type t

val of_utf8 : string -> (t, Error.t) result
val empty : t
val contents : t -> string
val byte_length : t -> int
val is_code_point_boundary : t -> int -> bool
val replace : t -> start:int -> stop:int -> with_:string -> (t, Error.t) result

