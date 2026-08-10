type t =
  | Document_read
  | Document_edit
  | Selection_read
  | Selection_write
  | Syntax_read
  | Command_invoke
  | Ui_message
  | Event_subscribe

val all : t list
val id : t -> string
val of_id : string -> (t, Zenbu_kernel.Error.t) result
val description : t -> string
