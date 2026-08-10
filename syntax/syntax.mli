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
