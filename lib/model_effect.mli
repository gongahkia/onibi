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

val message :
  level:message_level -> text:string -> (t, Zenbu_kernel.Error.t) result

val execute :
  ?selector_id:string -> ?transformation_id:string -> Model_intent.t -> t

val execute_semantic_operation : Semantic_operation.t -> t
val selector_id : t -> string option
val transformation_id : t -> string option
val identity : t -> string
val describe : t -> string
