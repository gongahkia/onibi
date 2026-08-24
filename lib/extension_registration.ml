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

type binding_layer = {
  id : string;
  title : string;
  description : string;
  priority : int;
  provider : Zenbu_kernel.Provider.t;
}

type binding = {
  inputs : Input_event.binding_pattern list;
  command : string;
  scope : scope;
  layer : string option;
  mode_transition : mode_transition option;
  text_argument : string option;
  provider : Zenbu_kernel.Provider.t;
}

type hook = {
  event : event;
  provider : Zenbu_kernel.Provider.t;
  run : Editor_context.t -> (Model_effect.t list, Zenbu_kernel.Error.t) result;
}

let binding ~input ~command ~scope ~mode_transition ~text_argument ~provider =
  {
    inputs = [ input ];
    command;
    scope;
    layer = None;
    mode_transition;
    text_argument;
    provider;
  }

let binding_in_layer ~layer ~input ~command ~scope ~mode_transition
    ~text_argument ~provider =
  {
    inputs = [ input ];
    command;
    scope;
    layer;
    mode_transition;
    text_argument;
    provider;
  }

let binding_sequence ~head ~tail ~command ~scope ~mode_transition
    ~text_argument ~provider =
  {
    inputs = head :: tail;
    command;
    scope;
    layer = None;
    mode_transition;
    text_argument;
    provider;
  }

let binding_sequence_in_layer ~layer ~head ~tail ~command ~scope
    ~mode_transition ~text_argument ~provider =
  {
    inputs = head :: tail;
    command;
    scope;
    layer;
    mode_transition;
    text_argument;
    provider;
  }

let create_binding_layer ~id ~title ~description ~priority ~provider =
  { id; title; description; priority; provider }

let binding_input (value : binding) = List.hd value.inputs
let binding_inputs (value : binding) = value.inputs
let binding_command (value : binding) = value.command
let binding_scope (value : binding) = value.scope
let binding_layer (value : binding) = value.layer
let binding_mode_transition (value : binding) = value.mode_transition
let binding_text_argument (value : binding) = value.text_argument
let binding_provider (value : binding) = value.provider
let binding_layer_id (value : binding_layer) = value.id
let binding_layer_title (value : binding_layer) = value.title
let binding_layer_description (value : binding_layer) = value.description
let binding_layer_priority (value : binding_layer) = value.priority
let binding_layer_provider (value : binding_layer) = value.provider

let rec sequence_is_prefix prefix sequence =
  match (prefix, sequence) with
  | [], _ -> true
  | _, [] -> false
  | left :: left_rest, right :: right_rest ->
      Input_event.binding_patterns_overlap left right
      && sequence_is_prefix left_rest right_rest

let bindings_conflict left right =
  left.scope = right.scope
  && left.layer = right.layer
  && (sequence_is_prefix left.inputs right.inputs
     || sequence_is_prefix right.inputs left.inputs)

let hook ~event ~provider ~run = { event; provider; run }
let hook_event (value : hook) = value.event
let hook_provider (value : hook) = value.provider
let run_hook (value : hook) = value.run
