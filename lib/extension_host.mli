(** Runtime-neutral, data-only boundary for semantic extensions.

    Runtime adapters own their private callback handles. Registries retain only
    an [invocation] token and this host, so future adapters need not expose an
    implementation value to the semantic runtime. *)

type kind = Command | Selector | Transformation | Model | Event
type invocation
type t
type deferred
type call

type request = {
  kind : kind;
  operation : string;
  provider : Zenbu_kernel.Provider.t;
  granted : string list;
  context : Extension_value.t;
  arguments : Extension_value.t;
}

type response = Immediate of Extension_value.t | Deferred of deferred

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

val invoke :
  t -> invocation -> request -> (response, Zenbu_kernel.Error.t) result

val deferred : start:(unit -> (call, Zenbu_kernel.Error.t) result) -> response

val call :
  wakeup_fd:Unix.file_descr ->
  closed:(unit -> bool) ->
  take:(unit -> (Extension_value.t, Zenbu_kernel.Error.t) result option) ->
  cancel:(unit -> unit) ->
  call

val start : response -> (call, Zenbu_kernel.Error.t) result
val immediate : response -> (Extension_value.t, Zenbu_kernel.Error.t) result
val wakeup_fd : call -> Unix.file_descr
val closed : call -> bool
val take : call -> (Extension_value.t, Zenbu_kernel.Error.t) result option
val cancel : call -> unit
val has_capability : request -> string -> bool

val require :
  request -> capability:string -> (unit, Zenbu_kernel.Error.t) result

val kind_name : kind -> string
