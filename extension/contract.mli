(** Authoritative Extension API v1 metadata. Generated reference material is
    derived from these descriptors. *)

type service = {
  id : string;
  purpose : string;
  capability : Capability.t option;
  arguments : string;
  result : string;
  errors : string list;
  since : int;
}

val api_version : int
val manifest_version : int
val runtime_ids : string list
val services : service list
val stable_error_codes : string list
val supports_runtime : string -> bool
val markdown : unit -> string
val lua_stub : unit -> string
