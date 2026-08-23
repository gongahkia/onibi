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

type binding = {
  input : string;
  command : string;
  scope : string option;
  mode_transition : mode_transition option;
  text_argument : string option;
}

and mode_transition =
  | Replace_mode of string
  | Push_mode of string
  | Pop_mode
  | Clear_modes

type hook = { event : string; callback : callback }

type registration =
  | Command of descriptor * callback
  | Selector of descriptor * callback
  | Transformation of descriptor * callback
  | Mode of mode
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
