type entry =
  | Model of { id : string; provider : Provider.t }
  | Input of string
  | Interaction of int
  | Effect of string
  | Command of { id : string; provider : Provider.t }
  | Binding of { input : string; command : string; provider : Provider.t }
  | Event of { name : string; provider : Provider.t }
  | Selector of string
  | Transformation of string
  | Repeat of string

type t = { execution_id : int; entries : entry list }

let create ~execution_id ~model_id ~provider ~input =
  { execution_id; entries = [ Model { id = model_id; provider }; Input input ] }

let execution_id value = value.execution_id
let entries value = value.entries
let add value entry = { value with entries = value.entries @ [ entry ] }

let entry_name = function
  | Model { id; provider } -> "model " ^ id ^ " (" ^ Provider.describe provider ^ ")"
  | Input input -> "input " ^ input
  | Interaction id -> "interaction " ^ string_of_int id
  | Effect id -> "effect " ^ id
  | Command { id; provider } -> "command " ^ id ^ " (" ^ Provider.describe provider ^ ")"
  | Binding { input; command; provider } ->
      "binding " ^ input ^ " -> " ^ command ^ " (" ^ Provider.describe provider ^ ")"
  | Event { name; provider } -> "event " ^ name ^ " (" ^ Provider.describe provider ^ ")"
  | Selector id -> "selector " ^ id
  | Transformation id -> "transformation " ^ id
  | Repeat description -> "repeat " ^ description
