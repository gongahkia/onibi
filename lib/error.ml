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
  | Malformed_replay of string
  | Replay_diverged of { step : int; cause : t }
  | History_at_root
  | History_no_redo
  | Unknown_history_node of int

let to_string = function
  | Invalid_document_id value -> Printf.sprintf "invalid document id: %S" value
  | Invalid_version value -> Printf.sprintf "invalid document version: %d" value
  | Invalid_utf8 context -> Printf.sprintf "invalid UTF-8 in %s" context
  | Invalid_anchor { offset; byte_length; reason } ->
      Printf.sprintf "invalid anchor offset %d for %d-byte text: %s" offset
        byte_length reason
  | Wrong_document { expected; actual } ->
      Printf.sprintf "wrong document: expected %s, got %s" expected actual
  | Stale_version { expected; actual } ->
      Printf.sprintf "stale version: expected %d, got %d" expected actual
  | Invalid_range message -> Printf.sprintf "invalid range: %s" message
  | Overlapping_edits { first_index; second_index } ->
      Printf.sprintf "conflicting edits %d and %d" first_index second_index
  | Invalid_selection_set message -> Printf.sprintf "invalid selection set: %s" message
  | Empty_transaction -> "transaction has neither edits nor a selection change"
  | Invalid_target_version { source; target } ->
      Printf.sprintf "target version %d is not newer than source version %d" target source
  | Malformed_intent message -> Printf.sprintf "malformed intent: %s" message
  | Malformed_replay message -> Printf.sprintf "malformed replay: %s" message
  | Replay_diverged { step; cause } ->
      Printf.sprintf "replay diverged at action %d: %s" step (to_string cause)
  | History_at_root -> "cannot undo at history root"
  | History_no_redo -> "no redo branch is available"
  | Unknown_history_node id -> Printf.sprintf "unknown history node %d" id

