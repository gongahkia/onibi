open Zenbu_kernel

type message_level = Info | Warning | Error
type message = { level : message_level; text : string }
type search_direction = Forward | Backward

type macro_request =
  | Reserve_macro_input
  | Toggle_macro_recording of string
  | Replay_macro of { register : string; count : int }

type location_request = Set_location of string | Jump_location of string
type jump_direction = Backward | Forward

type jump_request =
  | Push_current_jump
  | Traverse_jump of { direction : jump_direction; count : int }

type workspace_request =
  | Split_view_vertical
  | Split_view_horizontal
  | Focus_next_view
  | Close_view
  | Keep_only_view
  | Resize_view_width of int
  | Resize_view_height of int
  | Balance_views
  | New_buffer
  | Open_buffer
  | Close_buffer
  | Next_buffer
  | Previous_buffer

type viewport_request =
  | Scroll_view_lines of int
  | Scroll_view_pages of int
  | Center_view

type process_request = { program : string; arguments : string list }
type external_filter_request = process_request
type background_process_request = process_request

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
  | Cut_to_clipboard of {
      slot : Clipboard.slot;
      selector : Model_intent.selector;
      kind : Clipboard.kind;
    }
  | Paste_from_clipboard of {
      slot : Clipboard.slot;
      placement : Clipboard.placement;
    }
  | Paste_from_kill_ring of { index : int; placement : Clipboard.placement }
  | Request_search of search_direction
  | Repeat_search of search_direction
  | Request_macro of macro_request
  | Request_location of location_request
  | Request_jump of jump_request
  | Request_workspace of workspace_request
  | Request_viewport of viewport_request
  | Request_external_filter of external_filter_request
  | Request_background_process of background_process_request
  | Request_save
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
  | Invoke_command _ | Emit_message _ | Copy_to_clipboard _ | Cut_to_clipboard _
  | Paste_from_clipboard _ | Paste_from_kill_ring _ | Request_search _
  | Repeat_search _ | Request_macro _ | Request_location _ | Request_jump _
  | Request_workspace _ | Request_viewport _ | Request_external_filter _
  | Request_background_process _ | Request_save | Undo | Redo | Repeat_last_edit
    ->
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
  | Invoke_command _ | Emit_message _ | Copy_to_clipboard _ | Cut_to_clipboard _
  | Paste_from_clipboard _ | Paste_from_kill_ring _ | Request_search _
  | Repeat_search _ | Request_macro _ | Request_location _ | Request_jump _
  | Request_workspace _ | Request_viewport _ | Request_external_filter _
  | Request_background_process _ | Request_save | Undo | Redo | Repeat_last_edit
    ->
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
  | Cut_to_clipboard _ -> "cut-to-clipboard"
  | Paste_from_clipboard _ -> "paste-from-clipboard"
  | Paste_from_kill_ring { index; _ } ->
      "paste-from-kill-ring:" ^ string_of_int index
  | Request_search Forward -> "request-search:forward"
  | Request_search Backward -> "request-search:backward"
  | Repeat_search Forward -> "repeat-search:forward"
  | Repeat_search Backward -> "repeat-search:backward"
  | Request_macro Reserve_macro_input -> "request-macro:reserve-input"
  | Request_macro (Toggle_macro_recording register) ->
      "request-macro:toggle-recording:" ^ register
  | Request_macro (Replay_macro { register; count }) ->
      "request-macro:replay:" ^ register ^ ":" ^ string_of_int count
  | Request_location (Set_location name) -> "request-location:set:" ^ name
  | Request_location (Jump_location name) -> "request-location:jump:" ^ name
  | Request_jump Push_current_jump -> "request-jump:push-current"
  | Request_jump (Traverse_jump { direction = Backward; count }) ->
      "request-jump:backward:" ^ string_of_int count
  | Request_jump (Traverse_jump { direction = Forward; count }) ->
      "request-jump:forward:" ^ string_of_int count
  | Request_workspace Split_view_vertical -> "request-workspace:split-vertical"
  | Request_workspace Split_view_horizontal ->
      "request-workspace:split-horizontal"
  | Request_workspace Focus_next_view -> "request-workspace:focus-next-view"
  | Request_workspace Close_view -> "request-workspace:close-view"
  | Request_workspace Keep_only_view -> "request-workspace:keep-only-view"
  | Request_workspace (Resize_view_width delta) ->
      "request-workspace:resize-view-width:" ^ string_of_int delta
  | Request_workspace (Resize_view_height delta) ->
      "request-workspace:resize-view-height:" ^ string_of_int delta
  | Request_workspace Balance_views -> "request-workspace:balance-views"
  | Request_workspace New_buffer -> "request-workspace:new-buffer"
  | Request_workspace Open_buffer -> "request-workspace:open-buffer"
  | Request_workspace Close_buffer -> "request-workspace:close-buffer"
  | Request_workspace Next_buffer -> "request-workspace:next-buffer"
  | Request_workspace Previous_buffer -> "request-workspace:previous-buffer"
  | Request_viewport (Scroll_view_lines lines) ->
      "request-viewport:scroll-lines:" ^ string_of_int lines
  | Request_viewport (Scroll_view_pages pages) ->
      "request-viewport:scroll-pages:" ^ string_of_int pages
  | Request_viewport Center_view -> "request-viewport:center"
  | Request_external_filter { program; arguments } ->
      "request-external-filter:" ^ program ^ ":" ^ String.concat "," arguments
  | Request_background_process { program; arguments } ->
      "request-background-process:" ^ program ^ ":"
      ^ String.concat "," arguments
  | Request_save -> "request-save"
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
  | Cut_to_clipboard { slot; selector = _; kind } ->
      "cut " ^ Clipboard.kind_name kind ^ " to " ^ Clipboard.slot_name slot
      ^ " and kill history"
  | Paste_from_clipboard { slot; placement } ->
      "paste " ^ Clipboard.slot_name slot ^ " "
      ^ Clipboard.placement_name placement
  | Paste_from_kill_ring { index; placement } ->
      "paste kill history entry " ^ string_of_int index ^ " "
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
  | Request_location (Set_location name) -> "set location " ^ name
  | Request_location (Jump_location name) -> "jump to location " ^ name
  | Request_jump Push_current_jump ->
      "add the current selection to jump history"
  | Request_jump (Traverse_jump { direction = Backward; count }) ->
      Printf.sprintf "jump backward %d time(s)" count
  | Request_jump (Traverse_jump { direction = Forward; count }) ->
      Printf.sprintf "jump forward %d time(s)" count
  | Request_workspace Split_view_vertical -> "split the current view vertically"
  | Request_workspace Split_view_horizontal ->
      "split the current view horizontally"
  | Request_workspace Focus_next_view -> "focus the next view"
  | Request_workspace Close_view -> "close the current view"
  | Request_workspace Keep_only_view -> "keep only the current view"
  | Request_workspace (Resize_view_width delta) ->
      Printf.sprintf "resize the current view width by %d column(s)" delta
  | Request_workspace (Resize_view_height delta) ->
      Printf.sprintf "resize the current view height by %d row(s)" delta
  | Request_workspace Balance_views -> "balance all split-view proportions"
  | Request_workspace New_buffer -> "create an unnamed buffer"
  | Request_workspace Open_buffer -> "open the host buffer prompt"
  | Request_workspace Close_buffer -> "close the active clean buffer"
  | Request_workspace Next_buffer -> "select the next buffer"
  | Request_workspace Previous_buffer -> "select the previous buffer"
  | Request_viewport (Scroll_view_lines lines) ->
      Printf.sprintf "scroll the current view by %d line(s)" lines
  | Request_viewport (Scroll_view_pages pages) ->
      Printf.sprintf "scroll the current view by %d page(s)" pages
  | Request_viewport Center_view ->
      "center the current view on the primary selection"
  | Request_external_filter { program; arguments } -> (
      "filter each current selection through " ^ program
      ^ match arguments with [] -> "" | _ -> " " ^ String.concat " " arguments)
  | Request_background_process { program; arguments } -> (
      "start background process " ^ program
      ^ match arguments with [] -> "" | _ -> " " ^ String.concat " " arguments)
  | Request_save -> "request save"
  | Undo -> "undo"
  | Redo -> "redo"
  | Repeat_last_edit -> "repeat-last-edit"
