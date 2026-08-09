type t

val make : start:Anchor.t -> stop:Anchor.t -> (t, Error.t) result
val start : t -> Anchor.t
val stop : t -> Anchor.t
val document_id : t -> Document_id.t
val version : t -> Document_version.t
val is_empty : t -> bool
val compare : t -> t -> int

