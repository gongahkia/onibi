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
           };
           {
             Command_descriptor.name = "transformation";
             description = "The model-neutral transformation.";
             required = true;
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
