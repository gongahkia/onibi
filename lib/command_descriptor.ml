open Zenbu_kernel

type parameter = { name : string; description : string; required : bool }

type t = {
  id : Command_id.t;
  title : string;
  description : string option;
  category : string option;
  parameters : parameter list;
  examples : string list;
  provider : Provider.t;
}

let create ~id ~title ?description ?category ?(parameters = []) ?(examples = [])
    ?provider () =
  if String.length title = 0 then
    Error (Error.Invalid_command_arguments "command title must not be empty")
  else if
    List.exists (fun parameter -> String.length parameter.name = 0) parameters
  then
    Error
      (Error.Invalid_command_arguments
         "command parameter name must not be empty")
  else
    let names = List.map (fun parameter -> parameter.name) parameters in
    if List.length names <> List.length (List.sort_uniq String.compare names)
    then
      Error
        (Error.Invalid_command_arguments
           "command parameter names must be unique")
    else
      let provider =
        Option.value provider
          ~default:
            (Provider.create ~id:"zenbu.builtin" ~kind:Provider.Builtin
            |> Result.get_ok)
      in
      Ok { id; title; description; category; parameters; examples; provider }

let id value = value.id
let title value = value.title
let description value = value.description
let category value = value.category
let parameters value = value.parameters
let examples value = value.examples
let provider value = value.provider
