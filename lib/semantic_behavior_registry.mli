(** An immutable executable counterpart to [Semantic_registry]. Built-in
    behavior remains in the typed kernel; this registry owns only extensions. *)

type t

val empty : t

val register_selector :
  t -> Semantic_behavior.selector_entry -> (t, Zenbu_kernel.Error.t) result

val register_transformation :
  t ->
  Semantic_behavior.transformation_entry ->
  (t, Zenbu_kernel.Error.t) result

val find_selector : t -> string -> Semantic_behavior.selector_entry option

val find_transformation :
  t -> string -> Semantic_behavior.transformation_entry option

val descriptors : t -> Zenbu_kernel.Semantic_descriptor.t list
val merge : t -> t -> (t, Zenbu_kernel.Error.t) result
