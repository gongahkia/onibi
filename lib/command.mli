type handler =
  Editor_context.t ->
  Command_invocation.t ->
  (Model_intent.t list, Zenbu_kernel.Error.t) result

type effect_handler =
  Editor_context.t ->
  Command_invocation.t ->
  (Model_effect.t list, Zenbu_kernel.Error.t) result

type t

val create : descriptor:Command_descriptor.t -> handler:handler -> t

val create_effectful :
  descriptor:Command_descriptor.t -> effect_handler:effect_handler -> t

val descriptor : t -> Command_descriptor.t

val execute :
  t ->
  Editor_context.t ->
  Command_invocation.t ->
  (Model_intent.t list, Zenbu_kernel.Error.t) result

val execute_effects :
  t ->
  Editor_context.t ->
  Command_invocation.t ->
  (Model_effect.t list, Zenbu_kernel.Error.t) result
