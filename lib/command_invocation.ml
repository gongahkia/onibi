open Zenbu_kernel

type t = { id : Command_id.t; arguments : Command_argument.t list }

let create ~id ~arguments =
  let names = List.map Command_argument.name arguments in
  if List.length names <> List.length (List.sort_uniq String.compare names) then
    Error (Error.Invalid_command_arguments "argument names must be unique")
  else Ok { id; arguments }

let id value = value.id
let arguments value = value.arguments

let find value ~name =
  match
    List.find_opt
      (fun argument -> String.equal (Command_argument.name argument) name)
      value.arguments
  with
  | Some argument -> Ok (Command_argument.value argument)
  | None -> Error (Error.Invalid_command_arguments ("missing argument " ^ name))
