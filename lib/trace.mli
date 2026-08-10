type t

val disabled : unit -> t
val enabled : capacity:int -> (t, Zenbu_kernel.Error.t) result
val is_enabled : t -> bool
val capacity : t -> int option
val emit_lazy : t -> (unit -> Trace_event.t) -> unit
val events : t -> Trace_event.t list
val clear : t -> unit
