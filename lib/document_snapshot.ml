type t = {
  document_id : Document_id.t;
  version : Document_version.t;
  buffer : Text_buffer.t;
  selections : Selection_set.t;
}

let document_id value = value.document_id
let version value = value.version
let contents value = Text_buffer.contents value.buffer
let byte_length value = Text_buffer.byte_length value.buffer
let selections value = value.selections

let validate_anchor value anchor =
  if not (Document_id.equal value.document_id (Anchor.document_id anchor)) then
    Error
      (Error.Wrong_document
         {
           expected = Document_id.to_string value.document_id;
           actual = Document_id.to_string (Anchor.document_id anchor);
         })
  else if not (Document_version.equal value.version (Anchor.version anchor))
  then
    Error
      (Error.Stale_version
         {
           expected = Document_version.to_int value.version;
           actual = Document_version.to_int (Anchor.version anchor);
         })
  else
    let offset = Anchor.byte_offset anchor in
    let byte_length = Text_buffer.byte_length value.buffer in
    if offset < 0 || offset > byte_length then
      Error
        (Error.Invalid_anchor
           { offset; byte_length; reason = "offset is out of bounds" })
    else if not (Text_buffer.is_code_point_boundary value.buffer offset) then
      Error
        (Error.Invalid_anchor
           { offset; byte_length; reason = "offset splits a UTF-8 code point" })
    else Ok ()

let anchor value ~byte_offset =
  match
    Anchor.make ~document_id:value.document_id ~version:value.version
      ~byte_offset
  with
  | Error _ as error -> error
  | Ok anchor -> (
      match validate_anchor value anchor with
      | Error _ as error -> error
      | Ok () -> Ok anchor)

let validate_range value range =
  match validate_anchor value (Range.start range) with
  | Error _ as error -> error
  | Ok () -> validate_anchor value (Range.stop range)

let range value ~start_offset ~stop_offset =
  match anchor value ~byte_offset:start_offset with
  | Error _ as error -> error
  | Ok start -> (
      match anchor value ~byte_offset:stop_offset with
      | Error _ as error -> error
      | Ok stop -> Range.make ~start ~stop)

let validate_selection_set value selections =
  if
    not
      (Document_id.equal value.document_id
         (Selection_set.document_id selections))
  then
    Error
      (Error.Wrong_document
         {
           expected = Document_id.to_string value.document_id;
           actual = Document_id.to_string (Selection_set.document_id selections);
         })
  else if
    not
      (Document_version.equal value.version (Selection_set.version selections))
  then
    Error
      (Error.Stale_version
         {
           expected = Document_version.to_int value.version;
           actual = Document_version.to_int (Selection_set.version selections);
         })
  else
    let rec validate = function
      | [] -> Ok ()
      | selection :: rest -> (
          match validate_anchor value (Selection.anchor selection) with
          | Error _ as error -> error
          | Ok () -> (
              match validate_anchor value (Selection.head selection) with
              | Error _ as error -> error
              | Ok () -> validate rest))
    in
    validate (Selection_set.to_list selections)

let make ~document_id ~version ~buffer ~selections =
  let snapshot = { document_id; version; buffer; selections } in
  match validate_selection_set snapshot selections with
  | Error _ as error -> error
  | Ok () -> Ok snapshot
