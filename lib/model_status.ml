open Zenbu_kernel

type input_mode = Key_commands | Text_entry

type t = {
  id : string;
  label : string;
  description : string option;
  pending_input : string option;
  metadata : (string * string) list;
  input_mode : input_mode;
}

let create ~id ~label ?description ?pending_input ?(metadata = [])
    ?(input_mode = Key_commands) () =
  if String.length id = 0 || String.length label = 0 then
    Error (Error.Invalid_model_status "id and label must not be empty")
  else
    let keys = List.map fst metadata in
    if List.length keys <> List.length (List.sort_uniq String.compare keys) then
      Error (Error.Invalid_model_status "metadata keys must be unique")
    else Ok { id; label; description; pending_input; metadata; input_mode }

let id value = value.id
let label value = value.label
let description value = value.description
let pending_input value = value.pending_input
let metadata value = value.metadata
let input_mode value = value.input_mode
