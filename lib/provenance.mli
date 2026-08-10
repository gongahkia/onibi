(** Ordered semantic origin data attached to a transaction.  It deliberately
    excludes clocks and backend values so replay semantics stay deterministic. *)

type entry =
  | Model of { id : string; provider : Provider.t }
  | Input of string
  | Interaction of int
  | Effect of string
  | Command of { id : string; provider : Provider.t }
  | Selector of string
  | Transformation of string
  | Repeat of string

type t

val create : execution_id:int -> model_id:string -> provider:Provider.t -> input:string -> t
val execution_id : t -> int
val entries : t -> entry list
val add : t -> entry -> t
val entry_name : entry -> string
