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
  | Duplicate_descriptor of string
  | Unknown_command of string
  | Invalid_command_arguments of string
  | Invalid_provenance of string
  | Script_error of {
      phase : string;
      source : string option;
      line : int option;
      message : string;
    }
  | Extension_error of {
      code : extension_error_code;
      plugin_id : string option;
      provider : string option;
      operation : string option;
      required : string option;
      granted : string list;
      message : string;
    }
  | Model_execution_failed of string
  | No_repeatable_edit

and extension_error_code =
  | Invalid_manifest
  | Incompatible_api
  | Unknown_runtime
  | Unknown_capability
  | Unknown_contribution
  | Capability_denied
  | Contribution_not_declared
  | Namespace_violation
  | Invalid_plugin_package
  | Plugin_not_active
  | Extension_runtime_error

val to_string : t -> string
val extension_error_code_name : extension_error_code -> string
