(** Private PUC-Lua 5.4 adapter. Its values are converted at this boundary; no
    Lua state is exposed by [Zenbu_scripting]. *)

type callback

type descriptor = {
  id : string;
  title : string;
  description : string;
  requires_syntax : bool;
  parameters : Zenbu_model_api.Command_descriptor.parameter list;
}

type mode = {
  id : string;
  title : string;
  description : string;
  input_mode : input_mode;
  initial : bool;
}

and input_mode = Key_commands | Text_entry

type binding_layer = {
  id : string;
  title : string;
  description : string;
  priority : int;
}

type binding = {
  input : string;
  command : string;
  scope : string option;
  layer : string option;
  mode_transition : mode_transition option;
  text_argument : string option;
}

and mode_transition =
  | Replace_mode of string
  | Push_mode of string
  | Pop_mode
  | Clear_modes

type hook = { event : string; callback : callback }

type state_persistence = {
  schema : string;
  version : int;
  export : callback;
  import : callback;
}

type model = {
  descriptor : descriptor;
  initial_state : Zenbu_model_api.Extension_value.t;
  initial_status : Zenbu_model_api.Extension_value.t;
  callback : callback;
  persistence : state_persistence option;
}

type registration =
  | Command of descriptor * callback
  | Selector of descriptor * callback
  | Transformation of descriptor * callback
  | Model of model
  | Mode of mode
  | Binding_layer of binding_layer
  | Binding of binding
  | Hook of hook

type t

val create : source:string -> (t, Zenbu_kernel.Error.t) result
val evaluate : t -> string -> (unit, Zenbu_kernel.Error.t) result
val registrations : t -> registration list

val call :
  t ->
  callback ->
  request:Zenbu_model_api.Extension_host.request ->
  (Zenbu_model_api.Extension_value.t, Zenbu_kernel.Error.t) result

val dispose : t -> unit
