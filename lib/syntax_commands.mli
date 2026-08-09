(** Generic syntax-selection commands.  These commands operate only on the
    abstract syntax snapshot carried by an [Editor_context]. *)

type operation =
  | Focus_primary
  | Parent
  | First_child
  | Next_sibling
  | Previous_sibling
  | Expand
  | Same_kind_siblings

val resolve :
  Editor_context.t -> operation -> (Model_intent.t list, Zenbu_kernel.Error.t) result

val commands : unit -> Command.t list
val invocation : operation -> (Command_invocation.t, Zenbu_kernel.Error.t) result
