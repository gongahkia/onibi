type t

val make :
  document_id:Document_id.t ->
  version:Document_version.t ->
  buffer:Text_buffer.t ->
  selections:Selection_set.t ->
  (t, Error.t) result

val document_id : t -> Document_id.t
val version : t -> Document_version.t
val contents : t -> string
val byte_length : t -> int
val selections : t -> Selection_set.t
val anchor : t -> byte_offset:int -> (Anchor.t, Error.t) result

val range :
  t -> start_offset:int -> stop_offset:int -> (Range.t, Error.t) result

val validate_anchor : t -> Anchor.t -> (unit, Error.t) result
val validate_range : t -> Range.t -> (unit, Error.t) result
val validate_selection_set : t -> Selection_set.t -> (unit, Error.t) result
