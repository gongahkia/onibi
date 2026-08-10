type stage =
  | Model_handle
  | Selector_resolve
  | Transformation_apply
  | Transaction_commit
  | Syntax_update

type aggregate
type t

val disabled : unit -> t
val enabled : capacity:int -> (t, Zenbu_kernel.Error.t) result
val is_enabled : t -> bool
val capacity : t -> int option
val measure : t -> ?model_id:string -> stage -> (unit -> 'a) -> 'a
val reset : t -> unit
val aggregates : t -> aggregate list
val stage_name : stage -> string
val aggregate_stage : aggregate -> stage
val aggregate_model_id : aggregate -> string option
val aggregate_count : aggregate -> int
val aggregate_total_seconds : aggregate -> float
val aggregate_mean_seconds : aggregate -> float
val aggregate_max_seconds : aggregate -> float
