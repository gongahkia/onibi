(** Bounded trusted-local streaming programs for the Session host.

    A job receives no editor handle, shell parser, stdin, inherited environment,
    caller-selected working directory, or process handle. The host runs an
    absolute executable plus argument vector in a new process session at [/]
    with a fixed locale/path environment. Stdout and stderr are continuously
    drained, retained as bounded UTF-8 snapshots, and wake the terminal while
    running. Cancellation and [close] terminate the isolated process group. *)

type t
type completion

val create : unit -> t

val start :
  t ->
  Zenbu_model_api.Model_effect.background_process_request ->
  (int, Zenbu_kernel.Error.t) result

val cancel : t -> id:int -> (unit, Zenbu_kernel.Error.t) result
val wakeup_fd : t -> Unix.file_descr
val drain : t -> completion list
val completion_message : completion -> string
val output : t -> id:int -> (string, Zenbu_kernel.Error.t) result
val lines : t -> string list
val close : t -> unit
