open Zenbu_kernel

type message_level = Info | Warning | Error
type message = { level : message_level; text : string }

type t =
  | Execute_intent of Model_intent.t
  | Invoke_command of Command_invocation.t
  | Emit_message of message

let message ~level ~text =
  if String.length text = 0 then
    Result.Error (Error.Invalid_model_status "messages must not be empty")
  else Ok (Emit_message { level; text })

let describe = function
  | Execute_intent intent -> "execute " ^ Model_intent.identity intent
  | Invoke_command invocation ->
      "invoke " ^ Command_id.to_string (Command_invocation.id invocation)
  | Emit_message { level; text } ->
      let level =
        match level with
        | Info -> "info"
        | Warning -> "warning"
        | Error -> "error"
      in
      level ^ ": " ^ text
