(** Runtime-neutral, data-only boundary for semantic extensions.

    Runtime adapters own their private callback handles. Registries retain only
    an [invocation] token and this host, so future adapters need not expose an
    implementation value to the semantic runtime. *)

type kind = Command | Selector | Transformation | Event

type invocation
type t

type request = {
  kind : kind;
  operation : string;
  provider : Zenbu_kernel.Provider.t;
  granted : string list;
  context : Extension_value.t;
  arguments : Extension_value.t;
}

type response = Extension_value.t

val create :
  runtime:string ->
  invoke:(invocation -> request -> (response, Zenbu_kernel.Error.t) result) ->
  t

val runtime : t -> string

val invocation :
  token:string ->
  provider:Zenbu_kernel.Provider.t ->
  granted:string list ->
  invocation

val invocation_provider : invocation -> Zenbu_kernel.Provider.t
val invocation_granted : invocation -> string list
val invocation_token : invocation -> string

val request :
  invocation ->
  kind:kind ->
  operation:string ->
  context:Editor_context.t ->
  arguments:Extension_value.t ->
  request

val invoke : t -> invocation -> request -> (response, Zenbu_kernel.Error.t) result
val has_capability : request -> string -> bool

val require :
  request -> capability:string -> (unit, Zenbu_kernel.Error.t) result

val kind_name : kind -> string
