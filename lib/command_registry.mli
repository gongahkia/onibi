type t

val empty : t
val register : t -> Command.t -> (t, Zenbu_kernel.Error.t) result
val find : t -> Command_id.t -> (Command.t, Zenbu_kernel.Error.t) result
val descriptors : t -> Command_descriptor.t list
val invoke :
  t ->
  context:Editor_context.t ->
  Command_invocation.t ->
  (Model_intent.t list, Zenbu_kernel.Error.t) result
