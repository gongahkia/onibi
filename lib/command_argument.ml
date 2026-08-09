open Zenbu_kernel

type value =
  | Text of string
  | Selector of Model_intent.selector
  | Transformation of Model_intent.transformation

type t = { name : string; value : value }

let make ~name ~value =
  if String.length name = 0 then
    Error (Error.Invalid_command_arguments "argument name must not be empty")
  else Ok { name; value }

let name value = value.name
let value value = value.value

let as_selector = function
  | Selector selector -> Ok selector
  | _ -> Error (Error.Invalid_command_arguments "expected a selector argument")

let as_transformation = function
  | Transformation transformation -> Ok transformation
  | _ -> Error (Error.Invalid_command_arguments "expected a transformation argument")
