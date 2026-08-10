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

type selector_entry = {
  descriptor : Zenbu_kernel.Semantic_descriptor.t;
  run : selector;
}

type transformation_entry = {
  descriptor : Zenbu_kernel.Semantic_descriptor.t;
  run : transformation;
}

let selector_entry ~descriptor ~(run : selector) : selector_entry =
  { descriptor; run }

let transformation_entry ~descriptor ~(run : transformation) :
    transformation_entry =
  { descriptor; run }

let selector_descriptor (value : selector_entry) = value.descriptor
let transformation_descriptor (value : transformation_entry) = value.descriptor
let run_selector (value : selector_entry) = value.run
let run_transformation (value : transformation_entry) = value.run
