type severity = Error | Warning
type t

val make :
  severity:severity ->
  message:string ->
  source_name:string ->
  source:string ->
  Source_span.t ->
  t

val severity : t -> severity
val message : t -> string
val source_name : t -> string
val span : t -> Source_span.t
val line : t -> int
val column : t -> int
val line_column : string -> int -> int * int
val format : t -> string
