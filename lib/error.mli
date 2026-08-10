type t =
  | Invalid_document_id of string
  | Invalid_version of int
  | Invalid_utf8 of string
  | Invalid_anchor of { offset : int; byte_length : int; reason : string }
  | Wrong_document of { expected : string; actual : string }
  | Stale_version of { expected : int; actual : int }
  | Invalid_range of string
  | Overlapping_edits of { first_index : int; second_index : int }
  | Invalid_selection_set of string
  | Empty_transaction
  | Invalid_target_version of { source : int; target : int }
  | Malformed_intent of string
  | Invalid_selector of string
  | Invalid_transformation of string
  | Malformed_replay of string
  | Replay_diverged of { step : int; cause : t }
  | History_at_root
  | History_no_redo
  | Unknown_history_node of int
  | Invalid_input_event of string
  | Invalid_command_id of string
  | Invalid_model_status of string
  | Invalid_clipboard_slot of string
  | Clipboard_slot_empty of string
  | Duplicate_command of string
  | Unknown_command of string
  | Invalid_command_arguments of string
  | Invalid_provenance of string
  | Model_execution_failed of string
  | No_repeatable_edit

val to_string : t -> string
