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

type mode_transition =
  | Replace_mode of string
  | Push_mode of string
  | Pop_mode
  | Clear_modes

type binding_layer
type binding
type hook

val create_binding_layer :
  id:string ->
  title:string ->
  description:string ->
  priority:int ->
  provider:Zenbu_kernel.Provider.t ->
  binding_layer
(** A host-owned, data-only binding layer. Higher priorities resolve before
    lower priorities when bindings have the same ordinary scope. *)

val binding_layer_id : binding_layer -> string
val binding_layer_title : binding_layer -> string
val binding_layer_description : binding_layer -> string
val binding_layer_priority : binding_layer -> int
val binding_layer_provider : binding_layer -> Zenbu_kernel.Provider.t

val binding :
  input:Input_event.binding_pattern ->
  command:string ->
  scope:scope ->
  mode_transition:mode_transition option ->
  text_argument:string option ->
  provider:Zenbu_kernel.Provider.t ->
  binding

val binding_in_layer :
  layer:string option ->
  input:Input_event.binding_pattern ->
  command:string ->
  scope:scope ->
  mode_transition:mode_transition option ->
  text_argument:string option ->
  provider:Zenbu_kernel.Provider.t ->
  binding

val binding_sequence :
  head:Input_event.binding_pattern ->
  tail:Input_event.binding_pattern list ->
  command:string ->
  scope:scope ->
  mode_transition:mode_transition option ->
  text_argument:string option ->
  provider:Zenbu_kernel.Provider.t ->
  binding
(** Register a nonempty ordered logical-input sequence. [head] is separate so
    adapter code cannot construct an empty binding. *)

val binding_sequence_in_layer :
  layer:string option ->
  head:Input_event.binding_pattern ->
  tail:Input_event.binding_pattern list ->
  command:string ->
  scope:scope ->
  mode_transition:mode_transition option ->
  text_argument:string option ->
  provider:Zenbu_kernel.Provider.t ->
  binding

val binding_input : binding -> Input_event.binding_pattern
(** Legacy one-event projection. New consumers should inspect [binding_inputs].
*)

val binding_inputs : binding -> Input_event.binding_pattern list
val binding_command : binding -> string
val binding_scope : binding -> scope
val binding_layer : binding -> string option
val binding_mode_transition : binding -> mode_transition option
val binding_text_argument : binding -> string option
val binding_provider : binding -> Zenbu_kernel.Provider.t

val bindings_conflict : binding -> binding -> bool
(** Two bindings conflict when they belong to the same layer and scope and
    either input sequence is a prefix of the other. This excludes an ambiguous
    command/prefix entry in one effective keymap. *)

val hook :
  event:event ->
  provider:Zenbu_kernel.Provider.t ->
  run:(Editor_context.t -> (Model_effect.t list, Zenbu_kernel.Error.t) result) ->
  hook

val hook_event : hook -> event
val hook_provider : hook -> Zenbu_kernel.Provider.t

val run_hook :
  hook -> Editor_context.t -> (Model_effect.t list, Zenbu_kernel.Error.t) result
