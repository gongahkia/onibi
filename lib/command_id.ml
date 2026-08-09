open Zenbu_kernel

type t = string

let valid_character = function
  | 'a' .. 'z' | 'A' .. 'Z' | '0' .. '9' | '.' | '-' | '_' -> true
  | _ -> false

let of_string value =
  if String.length value = 0 || not (String.for_all valid_character value) then
    Error (Error.Invalid_command_id value)
  else Ok value

let to_string value = value
let compare = String.compare
let equal = String.equal
