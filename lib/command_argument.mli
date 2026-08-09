type value = Text of string | Selector of Model_intent.selector | Transformation of Model_intent.transformation
type t

val make : name:string -> value:value -> (t, Zenbu_kernel.Error.t) result
val name : t -> string
val value : t -> value
val as_selector : value -> (Model_intent.selector, Zenbu_kernel.Error.t) result

val as_transformation :
  value ->
  (Model_intent.transformation, Zenbu_kernel.Error.t) result
