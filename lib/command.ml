open Zenbu_kernel

type handler =
  Editor_context.t ->
  Command_invocation.t ->
  (Model_intent.t list, Error.t) result

type effect_handler =
  Editor_context.t -> Command_invocation.t -> (Model_effect.t list, Error.t) result

type handler_kind = Intents of handler | Effects of effect_handler

type t = { descriptor : Command_descriptor.t; handler : handler_kind }

let create ~descriptor ~handler = { descriptor; handler = Intents handler }

let create_effectful ~descriptor ~effect_handler =
  { descriptor; handler = Effects effect_handler }

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
  else
    match value.handler with
    | Intents handler -> handler context invocation
    | Effects _ ->
        Error
          (Error.Invalid_command_arguments
             "effectful command must be invoked through the editing runtime")

let execute_effects value context invocation =
  if
    not
      (Command_id.equal
         (Command_descriptor.id value.descriptor)
         (Command_invocation.id invocation))
  then
    Error
      (Error.Invalid_command_arguments
         "invocation id does not match the command descriptor")
  else
    match value.handler with
    | Intents handler ->
        handler context invocation
        |> Result.map (List.map (fun intent -> Model_effect.Execute_intent intent))
    | Effects handler -> handler context invocation
