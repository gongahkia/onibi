type wasm_limits = { fuel : int; memory_bytes : int; deadline_ms : int }
type t

val filename : string
val parse : string -> (t, Zenbu_kernel.Error.t) result
val manifest_version : t -> int
val id : t -> Plugin_id.t
val name : t -> string
val description : t -> string option
val version : t -> Plugin_version.t
val api : t -> int
val runtime : t -> string
val entrypoint : t -> string
val entrypoint_path : t -> string
val wasm_limits : t -> wasm_limits option
val package_dir : t -> string
val path : t -> string
val contributions : t -> Contribution.t list
val requested_capabilities : t -> Capability.t list
