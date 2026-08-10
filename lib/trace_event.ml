type execution_id = int

type t =
  | Input_received of { execution_id : execution_id; input : string }
  | Interaction_started of { execution_id : execution_id; interaction_id : int }
  | Interaction_completed of { execution_id : execution_id; interaction_id : int; started_execution : execution_id; inputs : string list }
  | Model_before of { execution_id : execution_id; model_id : string; status_id : string; status_label : string }
  | Model_transition of { execution_id : execution_id; model_id : string; previous_status : string; next_status : string }
  | Model_effect of { execution_id : execution_id; effect_id : string }
  | Command_invoked of { execution_id : execution_id; command_id : string }
  | Selector_resolved of { execution_id : execution_id; selector_id : string; selection_count : int }
  | Transformation_applied of { execution_id : execution_id; transformation_id : string }
  | Transaction_created of { execution_id : execution_id; source_version : int; edit_count : int; provenance : Zenbu_kernel.Provenance.t }
  | Transaction_committed of { execution_id : execution_id; change_id : int; source_version : int; result_version : int; edit_count : int; provenance : Zenbu_kernel.Provenance.t }
  | Transaction_rejected of { execution_id : execution_id; reason : string }
  | History_changed of { execution_id : execution_id; operation : string; current_change : int option }
  | Syntax_refreshed of { execution_id : execution_id; language_id : string; document_version : int; strategy : string; has_error : bool }
  | Error_reported of { execution_id : execution_id; reason : string }

let execution_id = function
  | Input_received { execution_id; _ }
  | Interaction_started { execution_id; _ }
  | Interaction_completed { execution_id; _ }
  | Model_before { execution_id; _ }
  | Model_transition { execution_id; _ }
  | Model_effect { execution_id; _ }
  | Command_invoked { execution_id; _ }
  | Selector_resolved { execution_id; _ }
  | Transformation_applied { execution_id; _ }
  | Transaction_created { execution_id; _ }
  | Transaction_committed { execution_id; _ }
  | Transaction_rejected { execution_id; _ }
  | History_changed { execution_id; _ }
  | Syntax_refreshed { execution_id; _ }
  | Error_reported { execution_id; _ } -> execution_id

let name = function
  | Input_received _ -> "input-received"
  | Interaction_started _ -> "interaction-started"
  | Interaction_completed _ -> "interaction-completed"
  | Model_before _ -> "model-before"
  | Model_transition _ -> "model-transition"
  | Model_effect _ -> "model-effect"
  | Command_invoked _ -> "command-invoked"
  | Selector_resolved _ -> "selector-resolved"
  | Transformation_applied _ -> "transformation-applied"
  | Transaction_created _ -> "transaction-created"
  | Transaction_committed _ -> "transaction-committed"
  | Transaction_rejected _ -> "transaction-rejected"
  | History_changed _ -> "history-changed"
  | Syntax_refreshed _ -> "syntax-refreshed"
  | Error_reported _ -> "error-reported"
