(** A serialisable, implementation-neutral value exchanged with extensions.
    It deliberately excludes editor and runtime objects. *)

type t =
  | Nil
  | Bool of bool
  | Integer of int
  | Float of float
  | Text of string
  | List of t list
  | Record of (string * t) list

val record : (string * t) list -> (t, Zenbu_kernel.Error.t) result
val find : t -> string -> t option
val to_string : t -> string
