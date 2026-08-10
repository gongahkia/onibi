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
  | Extension_abi_mismatch
  | Extension_fuel_exhausted
  | Extension_memory_exhausted
  | Extension_trap
  | Extension_response_limit

let extension_error_code_name = function
  | Invalid_manifest -> "invalid-manifest"
  | Incompatible_api -> "incompatible-api"
  | Unknown_runtime -> "unknown-runtime"
  | Unknown_capability -> "unknown-capability"
  | Unknown_contribution -> "unknown-contribution"
  | Capability_denied -> "capability-denied"
  | Contribution_not_declared -> "contribution-not-declared"
  | Namespace_violation -> "namespace-violation"
  | Invalid_plugin_package -> "invalid-plugin-package"
  | Plugin_not_active -> "plugin-not-active"
  | Extension_runtime_error -> "extension-runtime-error"
  | Extension_abi_mismatch -> "extension-abi-mismatch"
  | Extension_fuel_exhausted -> "extension-fuel-exhausted"
  | Extension_memory_exhausted -> "extension-memory-exhausted"
  | Extension_trap -> "extension-trap"
  | Extension_response_limit -> "extension-response-limit"

let rec to_string = function
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
  | Invalid_selection_set message ->
      Printf.sprintf "invalid selection set: %s" message
  | Empty_transaction -> "transaction has neither edits nor a selection change"
  | Invalid_target_version { source; target } ->
      Printf.sprintf "target version %d is not newer than source version %d"
        target source
  | Malformed_intent message -> Printf.sprintf "malformed intent: %s" message
  | Invalid_selector message -> Printf.sprintf "invalid selector: %s" message
  | Invalid_transformation message ->
      Printf.sprintf "invalid transformation: %s" message
  | Malformed_replay message -> Printf.sprintf "malformed replay: %s" message
  | Replay_diverged { step; cause } ->
      Printf.sprintf "replay diverged at action %d: %s" step (to_string cause)
  | History_at_root -> "cannot undo at history root"
  | History_no_redo -> "no redo branch is available"
  | Unknown_history_node id -> Printf.sprintf "unknown history node %d" id
  | Invalid_input_event message ->
      Printf.sprintf "invalid input event: %s" message
  | Invalid_command_id value -> Printf.sprintf "invalid command id: %S" value
  | Invalid_model_status message ->
      Printf.sprintf "invalid model status: %s" message
  | Invalid_clipboard_slot slot ->
      Printf.sprintf "invalid clipboard slot: %s" slot
  | Clipboard_slot_empty slot ->
      Printf.sprintf "clipboard slot is empty: %s" slot
  | Duplicate_command id -> Printf.sprintf "duplicate command: %s" id
  | Duplicate_descriptor id -> Printf.sprintf "duplicate descriptor: %s" id
  | Unknown_command id -> Printf.sprintf "unknown command: %s" id
  | Invalid_command_arguments message ->
      Printf.sprintf "invalid command arguments: %s" message
  | Invalid_provenance message ->
      Printf.sprintf "invalid provenance: %s" message
  | Script_error { phase; source; line; message } ->
      let location =
        match (source, line) with
        | None, None -> ""
        | Some source, None -> " in " ^ source
        | None, Some line -> Printf.sprintf " at line %d" line
        | Some source, Some line -> Printf.sprintf " in %s:%d" source line
      in
      Printf.sprintf "script %s error%s: %s" phase location message
  | Extension_error
      { code; plugin_id; provider; operation; required; granted; message } ->
      let details =
        [
          Option.map (fun value -> "plugin=" ^ value) plugin_id;
          Option.map (fun value -> "provider=" ^ value) provider;
          Option.map (fun value -> "operation=" ^ value) operation;
          Option.map (fun value -> "required=" ^ value) required;
          (match granted with
          | [] -> None
          | values -> Some ("granted=" ^ String.concat "," values));
        ]
        |> List.filter_map Fun.id
      in
      Printf.sprintf "extension %s%s: %s"
        (extension_error_code_name code)
        (match details with
        | [] -> ""
        | _ -> " (" ^ String.concat "; " details ^ ")")
        message
  | Model_execution_failed message ->
      Printf.sprintf "model execution failed: %s" message
  | No_repeatable_edit -> "no repeatable semantic edit is available"
