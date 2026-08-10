(** Model-neutral language-service vocabulary.

    This module deliberately owns editor-facing language values.  LSP and
    JSON-RPC types stop in [zenbu.lsp] and never appear here. *)

module Data : sig
  type t =
    | Null
    | Bool of bool
    | Number of float
    | Text of string
    | List of t list
    | Object of (string * t) list
end

module Server_config : sig
  type t

  val create :
    id:string ->
    language_ids:string list ->
    extensions:string list ->
    executable:string ->
    ?argv:string list ->
    ?environment:(string * string) list ->
    ?root_markers:string list ->
    ?initialization_options:Data.t ->
    ?settings:Data.t ->
    unit ->
    (t, string) result

  val id : t -> string
  val language_ids : t -> string list
  val extensions : t -> string list
  val executable : t -> string
  val argv : t -> string list
  val environment : t -> (string * string) list
  val root_markers : t -> string list
  val initialization_options : t -> Data.t option
  val settings : t -> Data.t option
end

module Registry : sig
  type t

  val empty : t
  val register : t -> Server_config.t -> (t, string) result
  val all : t -> Server_config.t list
  val find_for_path : t -> language_id:string option -> string -> Server_config.t option
  val default : unit -> t
end

module Position : sig
  type encoding = Utf8 | Utf16 | Utf32
  type t = { line : int; character : int }
  type range = { start_ : t; end_ : t }

  val encoding_name : encoding -> string
  val of_name : string -> encoding option

  val offset_to_position :
    contents:string -> encoding:encoding -> byte_offset:int -> (t, string) result

  val position_to_offset :
    contents:string -> encoding:encoding -> t -> (int, string) result

  val offsets_to_range :
    contents:string ->
    encoding:encoding ->
    start_offset:int ->
    stop_offset:int ->
    (range, string) result

  val range_to_offsets :
    contents:string -> encoding:encoding -> range -> (int * int, string) result
end

module Uri : sig
  val file_of_path : string -> string
  val path_of_file : string -> (string, string) result
end

module Workspace : sig
  val discover_root : markers:string list -> file_path:string -> string
end

type diagnostic_severity = Error | Warning | Information | Hint

type diagnostic = {
  start_offset : int;
  stop_offset : int;
  severity : diagnostic_severity;
  message : string;
  source : string option;
  code : string option;
  document_id : string;
  document_version : int;
}

type hover = { text : string; start_offset : int option; stop_offset : int option }

type definition_target = {
  uri : string;
  start_offset : int;
  stop_offset : int;
}

type text_edit = { start_offset : int; stop_offset : int; replacement : string }

type completion = {
  label : string;
  detail : string option;
  documentation : string option;
  sort_text : string option;
  filter_text : string option;
  insert_text : string option;
  text_edit : text_edit option;
  additional_text_edits : text_edit list;
  snippet : bool;
  deprecated : bool;
}

type server_state = Stopped | Starting | Initializing | Ready | Failed | Shutting_down

type status = {
  language_id : string option;
  server_id : string option;
  executable : string option;
  workspace_root : string option;
  state : server_state;
  position_encoding : Position.encoding option;
  sync_kind : [ `None | `Full | `Incremental ] option;
  pending_requests : int;
  diagnostic_count : int;
  last_error : string option;
}

val diagnostic_severity_name : diagnostic_severity -> string
val server_state_name : server_state -> string
