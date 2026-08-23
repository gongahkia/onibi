open Zenbu_model_api

let static = function
  | Ok value -> value
  | Error _ -> failwith "invalid static semantic command declaration"

let provider =
  static
    (Zenbu_kernel.Provider.create ~id:"zenbu.models"
       ~kind:Zenbu_kernel.Provider.Editing_model)

let apply_id = static (Command_id.of_string "editor.apply")

let descriptor =
  static
    (Command_descriptor.create ~id:apply_id ~title:"Apply semantic operation"
       ~description:
         "Apply a reusable selector and transformation through the editing \
          runtime."
       ~category:"editing"
       ~parameters:
         [
           {
             Command_descriptor.name = "selector";
             description = "The model-neutral target selector.";
             required = true;
             kind = Command_descriptor.Selector;
           };
           {
             Command_descriptor.name = "transformation";
             description = "The model-neutral transformation.";
             required = true;
             kind = Command_descriptor.Transformation;
           };
         ]
       ~examples:[ "editor.apply(selector: next-word, transformation: delete)" ]
       ~provider ())

let handler _context invocation =
  let ( let* ) result f = Result.bind result f in
  let* selector = Command_invocation.find invocation ~name:"selector" in
  let* selector = Command_argument.as_selector selector in
  let* transformation =
    Command_invocation.find invocation ~name:"transformation"
  in
  let* transformation = Command_argument.as_transformation transformation in
  Ok [ Model_intent.apply ~selector ~transformation ]

let apply_command = Command.create ~descriptor ~handler

let selection_command id title description ?(parameters = []) run =
  let id = Command_id.of_string id |> static in
  let descriptor =
    Command_descriptor.create ~id ~title ~description ~category:"selection"
      ~parameters ~provider ()
    |> static
  in
  Command.create ~descriptor ~handler:(fun context invocation ->
      run context invocation |> Result.map (fun intent -> [ intent ]))

let required_text invocation name =
  let ( let* ) result f = Result.bind result f in
  let* argument = Command_invocation.find invocation ~name in
  match argument with
  | Command_argument.Text text -> Ok text
  | Command_argument.Selector _ | Command_argument.Transformation _ ->
      Error
        (Zenbu_kernel.Error.Invalid_command_arguments "expected a text argument")

let regex_parameter =
  Command_descriptor.
    {
      name = "pattern";
      description = "OCaml Str regular expression; empty matches are rejected.";
      required = true;
      kind = Text;
    }

let selection_commands =
  [
    selection_command "editor.selection.select-regex" "Select regex matches"
      "Replace current selections with non-empty regex matches."
      ~parameters:[ regex_parameter ] (fun context invocation ->
        Result.bind (required_text invocation "pattern") (fun pattern ->
            Selection_algebra.select_regex context ~pattern));
    selection_command "editor.selection.split-regex" "Split selections on regex"
      "Split current selections at non-empty regex matches, dropping \
       separators."
      ~parameters:[ regex_parameter ] (fun context invocation ->
        Result.bind (required_text invocation "pattern") (fun pattern ->
            Selection_algebra.split_regex context ~pattern));
    selection_command "editor.selection.keep-regex"
      "Keep regex-matching selections"
      "Keep current selections containing a non-empty regex match."
      ~parameters:[ regex_parameter ] (fun context invocation ->
        Result.bind (required_text invocation "pattern") (fun pattern ->
            Selection_algebra.keep_matching context ~pattern));
    selection_command "editor.selection.remove-regex"
      "Remove regex-matching selections"
      "Remove current selections containing a non-empty regex match."
      ~parameters:[ regex_parameter ] (fun context invocation ->
        Result.bind (required_text invocation "pattern") (fun pattern ->
            Selection_algebra.remove_matching context ~pattern));
    selection_command "editor.selection.merge-consecutive"
      "Merge consecutive selections"
      "Merge selections that touch at a document boundary." (fun context _ ->
        Selection_algebra.merge_consecutive context);
    selection_command "editor.selection.rotate-primary-forward"
      "Rotate primary selection forward"
      "Make the next selection in document order primary." (fun context _ ->
        Selection_algebra.rotate_primary context Selection_algebra.Forward);
    selection_command "editor.selection.rotate-primary-backward"
      "Rotate primary selection backward"
      "Make the previous selection in document order primary." (fun context _ ->
        Selection_algebra.rotate_primary context Selection_algebra.Backward);
    selection_command "editor.selection.rotate-contents-forward"
      "Rotate selection contents forward"
      "Move each non-empty selection's text to the next selection in document \
       order." (fun context _ ->
        Selection_algebra.rotate_contents context Selection_algebra.Forward);
    selection_command "editor.selection.rotate-contents-backward"
      "Rotate selection contents backward"
      "Move each non-empty selection's text to the previous selection in \
       document order." (fun context _ ->
        Selection_algebra.rotate_contents context Selection_algebra.Backward);
    selection_command "editor.selection.flip" "Flip selection orientation"
      "Swap anchor and head for every current selection." (fun context _ ->
        Selection_algebra.flip context);
    selection_command "editor.selection.ensure-forward"
      "Ensure selections are forward"
      "Normalize every selection to increasing anchor/head order."
      (fun context _ -> Selection_algebra.ensure_forward context);
  ]

let selection_effect id =
  Result.bind (Command_id.of_string id) (fun id ->
      Command_invocation.create ~id ~arguments:[])
  |> Result.map (fun invocation -> Model_effect.Invoke_command invocation)
  |> static

let merge_consecutive = selection_effect "editor.selection.merge-consecutive"

let rotate_primary_forward =
  selection_effect "editor.selection.rotate-primary-forward"

let rotate_primary_backward =
  selection_effect "editor.selection.rotate-primary-backward"

let rotate_contents_forward =
  selection_effect "editor.selection.rotate-contents-forward"

let rotate_contents_backward =
  selection_effect "editor.selection.rotate-contents-backward"

let flip_selections = selection_effect "editor.selection.flip"

let ensure_selections_forward =
  selection_effect "editor.selection.ensure-forward"

let apply ~selector ~transformation =
  let selector =
    static
      (Command_argument.make ~name:"selector"
         ~value:(Command_argument.Selector selector))
  in
  let transformation =
    static
      (Command_argument.make ~name:"transformation"
         ~value:(Command_argument.Transformation transformation))
  in
  let invocation =
    static
      (Command_invocation.create ~id:apply_id
         ~arguments:[ selector; transformation ])
  in
  Model_effect.Invoke_command invocation
