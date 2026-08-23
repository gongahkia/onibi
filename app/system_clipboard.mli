type t

val maximum_bytes : int

val create :
  name:string ->
  read:(unit -> (string, Zenbu_kernel.Error.t) result) ->
  write:(string -> (unit, Zenbu_kernel.Error.t) result) ->
  t
(** Constructs an injectable host clipboard provider. *)

val unavailable : string -> t
val default : unit -> t
val name : t -> string
val read : t -> (string, Zenbu_kernel.Error.t) result
val write : t -> string -> (unit, Zenbu_kernel.Error.t) result
