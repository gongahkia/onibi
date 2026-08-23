type parameter_kind = Text | Selector | Transformation

type parameter = {
  name : string;
  description : string;
  required : bool;
  kind : parameter_kind;
}
type t

val create :
  id:Command_id.t ->
  title:string ->
  ?description:string ->
  ?category:string ->
  ?parameters:parameter list ->
  ?examples:string list ->
  ?provider:Zenbu_kernel.Provider.t ->
  unit ->
  (t, Zenbu_kernel.Error.t) result

val id : t -> Command_id.t
val title : t -> string
val description : t -> string option
val category : t -> string option
val parameters : t -> parameter list
val parameter_kind_to_string : parameter_kind -> string
val parameter_kind_of_string : string -> (parameter_kind, Zenbu_kernel.Error.t) result
val examples : t -> string list
val provider : t -> Zenbu_kernel.Provider.t
