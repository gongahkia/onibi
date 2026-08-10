type execution_id = int

type t =
  | Input_received of { execution_id : execution_id; input : string }
  | Interaction_started of { execution_id : execution_id; interaction_id : int }
  | Interaction_completed of {
      execution_id : execution_id;
      interaction_id : int;
      started_execution : execution_id;
      inputs : string list;
    }
  | Model_before of {
      execution_id : execution_id;
      model_id : string;
      status_id : string;
      status_label : string;
    }
  | Model_transition of {
      execution_id : execution_id;
      model_id : string;
      previous_status : string;
      next_status : string;
    }
  | Model_effect of { execution_id : execution_id; effect_id : string }
  | Command_invoked of { execution_id : execution_id; command_id : string }
  | Selector_resolved of {
      execution_id : execution_id;
      selector_id : string;
      selection_count : int;
    }
  | Transformation_applied of {
      execution_id : execution_id;
      transformation_id : string;
    }
  | Transaction_created of {
      execution_id : execution_id;
      source_version : int;
      edit_count : int;
      provenance : Zenbu_kernel.Provenance.t;
    }
  | Transaction_committed of {
      execution_id : execution_id;
      change_id : int;
      source_version : int;
      result_version : int;
      edit_count : int;
      provenance : Zenbu_kernel.Provenance.t;
    }
  | Transaction_rejected of { execution_id : execution_id; reason : string }
  | History_changed of {
      execution_id : execution_id;
      operation : string;
      current_change : int option;
    }
  | Syntax_refreshed of {
      execution_id : execution_id;
      language_id : string;
      document_version : int;
      strategy : string;
      has_error : bool;
    }
  | Script_lifecycle of {
      execution_id : execution_id;
      phase : string;
      generation_id : int option;
      provider : Zenbu_kernel.Provider.t option;
      outcome : string;
      reason : string option;
    }
  | Script_callback of {
      execution_id : execution_id;
      kind : string;
      provider : Zenbu_kernel.Provider.t;
      semantic_id : string option;
      outcome : string;
      reason : string option;
    }
  | Extension_lifecycle of {
      execution_id : execution_id;
      phase : string;
      provider : Zenbu_kernel.Provider.t;
      outcome : string;
      reason : string option;
    }
  | Extension_callback of {
      execution_id : execution_id;
      kind : string;
      provider : Zenbu_kernel.Provider.t;
      semantic_id : string option;
      outcome : string;
      reason : string option;
    }
  | Capability_denied of {
      execution_id : execution_id;
      provider : Zenbu_kernel.Provider.t;
      operation : string;
      required : string;
      granted : string list;
    }
  | Binding_resolved of {
      execution_id : execution_id;
      input : string;
      command_id : string;
      provider : Zenbu_kernel.Provider.t;
      scope : string;
    }
  | Error_reported of { execution_id : execution_id; reason : string }

val execution_id : t -> execution_id
val name : t -> string
