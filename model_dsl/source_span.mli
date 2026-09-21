(** UTF-8 byte offsets in a [.zenmodel] source file. *)

type t

val make : start_offset:int -> stop_offset:int -> t
val start_offset : t -> int
val stop_offset : t -> int
val join : t -> t -> t
val whole : string -> t
