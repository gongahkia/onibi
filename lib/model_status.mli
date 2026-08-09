type input_mode = Key_commands | Text_entry
type t

val create :
  id:string ->
  label:string ->
  ?description:string ->
  ?pending_input:string ->
  ?metadata:(string * string) list ->
  ?input_mode:input_mode ->
  unit ->
  (t, Zenbu_kernel.Error.t) result

val id : t -> string
val label : t -> string
val description : t -> string option
val pending_input : t -> string option
val metadata : t -> (string * string) list
val input_mode : t -> input_mode
