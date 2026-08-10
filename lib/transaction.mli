type source = User | Test | Replay | System
type metadata
type t

val metadata :
  source:source ->
  ?intent:string ->
  ?description:string ->
  ?provenance:Provenance.t ->
  unit ->
  metadata

val source : metadata -> source
val intent : metadata -> string option
val description : metadata -> string option
val provenance : metadata -> Provenance.t option
val source_to_string : source -> string
val source_of_string : string -> (source, Error.t) result

val create :
  document_id:Document_id.t ->
  source_version:Document_version.t ->
  edits:Edit.t list ->
  ?selection_change:Selection_set.t ->
  metadata:metadata ->
  unit ->
  (t, Error.t) result

val document_id : t -> Document_id.t
val source_version : t -> Document_version.t
val edits : t -> Edit.t list
val selection_change : t -> Selection_set.t option
val metadata_of : t -> metadata
