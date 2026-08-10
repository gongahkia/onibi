open Zenbu_kernel

type descriptor = {
  id : string;
  title : string;
  description : string option;
  provider : Provider.t;
}

let descriptor ~id ~title ?description ?provider () =
  if String.length id = 0 || String.length title = 0 then
    Error (Error.Invalid_model_status "model id and title must not be empty")
  else
    let provider =
      Option.value provider
        ~default:
          (Provider.create ~id:"zenbu.builtin" ~kind:Provider.Builtin
          |> Result.get_ok)
    in
    Ok { id; title; description; provider }

let id value = value.id
let title value = value.title
let description value = value.description
let provider value = value.provider

module type S = sig
  type state

  val descriptor : descriptor
  val initialize : Editor_context.t -> state

  val handle_input :
    state -> Input_event.t -> Editor_context.t -> state * Model_effect.t list

  val reset : state -> Editor_context.t -> state
  val status : state -> Model_status.t
  val input_rules : state -> Input_rule.t list
end
