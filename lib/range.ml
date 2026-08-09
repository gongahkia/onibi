type t = { start : Anchor.t; stop : Anchor.t }

let make ~start ~stop =
  if not (Document_id.equal (Anchor.document_id start) (Anchor.document_id stop)) then
    Error (Error.Invalid_range "endpoints name different documents")
  else if not (Document_version.equal (Anchor.version start) (Anchor.version stop)) then
    Error (Error.Invalid_range "endpoints name different document versions")
  else if Anchor.byte_offset start > Anchor.byte_offset stop then
    Error (Error.Invalid_range "start offset is after stop offset")
  else Ok { start; stop }

let start value = value.start
let stop value = value.stop
let document_id value = Anchor.document_id value.start
let version value = Anchor.version value.start
let is_empty value = Anchor.byte_offset value.start = Anchor.byte_offset value.stop

let compare left right =
  match Int.compare (Anchor.byte_offset left.start) (Anchor.byte_offset right.start) with
  | 0 -> Int.compare (Anchor.byte_offset left.stop) (Anchor.byte_offset right.stop)
  | difference -> difference

