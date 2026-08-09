type descriptor = { id : string; title : string; description : string option }

let descriptor ~id ~title ?description () =
  if String.length id = 0 || String.length title = 0 then
    Error (Error.Invalid_model_status "model id and title must not be empty")
  else Ok { id; title; description }

let id value = value.id
let title value = value.title
let description value = value.description

module type S = sig
  type state

  val descriptor : descriptor
  val initialize : Editor_context.t -> state
  val handle_input : state -> Input_event.t -> Editor_context.t -> state * Model_effect.t list
  val reset : state -> Editor_context.t -> state
  val status : state -> Model_status.t
end

