type t = {
  id : Document_id.t;
  version : Document_version.t;
  buffer : Text_buffer.t;
  selections : Selection_set.t;
}

let id value = value.id
let version value = value.version

let snapshot value =
  match
    Document_snapshot.make ~document_id:value.id ~version:value.version ~buffer:value.buffer
      ~selections:value.selections
  with
  | Ok snapshot -> snapshot
  | Error error -> failwith ("document invariant violated: " ^ Error.to_string error)

let selection_set_of_specs ~id ~version ~buffer ~specs ~primary =
  let rec make_selections = function
    | [] -> Ok []
    | spec :: rest -> (
        match Anchor.make ~document_id:id ~version ~byte_offset:(Selection_spec.anchor_offset spec) with
        | Error _ as error -> error
        | Ok anchor -> (
            match Anchor.make ~document_id:id ~version ~byte_offset:(Selection_spec.head_offset spec) with
            | Error _ as error -> error
            | Ok head -> (
                match Selection.make ~anchor ~head with
                | Error _ as error -> error
                | Ok selection -> (
                    match make_selections rest with
                    | Error _ as error -> error
                    | Ok selections -> Ok (selection :: selections)))))
  in
  match make_selections specs with
  | Error _ as error -> error
  | Ok selections -> (
      match Selection_set.create ~primary selections with
      | Error _ as error -> error
      | Ok selection_set ->
          let provisional = { id; version; buffer; selections = selection_set } in
          (match Document_snapshot.validate_selection_set (snapshot provisional) selection_set with
          | Error _ as error -> error
          | Ok () -> Ok selection_set))

let create ~id ~contents ?(initial_selections = []) ?(primary = 0) () =
  match Text_buffer.of_utf8 contents with
  | Error _ as error -> error
  | Ok buffer ->
      let initial_selections =
        if initial_selections = [] then
          match Selection_spec.make ~anchor_offset:0 ~head_offset:0 with
          | Ok selection -> [ selection ]
          | Error error -> failwith ("impossible default selection: " ^ Error.to_string error)
        else initial_selections
      in
      (match
         selection_set_of_specs ~id ~version:Document_version.initial ~buffer
           ~specs:initial_selections ~primary
       with
      | Error _ as error -> error
      | Ok selections -> Ok { id; version = Document_version.initial; buffer; selections })

let validate_transaction value transaction =
  if not (Document_id.equal value.id (Transaction.document_id transaction)) then
    Error
      (Error.Wrong_document
         {
           expected = Document_id.to_string value.id;
           actual = Document_id.to_string (Transaction.document_id transaction);
         })
  else if not (Document_version.equal value.version (Transaction.source_version transaction)) then
    Error
      (Error.Stale_version
         {
           expected = Document_version.to_int value.version;
           actual = Document_version.to_int (Transaction.source_version transaction);
         })
  else
    let source_snapshot = snapshot value in
    let rec validate_edits = function
      | [] -> Ok ()
      | edit :: rest -> (
          match Document_snapshot.validate_range source_snapshot (Edit.range edit) with
          | Error _ as error -> error
          | Ok () -> validate_edits rest)
    in
    match validate_edits (Transaction.edits transaction) with
    | Error _ as error -> error
    | Ok () -> (
        match Transaction.selection_change transaction with
        | None -> Ok source_snapshot
        | Some selections -> (
            match Document_snapshot.validate_selection_set source_snapshot selections with
            | Error _ as error -> error
            | Ok () -> Ok source_snapshot))

let start_offset edit = Anchor.byte_offset (Range.start (Edit.range edit))
let stop_offset edit = Anchor.byte_offset (Range.stop (Edit.range edit))

let apply_edits buffer edits =
  let source = Text_buffer.contents buffer in
  let output = Buffer.create (String.length source) in
  let rec append cursor = function
    | [] ->
        Buffer.add_substring output source cursor (String.length source - cursor);
        Text_buffer.of_utf8 (Buffer.contents output)
    | edit :: rest ->
        let start = start_offset edit in
        let stop = stop_offset edit in
        Buffer.add_substring output source cursor (start - cursor);
        Buffer.add_string output (Edit.text edit);
        append stop rest
  in
  append 0 edits

let transform_offset edits offset =
  let delta_before point =
    List.fold_left
      (fun delta edit ->
        let start = start_offset edit in
        let stop = stop_offset edit in
        if stop <= point && start <> stop then
          delta + String.length (Edit.text edit) - (stop - start)
        else if start < point && start = stop then delta + String.length (Edit.text edit)
        else delta)
      0 edits
  in
  let insertions_at point =
    List.fold_left
      (fun length edit ->
        if start_offset edit = point && Edit.is_insertion edit then length + String.length (Edit.text edit)
        else length)
      0 edits
  in
  let active_range =
    List.find_opt
      (fun edit ->
        let start = start_offset edit in
        start <= offset && offset < stop_offset edit)
      edits
  in
  match active_range with
  | Some edit ->
      let start = start_offset edit in
      start + delta_before start + insertions_at start + String.length (Edit.text edit)
  | None -> offset + delta_before offset + insertions_at offset

let rebase_selections ~version ~edits selections =
  Selection_set.map_anchors selections ~f:(fun anchor ->
      Anchor.rebase anchor ~version ~byte_offset:(transform_offset edits (Anchor.byte_offset anchor)))

let apply ?result_version value transaction =
  match validate_transaction value transaction with
  | Error _ as error -> error
  | Ok _ ->
      let target_version =
        match result_version with Some version -> version | None -> Document_version.successor value.version
      in
      if Document_version.compare target_version value.version <= 0 then
        Error
          (Error.Invalid_target_version
             {
               source = Document_version.to_int value.version;
               target = Document_version.to_int target_version;
             })
      else
        let edits = Transaction.edits transaction in
        match apply_edits value.buffer edits with
        | Error _ as error -> error
        | Ok buffer ->
            let source_selections =
              match Transaction.selection_change transaction with
              | Some selections -> selections
              | None -> value.selections
            in
            (match rebase_selections ~version:target_version ~edits source_selections with
            | Error _ as error -> error
            | Ok selections -> Ok { id = value.id; version = target_version; buffer; selections })
