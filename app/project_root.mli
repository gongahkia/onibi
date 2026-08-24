type t
type entry = { relative_path : string }

val select : string -> (t, string) result
val path : t -> string
val contains : t -> path:string -> bool
val discover : t -> (entry list, string) result
val filter : t -> query:string -> (entry list, string) result
val resolve : t -> relative_path:string -> (string, string) result
