type error
type snapshot

val to_string : error -> string
val read : string -> (string, error) result
val snapshot : path:string -> contents:string -> (snapshot, error) result
val read_snapshot : string -> (string * snapshot, error) result
val check_snapshot : snapshot -> path:string -> (unit, error) result

val save_atomic : path:string -> contents:string -> (unit, error) result
(** Write an adjacent, fsynced temporary file and atomically rename it over the
    target. The existing file mode is retained when the target already exists.
*)

val save_atomic_snapshot :
  path:string -> contents:string -> (snapshot, error) result
(** Atomically save and return the baseline for the resulting target. *)
