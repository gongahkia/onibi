type kind = Builtin | Editing_model | Syntax | Application | Plugin
type t

val create : id:string -> kind:kind -> (t, Error.t) result
val id : t -> string
val kind : t -> kind
val kind_name : kind -> string
