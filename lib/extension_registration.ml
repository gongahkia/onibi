type event = Document_changed | After_save

type scope =
  | Global
  | Model of string
  | Model_status of { model : string; status : string }

type binding = {
  input : Input_event.t;
  command : string;
  scope : scope;
  provider : Zenbu_kernel.Provider.t;
}

type hook = {
  event : event;
  provider : Zenbu_kernel.Provider.t;
  run : Editor_context.t -> (Model_effect.t list, Zenbu_kernel.Error.t) result;
}

let binding ~input ~command ~scope ~provider =
  { input; command; scope; provider }

let binding_input (value : binding) = value.input
let binding_command (value : binding) = value.command
let binding_scope (value : binding) = value.scope
let binding_provider (value : binding) = value.provider
let hook ~event ~provider ~run = { event; provider; run }
let hook_event (value : hook) = value.event
let hook_provider (value : hook) = value.provider
let run_hook (value : hook) = value.run
