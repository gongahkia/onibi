type t = Current_selections | Document | Next_text_unit | Previous_text_unit

val resolve : Document_snapshot.t -> t -> (Selection_set.t, Error.t) result
val to_string : t -> string
val of_string : string -> (t, Error.t) result
