open Zenbu_kernel

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
  | Copy_to_clipboard { slot; selector = _; kind } ->
      "copy " ^ Clipboard.kind_name kind ^ " to " ^ Clipboard.slot_name slot
  | Paste_from_clipboard { slot; placement } ->
      "paste " ^ Clipboard.slot_name slot ^ " "
      ^ Clipboard.placement_name placement
  | Undo -> "undo"
  | Redo -> "redo"
  | Repeat_last_edit -> "repeat-last-edit"
