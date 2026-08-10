type t =
  | Insert_text of string
  | Delete_selected_ranges
  | Replace_selected_ranges of string
  | Set_selections of { selections : Selection_spec.t list; primary : int }
  | Apply of { selector : Selector.t; transformation : Transformation.t }

let identity = function
  | Insert_text _ -> "insert-text"
  | Delete_selected_ranges -> "delete-selected-ranges"
  | Replace_selected_ranges _ -> "replace-selected-ranges"
  | Set_selections _ -> "set-selections"
  | Apply { selector; transformation } ->
      "apply:"
      ^ Selector.to_string selector
      ^ ":"
      ^ Transformation.name transformation

let collect results =
  let rec loop values = function
    | [] -> Ok (List.rev values)
    | Ok value :: rest -> loop (value :: values) rest
    | Error error :: _ -> Error error
  in
  loop [] results

let rec edits_for_selections snapshot ~text =
  edits_for_selection_set (Document_snapshot.selections snapshot) ~text

and edits_for_selection_set selections ~text =
  let edits =
    List.map
      (fun selection -> Edit.replace (Selection.range selection) ~text)
      (Selection_set.to_list selections)
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

let collapse_selections snapshot selections endpoint =
  let collapsed =
    Selection_set.to_list selections
    |> List.map (fun selection ->
        let range = Selection.range selection in
        let offset =
          match endpoint with
          | `Start -> Anchor.byte_offset (Range.start range)
          | `End -> Anchor.byte_offset (Range.stop range)
        in
        match Document_snapshot.anchor snapshot ~byte_offset:offset with
        | Error _ as error -> error
        | Ok anchor -> Selection.make ~anchor ~head:anchor)
  in
  match collect collapsed with
  | Error _ as error -> error
  | Ok collapsed ->
      Selection_set.create
        ~primary:(Selection_set.primary_index selections)
        collapsed

let transaction snapshot ~edits ~selection_change ~source ~intent ~description
    ~provenance =
  let metadata =
    Transaction.metadata ~source ~intent ?description ?provenance ()
  in
  Transaction.create
    ~document_id:(Document_snapshot.document_id snapshot)
    ~source_version:(Document_snapshot.version snapshot)
    ~edits ?selection_change ~metadata ()

let resolve_on_selections ~source ?description ?provenance ~intent snapshot
    selection_change transformation =
  match transformation with
  | Transformation.Select ->
      transaction snapshot ~edits:[] ~selection_change:(Some selection_change)
        ~source ~intent ~description ~provenance
  | Transformation.Collapse_to_start -> (
      match collapse_selections snapshot selection_change `Start with
      | Error _ as error -> error
      | Ok selection_change ->
          transaction snapshot ~edits:[]
            ~selection_change:(Some selection_change) ~source ~intent
            ~description ~provenance)
  | Transformation.Collapse_to_end -> (
      match collapse_selections snapshot selection_change `End with
      | Error _ as error -> error
      | Ok selection_change ->
          transaction snapshot ~edits:[]
            ~selection_change:(Some selection_change) ~source ~intent
            ~description ~provenance)
  | Transformation.Delete -> (
      match edits_for_selection_set selection_change ~text:"" with
      | Error _ as error -> error
      | Ok edits ->
          transaction snapshot ~edits ~selection_change:(Some selection_change)
            ~source ~intent ~description ~provenance)
  | Transformation.Replace_text text -> (
      match edits_for_selection_set selection_change ~text with
      | Error _ as error -> error
      | Ok edits ->
          transaction snapshot ~edits ~selection_change:(Some selection_change)
            ~source ~intent ~description ~provenance)

let resolve ~source ?description ?provenance snapshot = function
  | Insert_text text -> (
      match edits_for_selections snapshot ~text with
      | Error _ as error -> error
      | Ok edits ->
          transaction snapshot ~edits ~selection_change:None ~source
            ~intent:"insert-text" ~description ~provenance)
  | Delete_selected_ranges ->
      let edits =
        List.map
          (fun selection -> Edit.delete (Selection.range selection))
          (Selection_set.to_list (Document_snapshot.selections snapshot))
      in
      transaction snapshot ~edits ~selection_change:None ~source
        ~intent:"delete-selected-ranges" ~description ~provenance
  | Replace_selected_ranges text -> (
      match edits_for_selections snapshot ~text with
      | Error _ as error -> error
      | Ok edits ->
          transaction snapshot ~edits ~selection_change:None ~source
            ~intent:"replace-selected-ranges" ~description ~provenance)
  | Set_selections { selections; primary } -> (
      match selection_set_of_specs snapshot ~selections ~primary with
      | Error _ as error -> error
      | Ok selection_change ->
          transaction snapshot ~edits:[]
            ~selection_change:(Some selection_change) ~source
            ~intent:"set-selections" ~description ~provenance)
  | Apply { selector; transformation } -> (
      match Selector.resolve snapshot selector with
      | Error _ as error -> error
      | Ok selection_change ->
          resolve_on_selections ~source ?description ?provenance
            ~intent:(identity (Apply { selector; transformation }))
            snapshot selection_change transformation)
