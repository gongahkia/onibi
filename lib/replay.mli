type selection_state = { selections : Selection_spec.t list; primary : int }
type edit_spec = { start_offset : int; stop_offset : int; text : string }

type transaction_spec = {
  source : Transaction.source;
  intent : string option;
  description : string option;
  edits : edit_spec list;
  selection_change : selection_state option;
}

type action = Intent of Intent.t | Transaction of transaction_spec
type t

val create :
  document_id:string ->
  contents:string ->
  initial_selections:selection_state ->
  actions:action list ->
  (t, Error.t) result

val document_id : t -> string
val contents : t -> string
val initial_selections : t -> selection_state
val actions : t -> action list
val run : t -> (History.t, Error.t) result
val to_string : t -> string
val of_string : string -> (t, Error.t) result
