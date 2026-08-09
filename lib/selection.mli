type t

val make : anchor:Anchor.t -> head:Anchor.t -> (t, Error.t) result
val anchor : t -> Anchor.t
val head : t -> Anchor.t
val range : t -> Range.t
val document_id : t -> Document_id.t
val version : t -> Document_version.t
val equal : t -> t -> bool
val compare : t -> t -> int
val map_anchors : t -> f:(Anchor.t -> Anchor.t) -> (t, Error.t) result
