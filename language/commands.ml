open Zenbu_model_api

let static = function
  | Ok value -> value
  | Error error -> failwith (Zenbu_kernel.Error.to_string error)

let provider =
  Zenbu_kernel.Provider.create ~id:"zenbu.language"
    ~kind:Zenbu_kernel.Provider.Application
  |> static

let apply_edits_id = "language.apply-edits"

let descriptor =
  Zenbu_kernel.Semantic_descriptor.create ~id:apply_edits_id
    ~title:"Apply language-service edits"
    ~description:
      "Apply validated current-document edits from a language service."
    ~provider ~kind:Zenbu_kernel.Semantic_descriptor.Transformation ()
  |> static

let edit_value (edit : Language.text_edit) =
  Extension_value.Record
    [
      ("start", Extension_value.Integer edit.start_offset);
      ("stop", Extension_value.Integer edit.stop_offset);
      ("text", Extension_value.Text edit.replacement);
    ]

let arguments edits =
  Extension_value.Record
    [ ("edits", Extension_value.List (List.map edit_value edits)) ]

let decode_edit = function
  | Extension_value.Record fields -> (
      match
        ( List.assoc_opt "start" fields,
          List.assoc_opt "stop" fields,
          List.assoc_opt "text" fields )
      with
      | ( Some (Extension_value.Integer start_offset),
          Some (Extension_value.Integer stop_offset),
          Some (Extension_value.Text replacement) ) ->
          if start_offset < 0 || stop_offset < start_offset then
            Error
              (Zenbu_kernel.Error.Invalid_range "language edit range is invalid")
          else Ok { Language.start_offset; stop_offset; replacement }
      | _ ->
          Error
            (Zenbu_kernel.Error.Invalid_command_arguments
               "language edits are malformed"))
  | _ ->
      Error
        (Zenbu_kernel.Error.Invalid_command_arguments
           "language edits are malformed")

let decode arguments =
  match Extension_value.find arguments "edits" with
  | Some (Extension_value.List values) ->
      List.fold_right
        (fun value result ->
          Result.bind (decode_edit value) (fun edit ->
              Result.map (fun edits -> edit :: edits) result))
        values (Ok [])
  | Some _ | None ->
      Error
        (Zenbu_kernel.Error.Invalid_command_arguments
           "language edits are missing")

let behavior =
  Semantic_behavior.transformation_entry ~descriptor
    ~run:(fun _context ~selections:_ ~arguments ->
      Result.map
        (fun edits ->
          {
            Semantic_behavior.edits =
              List.map
                (fun (edit : Language.text_edit) ->
                  {
                    Semantic_behavior.start_offset = edit.start_offset;
                    stop_offset = edit.stop_offset;
                    replacement = edit.replacement;
                  })
                edits;
            selections = None;
          })
        (decode arguments))

let behaviors =
  Semantic_behavior_registry.register_transformation
    Semantic_behavior_registry.empty behavior
  |> static

let descriptors () = [ descriptor ]

let apply_edits edits =
  Model_effect.Execute_semantic_operation
    {
      Semantic_operation.selector =
        Semantic_operation.Builtin_selector Model_intent.Current_selections;
      transformation =
        Semantic_operation.Registered_transformation
          { id = apply_edits_id; arguments = arguments edits };
    }
