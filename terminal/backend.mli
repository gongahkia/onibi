(** The only module that knows the concrete terminal backend. *)

type t

val create : unit -> (t, string) result
val release : t -> unit
val with_terminal : (t -> 'a) -> ('a, string) result
val size : t -> int * int
val set_theme : t -> Zenbu_view.Theme.t -> unit

val read :
  ?wakeup:Unix.file_descr -> ?wakeups:Unix.file_descr list -> t -> Event.t

val draw : t -> Zenbu_view.Frame.t -> unit
