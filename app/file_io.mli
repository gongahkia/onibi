type error

val to_string : error -> string
val read : string -> (string, error) result

val save_atomic : path:string -> contents:string -> (unit, error) result
(** Write an adjacent, fsynced temporary file and atomically rename it over the
    target. The existing file mode is retained when the target already exists.
*)
