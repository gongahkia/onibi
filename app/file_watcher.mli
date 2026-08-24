type event_kind =
  | Modified
  | Deleted
  | Replaced
  | Renamed
  | Overflow
  | Failure of string

type event = { path : string option; kind : event_kind }
type t
type fake

val create : ?interval_seconds:float -> unit -> t
val fake : unit -> t * fake
val watch : t -> path:string -> snapshot:File_io.snapshot -> unit
val unwatch : t -> path:string -> unit
val push : fake -> path:string option -> event_kind -> unit
val drain : t -> event list
val wakeup_fd : t -> Unix.file_descr
val close : t -> unit
val event_kind_name : event_kind -> string
