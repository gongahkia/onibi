(** Stable references to semantic behavior. Effects carry these values instead
    of runtime callbacks so resolution remains inspectable and replaceable. *)

type selector =
  | Builtin_selector of Model_intent.selector
  | Registered_selector of { id : string; arguments : Extension_value.t }

type transformation =
  | Builtin_transformation of Model_intent.transformation
  | Registered_transformation of { id : string; arguments : Extension_value.t }

type t = { selector : selector; transformation : transformation }

val selector_id : selector -> string
val transformation_id : transformation -> string
val selector_arguments : selector -> Extension_value.t
val transformation_arguments : transformation -> Extension_value.t
val identity : t -> string
