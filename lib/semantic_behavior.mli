type selection = { anchor_offset : int; head_offset : int }
type selection_set = { selections : selection list; primary : int }
type edit = { start_offset : int; stop_offset : int; replacement : string }

type transformation_result = {
  edits : edit list;
  selections : selection_set option;
}

type selector =
  Editor_context.t ->
  arguments:Extension_value.t ->
  (selection_set, Zenbu_kernel.Error.t) result

type transformation =
  Editor_context.t ->
  selections:selection_set ->
  arguments:Extension_value.t ->
  (transformation_result, Zenbu_kernel.Error.t) result

type selector_entry
type transformation_entry

val selector_entry :
  descriptor:Zenbu_kernel.Semantic_descriptor.t ->
  run:selector ->
  selector_entry

val transformation_entry :
  descriptor:Zenbu_kernel.Semantic_descriptor.t ->
  run:transformation ->
  transformation_entry

val selector_descriptor : selector_entry -> Zenbu_kernel.Semantic_descriptor.t

val transformation_descriptor :
  transformation_entry -> Zenbu_kernel.Semantic_descriptor.t

val run_selector : selector_entry -> selector
val run_transformation : transformation_entry -> transformation
