type kind = Builtin | Editing_model | Syntax | Application | Script | Plugin
type t

val create : id:string -> kind:kind -> ?source:string -> (t, Error.t) result
val id : t -> string
val kind : t -> kind
val source : t -> string option
val kind_name : kind -> string
