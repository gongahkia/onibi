type kind = Characterwise | Linewise
type placement = Before | After | Replace
type slot
type entry
type t

val unnamed : slot
val maximum_kill_ring_entries : int
val slot : string -> (slot, Zenbu_kernel.Error.t) result
val slot_name : slot -> string
val entry : kind:kind -> contents:string -> (entry, Zenbu_kernel.Error.t) result
val contents : entry -> string
val kind : entry -> kind
val empty : t
val find : t -> slot:slot -> entry option
val store : t -> slot:slot -> entry:entry -> t

val store_kill : t -> slot:slot -> entry:entry -> t
(** Stores the entry in its ordinary slot and prepends it to the bounded kill
    history. *)

val find_kill : t -> index:int -> entry option
val kill_ring_length : t -> int
val kill_ring_entries : t -> entry list

(* Replaces only the bounded kill history, retaining ordinary slots. *)
val with_kill_ring : t -> entry list -> t
val kind_name : kind -> string
val placement_name : placement -> string
