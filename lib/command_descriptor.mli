type parameter = { name : string; description : string; required : bool }
type t

val create :
  id:Command_id.t ->
  title:string ->
  ?description:string ->
  ?category:string ->
  ?parameters:parameter list ->
  ?examples:string list ->
  unit ->
  (t, Error.t) result

val id : t -> Command_id.t
val title : t -> string
val description : t -> string option
val category : t -> string option
val parameters : t -> parameter list
val examples : t -> string list

