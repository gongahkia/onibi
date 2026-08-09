type t

val create :
  id:Command_id.t ->
  arguments:Command_argument.t list ->
  (t, Zenbu_kernel.Error.t) result

val id : t -> Command_id.t
val arguments : t -> Command_argument.t list
val find :
  t -> name:string -> (Command_argument.value, Zenbu_kernel.Error.t) result
