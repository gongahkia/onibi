type t = int

let initial = 0

let of_int value = if value < 0 then Error (Error.Invalid_version value) else Ok value
let to_int value = value
let successor value = value + 1
let compare = Int.compare
let equal = Int.equal

