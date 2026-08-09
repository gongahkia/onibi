type handler = Editor_context.t -> Command_invocation.t -> (Model_intent.t list, Error.t) result
type t

val create : descriptor:Command_descriptor.t -> handler:handler -> t
val descriptor : t -> Command_descriptor.t
val execute : t -> Editor_context.t -> Command_invocation.t -> (Model_intent.t list, Error.t) result

