(** Private-process LSP adapter. Public values are deliberately Zenbu-owned;
    [Lsp] and [Jsonrpc] types do not escape this package. *)

type request_kind =
  | Hover
  | Definition
  | Completion
  | Code_action
  | Document_formatting
  | Range_formatting
  | Document_symbols
  | Workspace_symbols
  | Rename

type formatting_scope = Document | Range
type symbol_scope = Document_symbols_scope | Workspace_symbols_scope

type workspace_edit = {
  uri : string;
      (** The exact document contents used to translate the LSP positions. *)
  source_contents : string;
  edits : Zenbu_language.Language.text_edit list;
}

type code_action = {
  title : string;
  edits : workspace_edit list option;
  command : string option;
  disabled_reason : string option;
}

type symbol = {
  label : string;
  detail : string option;
  kind : int;
  uri : string;
  start_offset : int;
  stop_offset : int;
  hierarchy : string list;
}

type event =
  | Initialized
  | Diagnostics of {
      document_version : int option;
      diagnostics : Zenbu_language.Language.diagnostic list;
    }
  | Hover_result of {
      request_id : int;
      document_version : int;
      byte_offset : int;
      hover : Zenbu_language.Language.hover option;
    }
  | Definition_result of {
      request_id : int;
      document_version : int;
      byte_offset : int;
      targets : Zenbu_language.Language.definition_target list;
    }
  | Completion_result of {
      request_id : int;
      document_version : int;
      byte_offset : int;
      items : Zenbu_language.Language.completion list;
    }
  | Code_action_result of {
      request_id : int;
      document_version : int;
      start_offset : int;
      stop_offset : int;
      actions : code_action list;
    }
  | Formatting_result of {
      request_id : int;
      document_version : int;
      scope : formatting_scope;
      start_offset : int;
      stop_offset : int;
      edits : workspace_edit list;
    }
  | Symbol_result of {
      request_id : int;
      document_version : int;
      scope : symbol_scope;
      query : string;
      symbols : symbol list;
    }
  | Rename_result of {
      request_id : int;
      document_version : int;
      byte_offset : int;
      edits : workspace_edit list;
    }
  | Apply_edit of { request_id : int; edits : workspace_edit list }
  | Server_message of string
  | Request_failed of {
      request_id : int;
      kind : request_kind;
      document_version : int;
      reason : string;
    }
  | Server_failed of string
  | Server_exited of string

type t

val start :
  config:Zenbu_language.Language.Server_config.t ->
  document_id:string ->
  document_version:int ->
  file_path:string ->
  contents:string ->
  trace:Zenbu_model_api.Trace.t ->
  profiler:Zenbu_model_api.Profiler.t ->
  t

val status : t -> Zenbu_language.Language.status
val wakeup_fd : t -> Unix.file_descr
val drain : t -> event list
val set_execution_id : t -> execution_id:int -> unit
val set_workspace_documents : t -> (string * string) list -> unit

val notify_change :
  t ->
  source_contents:string ->
  contents:string ->
  document_version:int ->
  edits:Zenbu_language.Language.text_edit list ->
  unit

val observe_document_version : t -> document_version:int -> unit
val notify_save : t -> contents:string -> document_version:int -> unit
val request_hover : t -> byte_offset:int -> (int, string) result
val request_definition : t -> byte_offset:int -> (int, string) result
val request_completion : t -> byte_offset:int -> (int, string) result

val request_code_actions :
  t -> start_offset:int -> stop_offset:int -> (int, string) result

val request_document_formatting : t -> (int, string) result

val request_range_formatting :
  t -> start_offset:int -> stop_offset:int -> (int, string) result

val request_document_symbols : t -> (int, string) result
val request_workspace_symbols : t -> query:string -> (int, string) result

val request_rename :
  t -> byte_offset:int -> new_name:string -> (int, string) result

val cancel_stale : t -> document_version:int -> unit
val cancel : t -> request_kind -> unit

val respond_apply_edit :
  t -> request_id:int -> applied:bool -> reason:string option -> unit

val restart : t -> unit
val close : t -> unit
