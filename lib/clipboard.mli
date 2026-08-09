type kind = Characterwise | Linewise
type placement = Before | After | Replace
type slot
type entry
type t

val unnamed : slot
val slot : string -> (slot, Zenbu_kernel.Error.t) result
val slot_name : slot -> string
val entry : kind:kind -> contents:string -> (entry, Zenbu_kernel.Error.t) result
val contents : entry -> string
val kind : entry -> kind
val empty : t
val find : t -> slot:slot -> entry option
val store : t -> slot:slot -> entry:entry -> t
val kind_name : kind -> string
val placement_name : placement -> string
