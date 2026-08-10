module String_map = Map.Make (String)

type t = Zenbu_kernel.Semantic_descriptor.t String_map.t

let empty = String_map.empty

let register registry descriptor =
  let id = Zenbu_kernel.Semantic_descriptor.id descriptor in
  if String_map.mem id registry then
    Error (Zenbu_kernel.Error.Duplicate_descriptor id)
  else Ok (String_map.add id descriptor registry)

let find registry id = String_map.find_opt id registry

let descriptors registry = String_map.bindings registry |> List.map snd
