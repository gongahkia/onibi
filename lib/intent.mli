type t =
  | Insert_text of string
  | Delete_selected_ranges
  | Replace_selected_ranges of string
  | Replace_selection_contents of string list
  | Replace_ranges of {
      selections : Selection_spec.t list;
      primary : int;
      contents : string list;
    }
  | Set_selections of { selections : Selection_spec.t list; primary : int }
  | Apply of { selector : Selector.t; transformation : Transformation.t }

val identity : t -> string

val resolve :
  source:Transaction.source ->
  ?description:string ->
  ?provenance:Provenance.t ->
  Document_snapshot.t ->
  t ->
  (Transaction.t, Error.t) result

val resolve_on_selections :
  source:Transaction.source ->
  ?description:string ->
  ?provenance:Provenance.t ->
  intent:string ->
  Document_snapshot.t ->
  Selection_set.t ->
  Transformation.t ->
  (Transaction.t, Error.t) result
