type limits = { fuel : int; memory_bytes : int; deadline_ms : int }

type metrics = {
  compile_seconds : float;
  instantiate_seconds : float;
  call_seconds : float;
  fuel_consumed : int;
}

type initialization = {
  registrations : Zenbu_model_api.Extension_value.t;
  metrics : metrics;
}

type t
type call

val create :
  entrypoint:string ->
  capabilities:string list ->
  limits:limits ->
  (t * initialization, string) result

val start :
  t ->
  token:string ->
  request:Zenbu_model_api.Extension_value.t ->
  (call, string) result

val take :
  t ->
  call ->
  ((Zenbu_model_api.Extension_value.t, string) result * metrics) option

val invoke_blocking :
  t ->
  token:string ->
  request:Zenbu_model_api.Extension_value.t ->
  ((Zenbu_model_api.Extension_value.t, string) result * metrics, string) result

val wakeup_fd : t -> Unix.file_descr
val closed : t -> bool
val cancel : t -> call -> unit
val close : t -> unit
