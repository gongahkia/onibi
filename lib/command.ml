open Zenbu_kernel

type handler =
  Editor_context.t ->
  Command_invocation.t ->
  (Model_intent.t list, Error.t) result

type effect_handler =
  Editor_context.t ->
  Command_invocation.t ->
  (Model_effect.t list, Error.t) result

type handler_kind =
  | Intents of handler
  | Effects of effect_handler
  | Extension_effects of {
      host : Extension_host.t;
      invocation : Extension_host.invocation;
      decode :
        Extension_host.request ->
        Extension_value.t ->
        (Model_effect.t list, Error.t) result;
    }
type t = { descriptor : Command_descriptor.t; handler : handler_kind }

let create ~descriptor ~handler = { descriptor; handler = Intents handler }

let create_effectful ~descriptor ~effect_handler =
  { descriptor; handler = Effects effect_handler }

let create_extension_effectful ~descriptor ~host ~invocation ~decode =
  { descriptor; handler = Extension_effects { host; invocation; decode } }

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
    | Effects _ | Extension_effects _ ->
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
        |> Result.map
             (List.map (fun intent -> Model_effect.Execute_intent intent))
    | Effects handler -> handler context invocation
    | Extension_effects { host; invocation = extension_invocation; decode } ->
        let request =
          Extension_host.request extension_invocation ~kind:Extension_host.Command
            ~operation:"command.invoke" ~context
            ~arguments:Extension_value.Nil
        in
        Result.bind (Extension_host.invoke host extension_invocation request)
          (decode request)
