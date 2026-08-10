(** Private M9 Wasmtime Component Model adapter.

    This interface intentionally contains no Wasmtime handle or terminal/kernel
    value.  Component values are translated directly to the public,
    serialisable [Extension_value] protocol at this boundary. *)

type t

type limits = { fuel : int; memory_bytes : int }

val default_limits : limits

val load :
  entrypoint:string ->
  capabilities:string list ->
  limits:limits ->
  (t, string) result

val register : t -> (Zenbu_model_api.Extension_value.t, string) result

val invoke :
  t ->
  token:string ->
  request:Zenbu_model_api.Extension_value.t ->
  (Zenbu_model_api.Extension_value.t, string) result

val dispose : t -> unit
