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

type selector_entry =
  | Local_selector of {
      descriptor : Zenbu_kernel.Semantic_descriptor.t;
      run : selector;
    }
  | Extension_selector of {
      descriptor : Zenbu_kernel.Semantic_descriptor.t;
      host : Extension_host.t;
      invocation : Extension_host.invocation;
      decode :
        Extension_host.request ->
        Extension_value.t ->
        (selection_set, Zenbu_kernel.Error.t) result;
    }

type transformation_entry =
  | Local_transformation of {
      descriptor : Zenbu_kernel.Semantic_descriptor.t;
      run : transformation;
    }
  | Extension_transformation of {
      descriptor : Zenbu_kernel.Semantic_descriptor.t;
      host : Extension_host.t;
      invocation : Extension_host.invocation;
      decode :
        Extension_host.request ->
        Extension_value.t ->
        (transformation_result, Zenbu_kernel.Error.t) result;
    }

let selector_entry ~descriptor ~(run : selector) : selector_entry =
  Local_selector { descriptor; run }

let extension_selector_entry ~descriptor ~host ~invocation ~decode =
  Extension_selector { descriptor; host; invocation; decode }

let transformation_entry ~descriptor ~(run : transformation) :
    transformation_entry =
  Local_transformation { descriptor; run }

let extension_transformation_entry ~descriptor ~host ~invocation ~decode =
  Extension_transformation { descriptor; host; invocation; decode }

let selector_descriptor = function
  | Local_selector value -> value.descriptor
  | Extension_selector value -> value.descriptor

let transformation_descriptor = function
  | Local_transformation value -> value.descriptor
  | Extension_transformation value -> value.descriptor

let run_selector = function
  | Local_selector value -> value.run
  | Extension_selector { host; invocation; decode; _ } ->
      fun context ~arguments ->
        let request =
          Extension_host.request invocation ~kind:Extension_host.Selector
            ~operation:"selector.resolve" ~context ~arguments
        in
        let invoked =
          Result.bind
            (Extension_host.require request ~capability:"selection.write")
            (fun () -> Extension_host.invoke host invocation request)
        in
        Result.bind invoked (decode request)

let run_transformation = function
  | Local_transformation value -> value.run
  | Extension_transformation { host; invocation; decode; _ } ->
      fun context ~selections ~arguments ->
        let selection_set =
          Extension_value.List
            (List.map
               (fun selection ->
                 Extension_value.Record
                   [
                     ( "anchor",
                       Extension_value.Integer selection.anchor_offset );
                     ("head", Extension_value.Integer selection.head_offset);
                   ])
               selections.selections)
        in
        let arguments =
          Extension_value.Record
            [ ("selection_set", selection_set); ("arguments", arguments) ]
        in
        let request =
          Extension_host.request invocation ~kind:Extension_host.Transformation
            ~operation:"transformation.apply" ~context ~arguments
        in
        let invoked =
          Result.bind
            (Extension_host.require request ~capability:"selection.read")
            (fun () ->
              Extension_host.require request ~capability:"document.edit")
          |> fun result ->
          Result.bind result (fun () -> Extension_host.invoke host invocation request)
        in
        Result.bind invoked (decode request)
