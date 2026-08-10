(** Typed, model-neutral views for local inspection. Text formatting functions
    consume these views; they are not the source of observability data. *)

type description
type selection
type edit_preview
type change
type history_node
type history
type syntax_node
type syntax
type syntax_service
type why
type api

val describe_command : Command_descriptor.t -> description
val describe_model : Editing_model.descriptor -> description
val describe_semantic : Zenbu_kernel.Semantic_descriptor.t -> description
val description_id : description -> string
val description_kind : description -> string
val description_title : description -> string
val description_summary : description -> string option
val description_provider : description -> Zenbu_kernel.Provider.t
val description_fields : description -> (string * string) list
val commands : Command_registry.t -> description list
val find_command : Command_registry.t -> string -> description option

val semantic_registry :
  ?semantic_behaviors:Semantic_behavior_registry.t ->
  unit ->
  Semantic_registry.t

val find_semantic : Semantic_registry.t -> string -> description option
val selections : Editor_context.t -> selection list
val selection_primary : selection -> bool
val selection_anchor_offset : selection -> int
val selection_head_offset : selection -> int
val selection_start_offset : selection -> int
val selection_stop_offset : selection -> int
val change : Zenbu_kernel.History.change -> change
val change_id : change -> int
val change_source_version : change -> int
val change_result_version : change -> int
val change_source : change -> string
val change_intent : change -> string option
val change_description : change -> string option
val change_edit_count : change -> int
val change_provenance : change -> Zenbu_kernel.Provenance.t option
val change_edits : change -> edit_preview list
val edit_start_offset : edit_preview -> int
val edit_stop_offset : edit_preview -> int
val edit_replacement : edit_preview -> string
val edit_removed : edit_preview -> string
val history : ?saved_version:int -> Zenbu_kernel.History.t -> history
val history_nodes : history -> history_node list
val history_current_id : history -> int
val history_node_id : history_node -> int
val history_node_parent_id : history_node -> int option
val history_node_child_ids : history_node -> int list
val history_node_change : history_node -> change option
val history_node_current : history_node -> bool
val history_node_saved : history_node -> bool
val find_change : history -> int -> change option
val syntax : Editor_context.t -> syntax option
val syntax_language_id : syntax -> string
val syntax_document_version : syntax -> int
val syntax_has_error : syntax -> bool
val syntax_node : syntax -> syntax_node option
val syntax_node_kind : syntax_node -> string
val syntax_node_start_offset : syntax_node -> int
val syntax_node_stop_offset : syntax_node -> int
val syntax_node_parent_kind : syntax_node -> string option
val syntax_node_child_count : syntax_node -> int
val syntax_node_named : syntax_node -> bool
val syntax_node_error : syntax_node -> bool
val syntax_service : Zenbu_syntax.Syntax.Service.t -> syntax_service
val syntax_service_language_id : syntax_service -> string
val syntax_service_cached_version : syntax_service -> int option
val syntax_service_last_strategy : syntax_service -> string option
val why : Trace.t -> execution_id:int -> why option
val why_execution_id : why -> int
val why_events : why -> Trace_event.t list
val latest_why : Trace.t -> why option

val api :
  models:Editing_model.descriptor list ->
  commands:Command_registry.t ->
  ?semantic_behaviors:Semantic_behavior_registry.t ->
  unit ->
  api

val api_models : api -> description list
val api_commands : api -> description list
val api_selectors : api -> description list
val api_transformations : api -> description list
val api_languages : api -> Zenbu_syntax.Syntax.Language.t list
val format_description : description -> string list
val format_commands : description list -> string list

val format_bindings :
  Editing_model.descriptor -> Model_status.t -> Input_rule.t list -> string list

val format_selection : selection list -> string list
val format_change : change -> string list
val format_history : history -> string list
val format_syntax : syntax -> string list
val format_syntax_service : syntax_service -> string list
val format_why : why -> string list
val format_profile : Profiler.t -> string list
val format_api : api -> string list
