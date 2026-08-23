(** Adapter from a validated Lua editing-model registration to the stable
    [Editing_model.S] runtime contract. The selected declaration is captured in
    every state, so existing buffers never consult Lua configuration after
    initialization. *)

type state = Zenbu_scripting.Scripting.model_state

val configure : Zenbu_scripting.Scripting.model -> unit
val configure_state : state -> unit
val clear : unit -> unit

include Zenbu_model_api.Editing_model.S with type state := state
