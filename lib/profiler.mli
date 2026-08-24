type stage =
  | Model_handle
  | Selector_resolve
  | Transformation_apply
  | Transaction_commit
  | Syntax_update
  | Script_load
  | Script_reload
  | Script_command
  | Script_selector
  | Script_transformation
  | Script_event
  | Extension_load
  | Extension_reload
  | Extension_command
  | Extension_selector
  | Extension_transformation
  | Extension_event
  | Extension_wasm_compile
  | Extension_wasm_instantiate
  | Extension_wasm_register
  | Extension_wasm_call
  | Language_sync
  | Language_hover
  | Language_definition
  | Language_completion
  | Language_code_action
  | Language_formatting
  | Language_symbols
  | Language_semantic_tokens
  | Language_rename
  | Lsp_decode

type aggregate
type t

val disabled : unit -> t
val enabled : capacity:int -> (t, Zenbu_kernel.Error.t) result
val is_enabled : t -> bool
val capacity : t -> int option
val measure : t -> ?model_id:string -> stage -> (unit -> 'a) -> 'a
val record : t -> ?model_id:string -> stage -> seconds:float -> unit
val reset : t -> unit
val aggregates : t -> aggregate list
val stage_name : stage -> string
val aggregate_stage : aggregate -> stage
val aggregate_model_id : aggregate -> string option
val aggregate_count : aggregate -> int
val aggregate_total_seconds : aggregate -> float
val aggregate_mean_seconds : aggregate -> float
val aggregate_max_seconds : aggregate -> float
