type t = {
  selections : Selection.t list;
  primary_index : int;
  document_id : Document_id.t;
  version : Document_version.t;
}

let range_start selection = Anchor.byte_offset (Range.start (Selection.range selection))
let range_stop selection = Anchor.byte_offset (Range.stop (Selection.range selection))

let rec nth index = function
  | [] -> invalid_arg "Selection_set.nth"
  | value :: _ when index = 0 -> value
  | _ :: rest -> nth (index - 1) rest

let create ~primary selections =
  let count = List.length selections in
  if count = 0 then Error (Error.Invalid_selection_set "a selection set cannot be empty")
  else if primary < 0 || primary >= count then
    Error (Error.Invalid_selection_set "primary selection index is out of bounds")
  else
    let primary_selection = nth primary selections in
    let document_id = Selection.document_id primary_selection in
    let version = Selection.version primary_selection in
    if
      List.exists
        (fun selection ->
          not (Document_id.equal document_id (Selection.document_id selection))
          || not (Document_version.equal version (Selection.version selection)))
        selections
    then Error (Error.Invalid_selection_set "all selections must share a snapshot")
    else
      let sorted = List.sort Selection.compare selections in
      let rec validate previous = function
        | [] -> Ok ()
        | selection :: rest -> (
            match previous with
            | None -> validate (Some selection) rest
            | Some preceding ->
                if Selection.equal preceding selection then
                  Error (Error.Invalid_selection_set "duplicate selection")
                else
                  let previous_stop = range_stop preceding in
                  let next_start = range_start selection in
                  let previous_empty = previous_stop = range_start preceding in
                  let next_empty = range_stop selection = next_start in
                  if previous_stop > next_start && not (previous_empty || next_empty) then
                    Error (Error.Invalid_selection_set "overlapping selection ranges")
                  else validate (Some selection) rest)
      in
      match validate None sorted with
      | Error _ as error -> error
      | Ok () ->
          let rec find index = function
            | [] -> Error (Error.Invalid_selection_set "primary selection was lost")
            | selection :: _ when Selection.equal selection primary_selection -> Ok index
            | _ :: rest -> find (index + 1) rest
          in
          (match find 0 sorted with
          | Error _ as error -> error
          | Ok primary_index -> Ok { selections = sorted; primary_index; document_id; version })

let to_list value = value.selections
let primary value = nth value.primary_index value.selections
let primary_index value = value.primary_index
let document_id value = value.document_id
let version value = value.version

let map_anchors value ~f =
  let mapped = List.map (fun selection -> Selection.map_anchors selection ~f) value.selections in
  let rec collect = function
    | [] -> Ok []
    | Error error :: _ -> Error error
    | Ok selection :: rest -> (
        match collect rest with
        | Error _ as error -> error
        | Ok selections -> Ok (selection :: selections))
  in
  match collect mapped with
  | Error _ as error -> error
  | Ok selections ->
      let mapped_primary = nth value.primary_index selections in
      let sorted = List.sort Selection.compare selections in
      let rec deduplicate previous = function
        | [] -> []
        | selection :: rest ->
            if Option.fold ~none:false ~some:(Selection.equal selection) previous then
              deduplicate previous rest
            else selection :: deduplicate (Some selection) rest
      in
      let normalized = deduplicate None sorted in
      let rec index_of index = function
        | [] -> Error (Error.Invalid_selection_set "mapped primary selection was lost")
        | selection :: _ when Selection.equal selection mapped_primary -> Ok index
        | _ :: rest -> index_of (index + 1) rest
      in
      (match index_of 0 normalized with
      | Error _ as error -> error
      | Ok primary -> create ~primary normalized)
