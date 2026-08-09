type t = Select | Delete | Replace_text of string

val name : t -> string
