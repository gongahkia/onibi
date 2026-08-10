(** A model-owned, state-specific description of accepted logical input. *)

type pattern =
  | Exact of string
  | Named of string
  | Text_input
  | Text_range of string

type kind = Binding | Prefix | Catch_all
type t

val create :
  id:string ->
  pattern:pattern ->
  kind:kind ->
  summary:string ->
  ?next_status:string ->
  ?command_id:string ->
  ?selector_id:string ->
  ?transformation_id:string ->
  ?requires_syntax:bool ->
  unit ->
  (t, Zenbu_kernel.Error.t) result

val id : t -> string
val pattern : t -> pattern
val kind : t -> kind
val summary : t -> string
val next_status : t -> string option
val command_id : t -> string option
val selector_id : t -> string option
val transformation_id : t -> string option
val requires_syntax : t -> bool
val pattern_to_string : pattern -> string
