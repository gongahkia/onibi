type source = User | Test | Replay | System

type metadata = {
  source : source;
  intent : string option;
  description : string option;
}

type indexed_edit = { edit : Edit.t; ordinal : int }

type t = {
  document_id : Document_id.t;
  source_version : Document_version.t;
  edits : indexed_edit list;
  selection_change : Selection_set.t option;
  metadata : metadata;
}

let metadata ~source ?intent ?description () = { source; intent; description }
let source value = value.source
let intent value = value.intent
let description value = value.description

let source_to_string = function
  | User -> "user"
  | Test -> "test"
  | Replay -> "replay"
  | System -> "system"

let source_of_string = function
  | "user" -> Ok User
  | "test" -> Ok Test
  | "replay" -> Ok Replay
  | "system" -> Ok System
  | value ->
      Error (Error.Malformed_replay ("unknown transaction source " ^ value))

let start_offset edit = Anchor.byte_offset (Range.start (Edit.range edit))
let stop_offset edit = Anchor.byte_offset (Range.stop (Edit.range edit))

let compare_indexed left right =
  match Int.compare (start_offset left.edit) (start_offset right.edit) with
  | 0 -> (
      let left_empty = Edit.is_insertion left.edit in
      let right_empty = Edit.is_insertion right.edit in
      if left_empty && not right_empty then -1
      else if right_empty && not left_empty then 1
      else if left_empty then Int.compare left.ordinal right.ordinal
      else
        match Int.compare (stop_offset left.edit) (stop_offset right.edit) with
        | 0 -> Int.compare left.ordinal right.ordinal
        | difference -> difference)
  | difference -> difference

let same_snapshot ~document_id ~source_version edit =
  Document_id.equal document_id (Range.document_id (Edit.range edit))
  && Document_version.equal source_version (Range.version (Edit.range edit))

let edits_conflict left right =
  let left_start = start_offset left.edit in
  let left_stop = stop_offset left.edit in
  let right_start = start_offset right.edit in
  let right_stop = stop_offset right.edit in
  let left_empty = left_start = left_stop in
  let right_empty = right_start = right_stop in
  if left_empty && right_empty then false
  else if left_empty then right_start < left_start && left_start < right_stop
  else if right_empty then left_start < right_start && right_start < left_stop
  else left_start < right_stop && right_start < left_stop

let validate_conflicts edits =
  let rec outer = function
    | [] -> Ok ()
    | edit :: rest -> (
        let rec inner = function
          | [] -> Ok ()
          | other :: tail ->
              if edits_conflict edit other then
                Error
                  (Error.Overlapping_edits
                     {
                       first_index = edit.ordinal;
                       second_index = other.ordinal;
                     })
              else inner tail
        in
        match inner rest with Error _ as error -> error | Ok () -> outer rest)
  in
  outer edits

let create ~document_id ~source_version ~edits ?selection_change ~metadata () =
  if
    List.exists
      (fun edit -> not (same_snapshot ~document_id ~source_version edit))
      edits
  then
    Error
      (Error.Invalid_range
         "an edit does not belong to the transaction source snapshot")
  else
    match selection_change with
    | Some selections
      when not
             (Document_id.equal document_id
                (Selection_set.document_id selections)) ->
        Error
          (Error.Invalid_selection_set "selection change names another document")
    | Some selections
      when not
             (Document_version.equal source_version
                (Selection_set.version selections)) ->
        Error
          (Error.Invalid_selection_set
             "selection change names another document version")
    | _ when edits = [] && Option.is_none selection_change ->
        Error Error.Empty_transaction
    | _ -> (
        let indexed = List.mapi (fun ordinal edit -> { edit; ordinal }) edits in
        match validate_conflicts indexed with
        | Error _ as error -> error
        | Ok () ->
            Ok
              {
                document_id;
                source_version;
                edits = List.sort compare_indexed indexed;
                selection_change;
                metadata;
              })

let document_id value = value.document_id
let source_version value = value.source_version
let edits value = List.map (fun indexed -> indexed.edit) value.edits
let selection_change value = value.selection_change
let metadata_of value = value.metadata
