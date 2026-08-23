open Zenbu_model_api

type state = unit

let static = function
  | Ok value -> value
  | Error _ -> failwith "invalid static proof model declaration"

let descriptor =
  static
    (Editing_model.descriptor ~id:"proof.selection-first"
       ~title:"Selection-first proof model"
       ~description:
         "Minimal visible-selection grammar used only to validate the M2 API."
       ())

let descriptor_of_state _ = descriptor
let initialize _context = ()
let reset _state _context = ()

let status () =
  static
    (Model_status.create ~id:"select" ~label:"SELECT"
       ~description:"selection-first proof model" ())

let is_text event expected =
  match Input_event.key event with
  | Some (Input_event.Logical_text text) ->
      Input_event.modifiers event = [] && String.equal text expected
  | Some (Input_event.Named_key _) | None -> false

let apply selector transformation =
  Model_effect.Invoke_command
    (Proof_commands.apply_invocation ~selector ~transformation)

let handle_input () event _context =
  if is_text event "w" then
    ((), [ apply Model_intent.Next_text_unit Model_intent.Select ])
  else if is_text event "d" then
    ((), [ apply Model_intent.Current_selections Model_intent.Delete ])
  else ((), [])

let input_rules () =
  [
    static
      (Input_rule.create ~id:"proof-selection.word"
         ~pattern:(Input_rule.Exact "w") ~kind:Input_rule.Binding
         ~summary:"select next text unit" ~selector_id:"next-text-unit"
         ~transformation_id:"select" ());
    static
      (Input_rule.create ~id:"proof-selection.delete"
         ~pattern:(Input_rule.Exact "d") ~kind:Input_rule.Binding
         ~summary:"delete current selections" ~selector_id:"current-selections"
         ~transformation_id:"delete" ());
  ]
