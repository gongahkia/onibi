type kind = Builtin | Editing_model | Syntax | Application | Script | Plugin
type t

val create : id:string -> kind:kind -> (t, Error.t) result

val create_with_source :
  id:string -> kind:kind -> source:string -> (t, Error.t) result

val create_with_metadata :
  id:string ->
  kind:kind ->
  ?source:string ->
  ?plugin_id:string ->
  ?version:string ->
  ?runtime:string ->
  unit ->
  (t, Error.t) result

val id : t -> string
val kind : t -> kind
val source : t -> string option
val plugin_id : t -> string option
val version : t -> string option
val runtime : t -> string option
val describe : t -> string
val kind_name : kind -> string
