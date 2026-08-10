open Zenbu_kernel

module Command_map = Map.Make (struct
  type t = Command_id.t

  let compare = Command_id.compare
end)

type t = Command.t Command_map.t

let empty = Command_map.empty

let register registry command =
  let id = Command_descriptor.id (Command.descriptor command) in
  if Command_map.mem id registry then
    Error (Error.Duplicate_command (Command_id.to_string id))
  else Ok (Command_map.add id command registry)

let find registry id =
  match Command_map.find_opt id registry with
  | Some command -> Ok command
  | None -> Error (Error.Unknown_command (Command_id.to_string id))

let descriptors registry =
  registry |> Command_map.bindings
  |> List.map (fun (_, command) -> Command.descriptor command)

let invoke registry ~context invocation =
  match find registry (Command_invocation.id invocation) with
  | Error _ as error -> error
  | Ok command -> Command.execute command context invocation

let invoke_effects registry ~context invocation =
  match find registry (Command_invocation.id invocation) with
  | Error _ as error -> error
  | Ok command -> Command.execute_effects command context invocation
