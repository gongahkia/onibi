type selector =
  | Builtin_selector of Model_intent.selector
  | Registered_selector of { id : string; arguments : Extension_value.t }

type transformation =
  | Builtin_transformation of Model_intent.transformation
  | Registered_transformation of { id : string; arguments : Extension_value.t }

type t = { selector : selector; transformation : transformation }

let selector_id = function
  | Builtin_selector selector ->
      Model_intent.apply ~selector ~transformation:Model_intent.Select
      |> Model_intent.semantic_components |> fst |> Option.get
  | Registered_selector { id; _ } -> id

let transformation_id = function
  | Builtin_transformation transformation ->
      Model_intent.apply ~selector:Model_intent.Current_selections
        ~transformation
      |> Model_intent.semantic_components |> snd |> Option.get
  | Registered_transformation { id; _ } -> id

let selector_arguments = function
  | Builtin_selector _ -> Extension_value.Nil
  | Registered_selector { arguments; _ } -> arguments

let transformation_arguments = function
  | Builtin_transformation _ -> Extension_value.Nil
  | Registered_transformation { arguments; _ } -> arguments

let identity value =
  "apply:" ^ selector_id value.selector ^ ":"
  ^ transformation_id value.transformation
