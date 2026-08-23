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

type binding = {
  inputs : Input_event.t list;
  command : string;
  scope : scope;
  mode_transition : mode_transition option;
  provider : Zenbu_kernel.Provider.t;
}

type hook = {
  event : event;
  provider : Zenbu_kernel.Provider.t;
  run : Editor_context.t -> (Model_effect.t list, Zenbu_kernel.Error.t) result;
}

let binding ~input ~command ~scope ~mode_transition ~provider =
  { inputs = [ input ]; command; scope; mode_transition; provider }

let binding_sequence ~head ~tail ~command ~scope ~mode_transition ~provider =
  { inputs = head :: tail; command; scope; mode_transition; provider }

let binding_input (value : binding) = List.hd value.inputs
let binding_inputs (value : binding) = value.inputs
let binding_command (value : binding) = value.command
let binding_scope (value : binding) = value.scope
let binding_mode_transition (value : binding) = value.mode_transition
let binding_provider (value : binding) = value.provider

let rec sequence_is_prefix prefix sequence =
  match (prefix, sequence) with
  | [], _ -> true
  | _, [] -> false
  | left :: left_rest, right :: right_rest ->
      left = right && sequence_is_prefix left_rest right_rest

let bindings_conflict left right =
  left.scope = right.scope
  && (sequence_is_prefix left.inputs right.inputs
     || sequence_is_prefix right.inputs left.inputs)

let hook ~event ~provider ~run = { event; provider; run }
let hook_event (value : hook) = value.event
let hook_provider (value : hook) = value.provider
let run_hook (value : hook) = value.run
