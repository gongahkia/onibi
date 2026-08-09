type message_level = Info | Warning | Error
type message = { level : message_level; text : string }

type t =
  | Execute_intent of Model_intent.t
  | Invoke_command of Command_invocation.t
  | Emit_message of message
  | Copy_to_clipboard of {
      slot : Clipboard.slot;
      selector : Model_intent.selector;
      kind : Clipboard.kind;
    }
  | Paste_from_clipboard of {
      slot : Clipboard.slot;
      placement : Clipboard.placement;
    }
  | Undo
  | Redo
  | Repeat_last_edit

val message :
  level:message_level -> text:string -> (t, Zenbu_kernel.Error.t) result

val describe : t -> string
