(** Main-thread delivery registry for deferred extension responses.

    The editing models see only the integer carried by
    [Model_effect.Await_extension]. Runtime handles remain in [Extension_host]
    and are polled by the host session. *)

type snapshot = {
  document_id : string;
  document_version : int;
  contents : string;
}

type completion = {
  id : int;
  provider : Zenbu_kernel.Provider.t;
  operation : string;
  snapshot : snapshot;
  input : Input_event.t;
  provenance : Zenbu_kernel.Provenance.t;
  result : (Model_effect.t list, Zenbu_kernel.Error.t) result;
}

val schedule :
  response:Extension_host.response ->
  request:Extension_host.request ->
  context:Editor_context.t ->
  decode:
    (Extension_host.request ->
    Extension_value.t ->
    (Model_effect.t list, Zenbu_kernel.Error.t) result) ->
  (int, Zenbu_kernel.Error.t) result

val new_owner : unit -> int

val activate :
  id:int ->
  owner:int ->
  input:Input_event.t ->
  provenance:Zenbu_kernel.Provenance.t ->
  (unit, Zenbu_kernel.Error.t) result

val wakeup_fds : owners:int list -> Unix.file_descr list
val drain : owners:int list -> completion list
val cancel : owners:int list -> unit
