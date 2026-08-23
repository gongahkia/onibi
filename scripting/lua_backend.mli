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

type binding = {
  input : string;
  command : string;
  scope : string option;
  next_mode : string option;
}
type hook = { event : string; callback : callback }

type registration =
  | Command of descriptor * callback
  | Selector of descriptor * callback
  | Transformation of descriptor * callback
  | Mode of descriptor
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
