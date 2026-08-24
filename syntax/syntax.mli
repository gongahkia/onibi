(** Zenbu's model-neutral, version-bound syntax service. Parser and grammar
    implementation details intentionally remain private to this library. *)

module Error : sig
  type t =
    | Unknown_language of string
    | Stale_document of {
        expected_id : string;
        expected_version : int;
        actual_id : string;
        actual_version : int;
      }
    | Invalid_edit of string
    | Backend_failure of string

  val to_string : t -> string
end

module Language : sig
  type t

  val id : t -> string
  val display_name : t -> string
  val extensions : t -> string list
  val supported : unit -> t list
  val find : string -> t option
  val detect_path : string -> t option
end

(** Host-owned grammar registration. Candidate manifests can select only the
    statically linked bundles enumerated by [Bundle]; there is no native-path,
    dynamic-library, or network loading entry point. *)
module Grammar : sig
  module Source : sig
    type t = Built_in of { package : string; revision : string }

    val package : t -> string
    val revision : t -> string
  end

  module Bundle : sig
    type t = Ocaml | Json

    val id : t -> string
    val source : t -> Source.t
    val version : t -> string
    val abi : t -> int
    val integrity : t -> string
  end

  module Candidate : sig
    type t

    val create :
      id:string ->
      display_name:string ->
      extensions:string list ->
      source:Source.t ->
      version:string ->
      abi:int ->
      integrity:string ->
      bundle:Bundle.t ->
      t
  end

  module Registry : sig
    type t
    type error

    val builtins : unit -> t
    val stage : Candidate.t list -> (t, error) result
    val current : unit -> t

    val reload : Candidate.t list -> (t, error) result
    (** Validates and activates the complete replacement registry atomically. On
        [Error], the current registry is retained. Existing services retain
        their own parser and language snapshot. *)

    val languages : t -> Language.t list
    val find : t -> string -> Language.t option
    val detect_path : t -> string -> Language.t option
    val error_to_string : error -> string
  end

  val maximum_registered_grammars : int
  val maximum_extensions_per_grammar : int
  val minimum_tree_sitter_abi : int
  val maximum_tree_sitter_abi : int
  val source : Language.t -> Source.t
  val version : Language.t -> string
  val abi : Language.t -> int
  val integrity : Language.t -> string
end

module Kind : sig
  type t

  val of_string : string -> t
  val to_string : t -> string
  val equal : t -> t -> bool
end

module Snapshot : sig
  type t

  val document_id : t -> string
  val document_version : t -> int
  val language : t -> Language.t
  val has_error : t -> bool
  val matches_document : t -> Zenbu_kernel.Document_snapshot.t -> bool

  module Node : sig
    type t

    val document_id : t -> string
    val document_version : t -> int
    val kind : t -> Kind.t
    val is_named : t -> bool
    val is_error : t -> bool
    val is_missing : t -> bool
    val has_error : t -> bool
    val start_offset : t -> int
    val stop_offset : t -> int
    val range : t -> (Zenbu_kernel.Range.t, Error.t) result
    val parent_named : t -> t option
    val first_named_child : t -> t option
    val named_children : t -> t list
    val next_named_sibling : t -> t option
    val previous_named_sibling : t -> t option
  end

  val root : t -> Node.t

  val smallest_named_containing :
    t -> start_offset:int -> stop_offset:int -> Node.t option
end

(** Bounded Tree-sitter pattern queries projected into Zenbu-owned capture
    metadata and kernel selections. Compiled queries retain no public parser,
    tree, node, cursor, or FFI value. *)
module Query : sig
  type t
  type capture

  type error =
    | Query_too_large of { maximum_bytes : int; actual_bytes : int }
    | Query_too_complex of {
        maximum_patterns : int;
        actual_patterns : int;
        maximum_capture_names : int;
        actual_capture_names : int;
      }
    | Invalid_query of string
    | Snapshot_has_parse_error
    | Stale_snapshot of {
        expected_id : string;
        expected_version : int;
        actual_id : string;
        actual_version : int;
      }
    | Wrong_language of {
        expected_id : string;
        expected_version : string;
        actual_id : string;
        actual_version : string;
      }
    | Result_limit_exceeded of { maximum_captures : int }
    | Invalid_capture of string
    | Backend_failure of string

  val maximum_query_bytes : int
  val maximum_patterns : int
  val maximum_capture_names : int
  val maximum_captures : int

  val compile : Snapshot.t -> source:string -> (t, error) result
  (** Compiles a UTF-8 query against an error-free snapshot's exact language
      identity and document version. *)

  val document_id : t -> string
  val document_version : t -> int
  val language_id : t -> string
  val language_version : t -> string

  val captures : t -> snapshot:Snapshot.t -> (capture list, error) result
  (** Runs only against a snapshot matching the compiled query's document,
      version, language variant, grammar version, ABI, and integrity token. A
      result above [maximum_captures] is rejected rather than truncated. *)

  val capture_name : capture -> string
  val capture_range : capture -> Zenbu_kernel.Range.t

  val selections :
    t ->
    snapshot:Snapshot.t ->
    capture:string ->
    (Zenbu_kernel.Selection_set.t, error) result
  (** Converts one named capture's non-overlapping result ranges into a normal
      kernel selection set. Every range is checked against the bound UTF-8
      document before this conversion. *)

  val error_to_string : error -> string
end

module Selector : sig
  type t =
    | Focus_primary
    | Containing
    | Parent
    | First_child
    | Next_sibling
    | Previous_sibling
    | Expand
    | Same_kind_siblings

  val resolve :
    Snapshot.t ->
    anchor_offset:int ->
    head_offset:int ->
    t ->
    Snapshot.Node.t list

  val id : t -> string
  val descriptors : unit -> Zenbu_kernel.Semantic_descriptor.t list
end

(** A stable, presentation-oriented classification derived from the current
    syntax snapshot. It exposes byte ranges only; renderers choose their own
    colours and do not receive Tree-sitter values. *)
module Highlight : sig
  type class_ = Keyword | String | Number | Comment | Type | Constructor
  type span

  val class_ : span -> class_
  val start_offset : span -> int
  val stop_offset : span -> int
  val spans : Snapshot.t -> span list
  val class_name : class_ -> string
end

module Service : sig
  type t
  type strategy = Cached | Full_parse | Incremental_parse | Tree_copy
  type status

  val maximum_source_bytes : int
  val create : Language.t -> t
  val language : t -> Language.t

  val refresh :
    t -> Zenbu_kernel.Document_snapshot.t -> (Snapshot.t, Error.t) result

  val update :
    t ->
    before:Zenbu_kernel.Document_snapshot.t ->
    transaction:Zenbu_kernel.Transaction.t ->
    after:Zenbu_kernel.Document_snapshot.t ->
    (Snapshot.t, Error.t) result

  val cached : t -> Snapshot.t option
  val status : t -> status
  val status_language : status -> Language.t
  val status_cached_version : status -> int option
  val status_last_strategy : status -> strategy option
  val strategy_to_string : strategy -> string
end
