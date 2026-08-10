type t

type limits = { fuel : int; memory_bytes : int }

let default_limits = { fuel = 5_000_000; memory_bytes = 16 * 1024 * 1024 }

external load_raw :
  string ->
  string list ->
  int ->
  int ->
  (t, string) result = "caml_zenbu_wasmtime_load"

external register_raw :
  t -> (Zenbu_model_api.Extension_value.t, string) result
  = "caml_zenbu_wasmtime_register"

external invoke_raw :
  t ->
  string ->
  Zenbu_model_api.Extension_value.t ->
  (Zenbu_model_api.Extension_value.t, string) result
  = "caml_zenbu_wasmtime_invoke"

external dispose_raw : t -> unit = "caml_zenbu_wasmtime_dispose"

let load ~entrypoint ~capabilities ~limits =
  if limits.fuel <= 0 then Error "fuel limit must be positive"
  else if limits.memory_bytes <= 0 then Error "memory limit must be positive"
  else load_raw entrypoint capabilities limits.fuel limits.memory_bytes

let register = register_raw
let invoke value ~token ~request =
  Result.bind
    (invoke_raw value token (Wasm_value.encode request))
    Wasm_value.decode
let dispose = dispose_raw
