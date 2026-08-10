type t =
  | Select
  | Delete
  | Replace_text of string
  | Collapse_to_start
  | Collapse_to_end

let name = function
  | Select -> "select"
  | Delete -> "delete"
  | Replace_text _ -> "replace-text"
  | Collapse_to_start -> "collapse-to-start"
  | Collapse_to_end -> "collapse-to-end"

let descriptors () =
  let provider =
    Provider.create ~id:"zenbu.kernel" ~kind:Provider.Builtin
    |> Result.get_ok
  in
  let declare value title description =
    Semantic_descriptor.create ~id:(name value) ~title ~description ~provider
      ~kind:Semantic_descriptor.Transformation ()
    |> Result.get_ok
  in
  [
    declare Select "Select" "Updates selections without changing document text.";
    declare Delete "Delete"
      "Removes resolved ranges through one atomic transaction.";
    declare (Replace_text "") "Replace text"
      "Replaces resolved ranges through one atomic transaction.";
    declare Collapse_to_start "Collapse to start"
      "Collapses each resolved selection to its start.";
    declare Collapse_to_end "Collapse to end"
      "Collapses each resolved selection to its end.";
  ]
