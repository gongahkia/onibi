open Zenbu_kernel

type message_level = Info | Warning | Error
type message = { level : message_level; text : string }
type search_direction = Forward | Backward

type macro_request =
  | Reserve_macro_input
  | Toggle_macro_recording of string
  | Replay_macro of { register : string; count : int }

type selection_action =
  | Transform of Model_intent.transformation
  | Copy of { slot : Clipboard.slot; kind : Clipboard.kind }

type t =
  | Execute_intent of Model_intent.t
  | Execute_intent_with of {
      intent : Model_intent.t;
      selector_id : string option;
      transformation_id : string option;
    }
  | Execute_semantic_operation of Semantic_operation.t
  | Apply_to_selections of {
      selections : (int * int) list;
      primary : int;
      selector_id : string;
      action : selection_action;
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
  | Request_search of search_direction
  | Repeat_search of search_direction
  | Request_macro of macro_request
  | Undo
  | Redo
  | Repeat_last_edit

let message ~level ~text =
  if String.length text = 0 then
    Result.Error (Error.Invalid_model_status "messages must not be empty")
  else Ok (Emit_message { level; text })

let execute ?selector_id ?transformation_id intent =
  Execute_intent_with { intent; selector_id; transformation_id }

let execute_semantic_operation operation = Execute_semantic_operation operation

let selector_id = function
  | Execute_intent intent -> fst (Model_intent.semantic_components intent)
  | Execute_intent_with { intent; selector_id; _ } -> (
      match selector_id with
      | Some _ -> selector_id
      | None -> fst (Model_intent.semantic_components intent))
  | Execute_semantic_operation operation ->
      Some (Semantic_operation.selector_id operation.selector)
  | Apply_to_selections { selector_id; _ } -> Some selector_id
  | Invoke_command _ | Emit_message _ | Copy_to_clipboard _
  | Paste_from_clipboard _ | Request_search _ | Repeat_search _
  | Request_macro _ | Undo | Redo | Repeat_last_edit ->
      None

let transformation_id = function
  | Execute_intent intent -> snd (Model_intent.semantic_components intent)
  | Execute_intent_with { intent; transformation_id; _ } -> (
      match transformation_id with
      | Some _ -> transformation_id
      | None -> snd (Model_intent.semantic_components intent))
  | Execute_semantic_operation operation ->
      Some (Semantic_operation.transformation_id operation.transformation)
  | Apply_to_selections { action = Transform transformation; _ } ->
      Some
        (Zenbu_kernel.Transformation.name
           (Model_intent.transformation_to_kernel transformation))
  | Apply_to_selections { action = Copy _; _ } -> None
  | Invoke_command _ | Emit_message _ | Copy_to_clipboard _
  | Paste_from_clipboard _ | Request_search _ | Repeat_search _
  | Request_macro _ | Undo | Redo | Repeat_last_edit ->
      None

let identity = function
  | Execute_intent intent | Execute_intent_with { intent; _ } ->
      "execute " ^ Model_intent.identity intent
  | Execute_semantic_operation operation ->
      "execute " ^ Semantic_operation.identity operation
  | Apply_to_selections { selector_id; action; _ } -> (
      "apply-explicit-selections:" ^ selector_id
      ^
      match action with
      | Transform transformation ->
          ":"
          ^ Zenbu_kernel.Transformation.name
              (Model_intent.transformation_to_kernel transformation)
      | Copy _ -> ":copy")
  | Invoke_command invocation ->
      "invoke " ^ Command_id.to_string (Command_invocation.id invocation)
  | Emit_message _ -> "emit-message"
  | Copy_to_clipboard _ -> "copy-to-clipboard"
  | Paste_from_clipboard _ -> "paste-from-clipboard"
  | Request_search Forward -> "request-search:forward"
  | Request_search Backward -> "request-search:backward"
  | Repeat_search Forward -> "repeat-search:forward"
  | Repeat_search Backward -> "repeat-search:backward"
  | Request_macro Reserve_macro_input -> "request-macro:reserve-input"
  | Request_macro (Toggle_macro_recording register) ->
      "request-macro:toggle-recording:" ^ register
  | Request_macro (Replay_macro { register; count }) ->
      "request-macro:replay:" ^ register ^ ":" ^ string_of_int count
  | Undo -> "undo"
  | Redo -> "redo"
  | Repeat_last_edit -> "repeat-last-edit"

let describe = function
  | Execute_intent intent | Execute_intent_with { intent; _ } ->
      "execute " ^ Model_intent.identity intent
  | Execute_semantic_operation operation ->
      "execute " ^ Semantic_operation.identity operation
  | Apply_to_selections { selector_id; action; _ } ->
      let action =
        match action with
        | Transform transformation ->
            Zenbu_kernel.Transformation.name
              (Model_intent.transformation_to_kernel transformation)
        | Copy { slot; kind } ->
            "copy " ^ Clipboard.kind_name kind ^ " to "
            ^ Clipboard.slot_name slot
      in
      "apply " ^ selector_id ^ " with " ^ action
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
  | Request_search Forward -> "request a forward literal search"
  | Request_search Backward -> "request a backward literal search"
  | Repeat_search Forward -> "request the next literal search match"
  | Repeat_search Backward -> "request the previous literal search match"
  | Request_macro Reserve_macro_input ->
      "reserve the current input for macro control"
  | Request_macro (Toggle_macro_recording register) ->
      "start or stop recording macro register " ^ register
  | Request_macro (Replay_macro { register; count }) ->
      Printf.sprintf "replay macro register %s %d time(s)" register count
  | Undo -> "undo"
  | Redo -> "redo"
  | Repeat_last_edit -> "repeat-last-edit"
