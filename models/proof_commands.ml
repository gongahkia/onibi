open Zenbu_model_api

let static = function
  | Ok value -> value
  | Error _ -> failwith "invalid static proof command declaration"

let apply_id = static (Command_id.of_string "editor.apply")

let apply_descriptor =
  static
    (Command_descriptor.create ~id:apply_id ~title:"Apply semantic operation"
       ~description:"Apply a model-neutral selector and transformation."
       ~category:"editing"
       ~parameters:
         [
           {
             Command_descriptor.name = "selector";
             description = "The target selector.";
             required = true;
           };
           {
             Command_descriptor.name = "transformation";
             description = "The action applied to selected regions.";
             required = true;
           };
         ]
       ~examples:[ "editor.apply(selector: next-text-unit, transformation: delete)" ]
       ())

let apply_handler _context invocation =
  let ( let* ) result f = Result.bind result f in
  let* selector = Command_invocation.find invocation ~name:"selector" in
  let* selector = Command_argument.as_selector selector in
  let* transformation =
    Command_invocation.find invocation ~name:"transformation"
  in
  let* transformation = Command_argument.as_transformation transformation in
  Ok [ Model_intent.apply ~selector ~transformation ]

let apply_command = Command.create ~descriptor:apply_descriptor ~handler:apply_handler

let apply_invocation ~selector ~transformation =
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
  static (Command_invocation.create ~id:apply_id ~arguments:[ selector; transformation ])

