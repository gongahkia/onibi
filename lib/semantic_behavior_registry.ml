open Zenbu_kernel
module Id_map = Map.Make (String)

type t = {
  selectors : Semantic_behavior.selector_entry Id_map.t;
  transformations : Semantic_behavior.transformation_entry Id_map.t;
}

let empty = { selectors = Id_map.empty; transformations = Id_map.empty }

let occupied registry id =
  Id_map.mem id registry.selectors || Id_map.mem id registry.transformations

let register_selector registry entry =
  let descriptor = Semantic_behavior.selector_descriptor entry in
  let id = Semantic_descriptor.id descriptor in
  if Semantic_descriptor.kind descriptor <> Semantic_descriptor.Selector then
    Error
      (Error.Invalid_provenance "selector behavior has wrong descriptor kind")
  else if occupied registry id then Error (Error.Duplicate_descriptor id)
  else Ok { registry with selectors = Id_map.add id entry registry.selectors }

let register_transformation registry entry =
  let descriptor = Semantic_behavior.transformation_descriptor entry in
  let id = Semantic_descriptor.id descriptor in
  if Semantic_descriptor.kind descriptor <> Semantic_descriptor.Transformation
  then
    Error
      (Error.Invalid_provenance
         "transformation behavior has wrong descriptor kind")
  else if occupied registry id then Error (Error.Duplicate_descriptor id)
  else
    Ok
      {
        registry with
        transformations = Id_map.add id entry registry.transformations;
      }

let find_selector registry id = Id_map.find_opt id registry.selectors

let find_transformation registry id =
  Id_map.find_opt id registry.transformations

let descriptors registry =
  let selectors =
    Id_map.bindings registry.selectors
    |> List.map (fun (_, entry) -> Semantic_behavior.selector_descriptor entry)
  in
  let transformations =
    Id_map.bindings registry.transformations
    |> List.map (fun (_, entry) ->
        Semantic_behavior.transformation_descriptor entry)
  in
  selectors @ transformations

let merge left right =
  let add_selector result (_, entry) =
    Result.bind result (fun registry -> register_selector registry entry)
  in
  let add_transformation result (_, entry) =
    Result.bind result (fun registry -> register_transformation registry entry)
  in
  let selectors =
    Id_map.bindings right.selectors |> List.fold_left add_selector (Ok left)
  in
  Result.bind selectors (fun registry ->
      Id_map.bindings right.transformations
      |> List.fold_left add_transformation (Ok registry))
