type t

val empty : t

val register :
  t -> Zenbu_kernel.Semantic_descriptor.t -> (t, Zenbu_kernel.Error.t) result

val find : t -> string -> Zenbu_kernel.Semantic_descriptor.t option
val descriptors : t -> Zenbu_kernel.Semantic_descriptor.t list
