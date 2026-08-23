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
  | Mode of string

type binding
type hook

val binding :
  input:Input_event.t ->
  command:string ->
  scope:scope ->
  next_mode:string option ->
  provider:Zenbu_kernel.Provider.t ->
  binding

val binding_sequence :
  head:Input_event.t ->
  tail:Input_event.t list ->
  command:string ->
  scope:scope ->
  next_mode:string option ->
  provider:Zenbu_kernel.Provider.t ->
  binding
(** Register a nonempty ordered logical-input sequence. [head] is separate so
    adapter code cannot construct an empty binding. *)

val binding_input : binding -> Input_event.t
(** Legacy one-event projection. New consumers should inspect [binding_inputs].
*)

val binding_inputs : binding -> Input_event.t list
val binding_command : binding -> string
val binding_scope : binding -> scope
val binding_next_mode : binding -> string option
val binding_provider : binding -> Zenbu_kernel.Provider.t

val bindings_conflict : binding -> binding -> bool
(** Two bindings conflict when their scopes are equal and either input sequence
    is a prefix of the other. This excludes an ambiguous command/prefix entry in
    one effective keymap. *)

val hook :
  event:event ->
  provider:Zenbu_kernel.Provider.t ->
  run:(Editor_context.t -> (Model_effect.t list, Zenbu_kernel.Error.t) result) ->
  hook

val hook_event : hook -> event
val hook_provider : hook -> Zenbu_kernel.Provider.t

val run_hook :
  hook -> Editor_context.t -> (Model_effect.t list, Zenbu_kernel.Error.t) result
