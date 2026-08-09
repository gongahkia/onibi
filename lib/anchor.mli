type t

val make :
  document_id:Document_id.t ->
  version:Document_version.t ->
  byte_offset:int ->
  (t, Error.t) result

val document_id : t -> Document_id.t
val version : t -> Document_version.t
val byte_offset : t -> int
val rebase : t -> version:Document_version.t -> byte_offset:int -> t
val equal : t -> t -> bool
val compare : t -> t -> int
