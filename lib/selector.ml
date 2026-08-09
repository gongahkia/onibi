type t = Current_selections | Document | Next_text_unit | Previous_text_unit

let to_string = function
  | Current_selections -> "current-selections"
  | Document -> "document"
  | Next_text_unit -> "next-text-unit"
  | Previous_text_unit -> "previous-text-unit"

let of_string = function
  | "current-selections" -> Ok Current_selections
  | "document" -> Ok Document
  | "next-text-unit" -> Ok Next_text_unit
  | "previous-text-unit" -> Ok Previous_text_unit
  | value -> Error (Error.Malformed_replay ("unknown selector " ^ value))

let is_continuation text index = Char.code text.[index] land 0xC0 = 0x80

let next_boundary text offset =
  let length = String.length text in
  if offset >= length then
    Error
      (Error.Invalid_selector "next text unit is unavailable at document end")
  else
    let rec find index =
      if index = length || not (is_continuation text index) then index
      else find (index + 1)
    in
    Ok (find (offset + 1))

let previous_boundary text offset =
  if offset <= 0 then
    Error
      (Error.Invalid_selector
         "previous text unit is unavailable at document start")
  else
    let rec find index =
      if not (is_continuation text index) then index else find (index - 1)
    in
    Ok (find (offset - 1))

let selection_from_offsets snapshot ~anchor_offset ~head_offset =
  match Document_snapshot.anchor snapshot ~byte_offset:anchor_offset with
  | Error _ as error -> error
  | Ok anchor -> (
      match Document_snapshot.anchor snapshot ~byte_offset:head_offset with
      | Error _ as error -> error
      | Ok head -> Selection.make ~anchor ~head)

let collect selections =
  let rec loop values = function
    | [] -> Ok (List.rev values)
    | Ok value :: rest -> loop (value :: values) rest
    | Error error :: _ -> Error error
  in
  loop [] selections

let resolve_text_unit snapshot direction =
  let text = Document_snapshot.contents snapshot in
  let existing =
    Selection_set.to_list (Document_snapshot.selections snapshot)
  in
  let selections =
    List.map
      (fun selection ->
        let head = Anchor.byte_offset (Selection.head selection) in
        match direction with
        | `Next -> (
            match next_boundary text head with
            | Error _ as error -> error
            | Ok stop ->
                selection_from_offsets snapshot ~anchor_offset:head
                  ~head_offset:stop)
        | `Previous -> (
            match previous_boundary text head with
            | Error _ as error -> error
            | Ok start ->
                selection_from_offsets snapshot ~anchor_offset:head
                  ~head_offset:start))
      existing
  in
  match collect selections with
  | Error _ as error -> error
  | Ok selections ->
      Selection_set.create
        ~primary:
          (Selection_set.primary_index (Document_snapshot.selections snapshot))
        selections

let resolve snapshot = function
  | Current_selections -> Ok (Document_snapshot.selections snapshot)
  | Document -> (
      match
        selection_from_offsets snapshot ~anchor_offset:0
          ~head_offset:(Document_snapshot.byte_length snapshot)
      with
      | Error _ as error -> error
      | Ok selection -> Selection_set.create ~primary:0 [ selection ])
  | Next_text_unit -> resolve_text_unit snapshot `Next
  | Previous_text_unit -> resolve_text_unit snapshot `Previous
