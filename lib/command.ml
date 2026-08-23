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

let extension_argument_value argument =
  match Command_argument.value argument with
  | Command_argument.Text text -> Extension_value.Text text
  | Command_argument.Selector selector ->
      Extension_value.Text
        (Selector.to_string (Model_intent.selector_to_kernel selector))
  | Command_argument.Transformation transformation ->
      let transformation =
        Model_intent.transformation_to_kernel transformation
      in
      Extension_value.Record
        ([ ("kind", Extension_value.Text (Transformation.name transformation)) ]
        @
        match transformation with
        | Transformation.Replace_text text ->
            [ ("text", Extension_value.Text text) ]
        | Transformation.Select | Transformation.Delete
        | Transformation.Collapse_to_start | Transformation.Collapse_to_end ->
            [])

let extension_arguments invocation =
  Extension_value.Record
    (Command_invocation.arguments invocation
    |> List.map (fun argument ->
        (Command_argument.name argument, extension_argument_value argument)))

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
          Extension_host.request extension_invocation
            ~kind:Extension_host.Command ~operation:"command.invoke" ~context
            ~arguments:(extension_arguments invocation)
        in
        Result.bind
          (Extension_host.invoke host extension_invocation request)
          (decode request)
