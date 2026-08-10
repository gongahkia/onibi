(** Runtime-neutral session registrations emitted by an extension adapter.

    Commands and semantic behaviours already cross [Extension_host]. Bindings
    and event hooks are session policy, so their adapter-owned callback remains
    behind this data-only registration surface rather than exposing a runtime
    handle to the session. *)

type event = Document_changed | After_save

type scope =
  | Global
  | Model of string
  | Model_status of { model : string; status : string }

type binding
type hook

val binding :
  input:Input_event.t ->
  command:string ->
  scope:scope ->
  provider:Zenbu_kernel.Provider.t ->
  binding

val binding_input : binding -> Input_event.t
val binding_command : binding -> string
val binding_scope : binding -> scope
val binding_provider : binding -> Zenbu_kernel.Provider.t

val hook :
  event:event ->
  provider:Zenbu_kernel.Provider.t ->
  run:
    (Editor_context.t ->
    (Model_effect.t list, Zenbu_kernel.Error.t) result) ->
  hook

val hook_event : hook -> event
val hook_provider : hook -> Zenbu_kernel.Provider.t

val run_hook :
  hook ->
  Editor_context.t ->
  (Model_effect.t list, Zenbu_kernel.Error.t) result
