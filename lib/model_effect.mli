type message_level = Info | Warning | Error
type message = { level : message_level; text : string }
type t = Execute_intent of Model_intent.t | Invoke_command of Command_invocation.t | Emit_message of message

val message : level:message_level -> text:string -> (t, Error.t) result
val describe : t -> string

