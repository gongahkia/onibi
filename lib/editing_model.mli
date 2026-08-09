type descriptor

val descriptor :
  id:string ->
  title:string ->
  ?description:string ->
  unit ->
  (descriptor, Zenbu_kernel.Error.t) result

val id : descriptor -> string
val title : descriptor -> string
val description : descriptor -> string option

module type S = sig
  type state

  val descriptor : descriptor
  val initialize : Editor_context.t -> state
  val handle_input : state -> Input_event.t -> Editor_context.t -> state * Model_effect.t list
  val reset : state -> Editor_context.t -> state
  val status : state -> Model_status.t
end
