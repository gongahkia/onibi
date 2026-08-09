open Zenbu_model_api

type state = Command | Pending_delete | Inserting

let static = function
  | Ok value -> value
  | Error _ -> failwith "invalid static proof model declaration"

let descriptor =
  static
    (Editing_model.descriptor ~id:"proof.operator-first"
       ~title:"Operator-first proof model"
       ~description:
         "Minimal pending-operator grammar used only to validate the M2 API."
       ())

let initialize _context = Command
let reset _state _context = Command

let status = function
  | Command ->
      static
        (Model_status.create ~id:"command" ~label:"COMMAND"
           ~description:"awaiting an operator or direct command" ())
  | Pending_delete ->
      static
        (Model_status.create ~id:"pending-delete" ~label:"DELETE PENDING"
           ~description:"awaiting a selector key" ~pending_input:"d" ())
  | Inserting ->
      static
        (Model_status.create ~id:"inserting" ~label:"INSERT"
           ~description:"committed text becomes a semantic insertion" ())

let is_text event expected =
  match Input_event.key event with
  | Some (Input_event.Logical_text text) ->
      Input_event.modifiers event = [] && String.equal text expected
  | Some (Input_event.Named_key _) | None -> false

let is_escape event =
  match Input_event.key event with
  | Some (Input_event.Named_key Input_event.Escape) -> true
  | Some (Input_event.Logical_text _) | Some (Input_event.Named_key _) | None ->
      false

let apply selector transformation =
  Model_effect.Invoke_command
    (Proof_commands.apply_invocation ~selector ~transformation)

let handle_input state event _context =
  match state with
  | Command when is_text event "d" -> (Pending_delete, [])
  | Command when is_text event "i" -> (Inserting, [])
  | Command when is_text event "x" ->
      (Command, [ apply Model_intent.Current_selections Model_intent.Delete ])
  | Pending_delete when is_text event "w" ->
      (Command, [ apply Model_intent.Next_text_unit Model_intent.Delete ])
  | Pending_delete when is_escape event -> (Command, [])
  | Pending_delete -> (Pending_delete, [])
  | Inserting when is_escape event -> (Command, [])
  | Inserting -> (
      match Input_event.text event with
      | Some text ->
          ( Inserting,
            [ Model_effect.Execute_intent (Model_intent.insert_text text) ] )
      | None -> (Inserting, []))
  | Command -> (Command, [])
