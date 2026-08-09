open Zenbu_kernel

type handler =
  Editor_context.t ->
  Command_invocation.t ->
  (Model_intent.t list, Error.t) result

type t = { descriptor : Command_descriptor.t; handler : handler }

let create ~descriptor ~handler = { descriptor; handler }
let descriptor value = value.descriptor

let execute value context invocation =
  if
    not
      (Command_id.equal
         (Command_descriptor.id value.descriptor)
         (Command_invocation.id invocation))
  then
    Error
      (Error.Invalid_command_arguments
         "invocation id does not match the command descriptor")
  else value.handler context invocation
