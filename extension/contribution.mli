type t = Commands | Selectors | Transformations | Bindings | Events

val all : t list
val id : t -> string
val of_id : string -> (t, Zenbu_kernel.Error.t) result
val description : t -> string
