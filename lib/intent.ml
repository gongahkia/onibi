type t =
  | Insert_text of string
  | Delete_selected_ranges
  | Replace_selected_ranges of string
  | Set_selections of { selections : Selection_spec.t list; primary : int }

let identity = function
  | Insert_text _ -> "insert-text"
  | Delete_selected_ranges -> "delete-selected-ranges"
  | Replace_selected_ranges _ -> "replace-selected-ranges"
  | Set_selections _ -> "set-selections"

let collect results =
  let rec loop values = function
    | [] -> Ok (List.rev values)
    | Ok value :: rest -> loop (value :: values) rest
    | Error error :: _ -> Error error
  in
  loop [] results

let edits_for_selections snapshot ~text =
  let edits =
    List.map
      (fun selection -> Edit.replace (Selection.range selection) ~text)
      (Selection_set.to_list (Document_snapshot.selections snapshot))
  in
  collect edits

let selection_set_of_specs snapshot ~selections ~primary =
  let rec make_selections values = function
    | [] -> Ok (List.rev values)
    | spec :: rest -> (
        match
          Document_snapshot.anchor snapshot
            ~byte_offset:(Selection_spec.anchor_offset spec)
        with
        | Error _ as error -> error
        | Ok anchor -> (
            match
              Document_snapshot.anchor snapshot
                ~byte_offset:(Selection_spec.head_offset spec)
            with
            | Error _ as error -> error
            | Ok head -> (
                match Selection.make ~anchor ~head with
                | Error _ as error -> error
                | Ok selection -> make_selections (selection :: values) rest)))
  in
  match make_selections [] selections with
  | Error _ as error -> error
  | Ok selections -> Selection_set.create ~primary selections

let transaction snapshot ~edits ~selection_change ~source ~intent ~description =
  let metadata = Transaction.metadata ~source ~intent ?description () in
  Transaction.create
    ~document_id:(Document_snapshot.document_id snapshot)
    ~source_version:(Document_snapshot.version snapshot)
    ~edits ?selection_change ~metadata ()

let resolve ~source ?description snapshot = function
  | Insert_text text -> (
      match edits_for_selections snapshot ~text with
      | Error _ as error -> error
      | Ok edits ->
          transaction snapshot ~edits ~selection_change:None ~source
            ~intent:"insert-text" ~description)
  | Delete_selected_ranges ->
      let edits =
        List.map
          (fun selection -> Edit.delete (Selection.range selection))
          (Selection_set.to_list (Document_snapshot.selections snapshot))
      in
      transaction snapshot ~edits ~selection_change:None ~source
        ~intent:"delete-selected-ranges" ~description
  | Replace_selected_ranges text -> (
      match edits_for_selections snapshot ~text with
      | Error _ as error -> error
      | Ok edits ->
          transaction snapshot ~edits ~selection_change:None ~source
            ~intent:"replace-selected-ranges" ~description)
  | Set_selections { selections; primary } -> (
      match selection_set_of_specs snapshot ~selections ~primary with
      | Error _ as error -> error
      | Ok selection_change ->
          transaction snapshot ~edits:[]
            ~selection_change:(Some selection_change) ~source
            ~intent:"set-selections" ~description)
