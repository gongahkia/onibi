type descriptor

val descriptor :
  id:string ->
  title:string ->
  ?description:string ->
  ?provider:Zenbu_kernel.Provider.t ->
  unit ->
  (descriptor, Zenbu_kernel.Error.t) result

val id : descriptor -> string
val title : descriptor -> string
val description : descriptor -> string option
val provider : descriptor -> Zenbu_kernel.Provider.t

module type S = sig
  type state

  val descriptor : descriptor

  val descriptor_of_state : state -> descriptor
  (** A host uses the initialized state descriptor for provenance and binding
      scope. Built-in models normally return [descriptor]. *)

  val initialize : Editor_context.t -> state

  val handle_input :
    state -> Input_event.t -> Editor_context.t -> state * Model_effect.t list

  val reset : state -> Editor_context.t -> state
  val status : state -> Model_status.t
  val input_rules : state -> Input_rule.t list
end
