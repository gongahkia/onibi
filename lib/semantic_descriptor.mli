type kind = Selector | Transformation
type t

val create :
  id:string ->
  title:string ->
  description:string ->
  provider:Provider.t ->
  kind:kind ->
  ?requires_syntax:bool ->
  unit ->
  (t, Error.t) result

val id : t -> string
val title : t -> string
val description : t -> string
val provider : t -> Provider.t
val kind : t -> kind
val requires_syntax : t -> bool
