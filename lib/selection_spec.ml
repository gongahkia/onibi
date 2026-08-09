type t = { anchor_offset : int; head_offset : int }

let make ~anchor_offset ~head_offset =
  if anchor_offset < 0 || head_offset < 0 then
    Error (Error.Invalid_selection_set "selection offsets must not be negative")
  else Ok { anchor_offset; head_offset }

let anchor_offset value = value.anchor_offset
let head_offset value = value.head_offset

let equal left right =
  left.anchor_offset = right.anchor_offset && left.head_offset = right.head_offset

