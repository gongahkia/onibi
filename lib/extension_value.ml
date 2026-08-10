type t =
  | Nil
  | Bool of bool
  | Integer of int
  | Float of float
  | Text of string
  | List of t list
  | Record of (string * t) list

let record fields =
  let keys = List.map fst fields in
  if List.length keys <> List.length (List.sort_uniq String.compare keys) then
    Error
      (Zenbu_kernel.Error.Invalid_command_arguments
         "extension record keys must be unique")
  else Ok (Record fields)

let find value key =
  match value with
  | Record fields -> List.assoc_opt key fields
  | Nil | Bool _ | Integer _ | Float _ | Text _ | List _ -> None

let rec to_string = function
  | Nil -> "nil"
  | Bool value -> string_of_bool value
  | Integer value -> string_of_int value
  | Float value -> string_of_float value
  | Text value -> Printf.sprintf "%S" value
  | List values -> "[" ^ String.concat ", " (List.map to_string values) ^ "]"
  | Record fields ->
      let field (name, value) = name ^ " = " ^ to_string value in
      "{" ^ String.concat ", " (List.map field fields) ^ "}"
