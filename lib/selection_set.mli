type t

val create : primary:int -> Selection.t list -> (t, Error.t) result
val to_list : t -> Selection.t list
val primary : t -> Selection.t
val primary_index : t -> int
val document_id : t -> Document_id.t
val version : t -> Document_version.t
val map_anchors : t -> f:(Anchor.t -> Anchor.t) -> (t, Error.t) result
