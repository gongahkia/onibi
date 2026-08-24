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
  | Await_extension of int
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

val message :
  level:message_level -> text:string -> (t, Zenbu_kernel.Error.t) result

val execute :
  ?selector_id:string -> ?transformation_id:string -> Model_intent.t -> t

val execute_semantic_operation : Semantic_operation.t -> t
val selector_id : t -> string option
val transformation_id : t -> string option
val identity : t -> string
val describe : t -> string
