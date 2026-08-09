type t = {
  document_id : Document_id.t;
  version : Document_version.t;
  byte_offset : int;
}

let make ~document_id ~version ~byte_offset =
  if byte_offset < 0 then
    Error
      (Error.Invalid_anchor
         { offset = byte_offset; byte_length = 0; reason = "offset must not be negative" })
  else Ok { document_id; version; byte_offset }

let document_id value = value.document_id
let version value = value.version
let byte_offset value = value.byte_offset

let rebase value ~version ~byte_offset = { value with version; byte_offset }

let equal left right =
  Document_id.equal left.document_id right.document_id
  && Document_version.equal left.version right.version
  && left.byte_offset = right.byte_offset

let compare left right =
  match Document_id.compare left.document_id right.document_id with
  | 0 -> (
      match Document_version.compare left.version right.version with
      | 0 -> Int.compare left.byte_offset right.byte_offset
      | difference -> difference)
  | difference -> difference

