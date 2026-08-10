open Zenbu_kernel

type message_level = Info | Warning | Error
type message = { level : message_level; text : string }

type t =
  | Execute_intent of Model_intent.t
  | Execute_intent_with of {
      intent : Model_intent.t;
      selector_id : string option;
      transformation_id : string option;
    }
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

let execute ?selector_id ?transformation_id intent =
  Execute_intent_with { intent; selector_id; transformation_id }

let selector_id = function
  | Execute_intent intent -> fst (Model_intent.semantic_components intent)
  | Execute_intent_with { intent; selector_id; _ } -> (
      match selector_id with
      | Some _ -> selector_id
      | None -> fst (Model_intent.semantic_components intent))
  | Invoke_command _ | Emit_message _ | Copy_to_clipboard _
  | Paste_from_clipboard _ | Undo | Redo | Repeat_last_edit ->
      None

let transformation_id = function
  | Execute_intent intent -> snd (Model_intent.semantic_components intent)
  | Execute_intent_with { intent; transformation_id; _ } -> (
      match transformation_id with
      | Some _ -> transformation_id
      | None -> snd (Model_intent.semantic_components intent))
  | Invoke_command _ | Emit_message _ | Copy_to_clipboard _
  | Paste_from_clipboard _ | Undo | Redo | Repeat_last_edit ->
      None

let identity = function
  | Execute_intent intent | Execute_intent_with { intent; _ } ->
      "execute " ^ Model_intent.identity intent
  | Invoke_command invocation ->
      "invoke " ^ Command_id.to_string (Command_invocation.id invocation)
  | Emit_message _ -> "emit-message"
  | Copy_to_clipboard _ -> "copy-to-clipboard"
  | Paste_from_clipboard _ -> "paste-from-clipboard"
  | Undo -> "undo"
  | Redo -> "redo"
  | Repeat_last_edit -> "repeat-last-edit"

let describe = function
  | Execute_intent intent | Execute_intent_with { intent; _ } ->
      "execute " ^ Model_intent.identity intent
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
