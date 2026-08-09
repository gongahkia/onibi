type t

val create :
  id:Document_id.t ->
  contents:string ->
  ?initial_selections:Selection_spec.t list ->
  ?primary:int ->
  unit ->
  (t, Error.t) result

val id : t -> Document_id.t
val version : t -> Document_version.t
val snapshot : t -> Document_snapshot.t

val apply :
  ?result_version:Document_version.t -> t -> Transaction.t -> (t, Error.t) result

