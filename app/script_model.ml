module Scripting = Zenbu_scripting.Scripting
module Model = Zenbu_model_api.Editing_model

type state = Scripting.model_state

let configured_model : Scripting.model option ref = ref None
let configure model = configured_model := Some model
let configure_state state = configure (Scripting.model_state_model state)
let clear () = configured_model := None

let require_configured_model () =
  match !configured_model with
  | Some model -> model
  | None -> failwith "no validated script editing model is configured"

let descriptor =
  Model.descriptor ~id:"zenbu.script" ~title:"Script editing model"
    ~description:"Checked Lua-defined editing grammar" ()
  |> Result.get_ok

let descriptor_of_state = Scripting.model_state_descriptor
let initialize _ = require_configured_model () |> Scripting.initial_model_state
let reset state _ = Scripting.reset_model_state state
let status = Scripting.model_state_status
let input_rules _ = []

let handle_input state input context =
  match Scripting.run_model state input context with
  | Ok result -> result
  | Error error -> failwith (Zenbu_kernel.Error.to_string error)
