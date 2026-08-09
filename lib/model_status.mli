type t

val create :
  id:string ->
  label:string ->
  ?description:string ->
  ?pending_input:string ->
  ?metadata:(string * string) list ->
  unit ->
  (t, Error.t) result

val id : t -> string
val label : t -> string
val description : t -> string option
val pending_input : t -> string option
val metadata : t -> (string * string) list

