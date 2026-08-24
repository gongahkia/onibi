(** Bounded, snapshot-bound display contributions. Providers contribute only
    immutable text and anchors; they never receive a renderer, terminal, or
    event-loop handle. *)

type placement = Before | After

type item =
  | Inline of { anchor_offset : int; text : string }
  | Virtual_line of { anchor_offset : int; placement : placement; text : string }

type contribution
type response = (contribution, string) result
type resolved
type collection

val create :
  provider_id:string ->
  priority:int ->
  document_id:string ->
  document_version:int ->
  items:item list ->
  (contribution, string) result

val collect :
  contents:string ->
  document_id:string ->
  document_version:int ->
  response list ->
  collection
(** Rejects failed, stale, oversized, duplicate, and invalid-anchor provider
    outputs. Accepted items are deterministically ordered by priority, provider
    id, and their declared item order. *)

val items : collection -> resolved list
val rejections : collection -> string list
val provider_id : resolved -> string
val item : resolved -> item

val inspection_lines : collection -> string list
