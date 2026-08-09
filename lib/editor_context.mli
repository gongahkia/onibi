type selection = { anchor_offset : int; head_offset : int }
type selection_set = { selections : selection list; primary_index : int }
type t

val from_snapshot :
  snapshot:Zenbu_kernel.Document_snapshot.t ->
  commands:Command_descriptor.t list ->
  ?clipboard:Clipboard.t ->
  ?syntax:Zenbu_syntax.Syntax.Snapshot.t ->
  unit ->
  t

val document_id : t -> string
val document_version : t -> int
val contents : t -> string
val byte_length : t -> int
val selections : t -> selection_set
val command_descriptors : t -> Command_descriptor.t list
val clipboard_entry : t -> slot:Clipboard.slot -> Clipboard.entry option
val syntax : t -> Zenbu_syntax.Syntax.Snapshot.t option
