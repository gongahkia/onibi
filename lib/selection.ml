type t = { anchor : Anchor.t; head : Anchor.t; range : Range.t }

let make ~anchor ~head =
  if
    not
      (Document_id.equal (Anchor.document_id anchor) (Anchor.document_id head))
  then
    Error
      (Error.Invalid_selection_set "anchor and head name different documents")
  else if
    not (Document_version.equal (Anchor.version anchor) (Anchor.version head))
  then
    Error
      (Error.Invalid_selection_set
         "anchor and head name different document versions")
  else
    let start, stop =
      if Anchor.byte_offset anchor <= Anchor.byte_offset head then (anchor, head)
      else (head, anchor)
    in
    match Range.make ~start ~stop with
    | Error _ as error -> error
    | Ok range -> Ok { anchor; head; range }

let anchor value = value.anchor
let head value = value.head
let range value = value.range
let document_id value = Anchor.document_id value.anchor
let version value = Anchor.version value.anchor

let equal left right =
  Anchor.equal left.anchor right.anchor && Anchor.equal left.head right.head

let compare left right =
  match Range.compare left.range right.range with
  | 0 -> (
      match Anchor.compare left.anchor right.anchor with
      | 0 -> Anchor.compare left.head right.head
      | difference -> difference)
  | difference -> difference

let map_anchors value ~f = make ~anchor:(f value.anchor) ~head:(f value.head)
