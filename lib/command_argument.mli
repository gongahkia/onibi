type value = Text of string | Selector of Model_intent.selector | Transformation of Model_intent.transformation
type t

val make : name:string -> value:value -> (t, Error.t) result
val name : t -> string
val value : t -> value

