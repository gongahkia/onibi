type t = string

let of_string value =
  if String.length value = 0 then Error (Error.Invalid_document_id value) else Ok value

let to_string value = value
let equal = String.equal
let compare = String.compare

