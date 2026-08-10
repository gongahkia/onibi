type entry =
  | Model of { id : string; provider : Provider.t }
  | Input of string
  | Effect of string
  | Command of { id : string; provider : Provider.t }
  | Selector of string
  | Transformation of string
  | Repeat of string

type t = { execution_id : int; entries : entry list }

let create ~execution_id ~model_id ~provider ~input =
  {
    execution_id;
    entries = [ Model { id = model_id; provider }; Input input ];
  }

let execution_id value = value.execution_id
let entries value = value.entries
let add value entry = { value with entries = value.entries @ [ entry ] }

let entry_name = function
  | Model { id; _ } -> "model " ^ id
  | Input input -> "input " ^ input
  | Effect id -> "effect " ^ id
  | Command { id; _ } -> "command " ^ id
  | Selector id -> "selector " ^ id
  | Transformation id -> "transformation " ^ id
  | Repeat description -> "repeat " ^ description
