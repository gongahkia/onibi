open Zenbu_model_api

type state = Direct | Control_x_prefix

let static = function
  | Ok value -> value
  | Error _ -> failwith "invalid static direct model declaration"

let provider =
  static
    (Zenbu_kernel.Provider.create ~id:"zenbu.models.direct"
       ~kind:Zenbu_kernel.Provider.Editing_model)

let descriptor =
  static
    (Editing_model.descriptor ~id:"zenbu.direct" ~title:"Direct editing model"
       ~description:
         "An always-inserting, direct-manipulation grammar for evaluating \
          non-modal terminal editor workloads through shared Zenbu semantics."
       ~provider ())

let descriptor_of_state _ = descriptor
let initialize _context = Direct
let reset _state _context = Direct

let status = function
  | Direct ->
      static
        (Model_status.create ~id:"direct" ~label:"DIRECT"
           ~description:
             "committed text inserts immediately; navigation keeps a caret or \
              extends a selection"
           ~input_mode:Model_status.Text_entry ())
  | Control_x_prefix ->
      static
        (Model_status.create ~id:"control-x-prefix" ~label:"C-X…"
           ~description:"awaiting an Emacs-style control-X command"
           ~pending_input:"Ctrl-x" ())

let is_named event named =
  match Input_event.key event with
  | Some (Input_event.Named_key value) -> value = named
  | Some (Input_event.Logical_text _) | None -> false

let is_control event text =
  match Input_event.key event with
  | Some (Input_event.Logical_text value) ->
      Input_event.modifiers event = [ Input_event.Control ]
      && String.equal value text
  | Some (Input_event.Named_key _) | None -> false

let is_plain_text event text =
  match Input_event.key event with
  | Some (Input_event.Logical_text value) ->
      Input_event.modifiers event = [] && String.equal value text
  | Some (Input_event.Named_key _) | None -> false

let is_shift event =
  match Input_event.key event with
  | Some _ -> Input_event.modifiers event = [ Input_event.Shift ]
  | None -> false

let text_key event =
  match Input_event.key event with
  | Some (Input_event.Logical_text text) when Input_event.modifiers event = []
    ->
      Some text
  | Some (Input_event.Named_key _) | Some (Input_event.Logical_text _) | None ->
      None

let apply selector transformation =
  Semantic_commands.apply ~selector ~transformation

let move selector collapse = [ apply selector collapse ]
let extend selector = [ apply selector Model_intent.Select ]

let has_nonempty_selection context =
  Editor_context.selections context |> fun selections ->
  List.exists
    (fun selection ->
      selection.Editor_context.anchor_offset
      <> selection.Editor_context.head_offset)
    selections.Editor_context.selections

let delete_backward context =
  if has_nonempty_selection context then
    [ apply Model_intent.Current_selections Model_intent.Delete ]
  else [ apply Model_intent.Previous_text_unit Model_intent.Delete ]

let delete_forward context =
  if has_nonempty_selection context then
    [ apply Model_intent.Current_selections Model_intent.Delete ]
  else [ apply Model_intent.Next_text_unit Model_intent.Delete ]

let copy =
  Model_effect.Copy_to_clipboard
    {
      slot = Clipboard.unnamed;
      selector = Model_intent.Current_selections;
      kind = Clipboard.Characterwise;
    }

let cut context =
  if has_nonempty_selection context then
    [
      Model_effect.Cut_to_clipboard
        {
          slot = Clipboard.unnamed;
          selector = Model_intent.Current_selections;
          kind = Clipboard.Characterwise;
        };
    ]
  else
    [
      static
        (Model_effect.message ~level:Model_effect.Warning
           ~text:"cut requires a non-empty selection");
    ]

let direct_key event context =
  if is_control event "x" then (Control_x_prefix, [])
  else if is_control event "s" then (Direct, [ Model_effect.Request_save ])
  else if is_control event "z" then (Direct, [ Model_effect.Undo ])
  else if is_control event "y" then (Direct, [ Model_effect.Redo ])
  else if is_control event "c" then (Direct, [ copy ])
  else if is_control event "w" then (Direct, cut context)
  else if is_control event "v" then
    ( Direct,
      [
        Model_effect.Paste_from_clipboard
          { slot = Clipboard.unnamed; placement = Clipboard.Replace };
      ] )
  else if is_control event "b" then
    (Direct, move Model_intent.Previous_text_unit Model_intent.Collapse_to_start)
  else if is_control event "f" then
    (Direct, move Model_intent.Next_text_unit Model_intent.Collapse_to_end)
  else if is_control event "p" then
    (Direct, move Model_intent.Previous_line Model_intent.Collapse_to_start)
  else if is_control event "n" then
    (Direct, move Model_intent.Next_line Model_intent.Collapse_to_end)
  else if is_control event "a" then
    (Direct, move Model_intent.Line_start Model_intent.Collapse_to_start)
  else if is_control event "e" then
    (Direct, move Model_intent.Line_end Model_intent.Collapse_to_end)
  else if is_control event "d" then (Direct, delete_forward context)
  else if is_control event "h" then (Direct, delete_backward context)
  else if is_named event Input_event.Backspace then
    (Direct, delete_backward context)
  else if is_named event Input_event.Delete then (Direct, delete_forward context)
  else if is_named event Input_event.Enter then
    (Direct, [ Model_effect.Execute_intent (Model_intent.insert_text "\n") ])
  else if is_named event Input_event.Arrow_left then
    ( Direct,
      if is_shift event then extend Model_intent.Previous_text_unit
      else move Model_intent.Previous_text_unit Model_intent.Collapse_to_start
    )
  else if is_named event Input_event.Arrow_right then
    ( Direct,
      if is_shift event then extend Model_intent.Next_text_unit
      else move Model_intent.Next_text_unit Model_intent.Collapse_to_end )
  else if is_named event Input_event.Arrow_up then
    ( Direct,
      if is_shift event then extend Model_intent.Previous_line
      else move Model_intent.Previous_line Model_intent.Collapse_to_start )
  else if is_named event Input_event.Arrow_down then
    ( Direct,
      if is_shift event then extend Model_intent.Next_line
      else move Model_intent.Next_line Model_intent.Collapse_to_end )
  else if is_named event Input_event.Home then
    (Direct, move Model_intent.Line_start Model_intent.Collapse_to_start)
  else if is_named event Input_event.End then
    (Direct, move Model_intent.Line_end Model_intent.Collapse_to_end)
  else
    match text_key event with
    | Some text ->
        (Direct, [ Model_effect.Execute_intent (Model_intent.insert_text text) ])
    | None -> (Direct, [])

let handle_input state event context =
  match state with
  | Direct -> (
      match Input_event.text event with
      | Some text ->
          ( Direct,
            [ Model_effect.Execute_intent (Model_intent.insert_text text) ] )
      | None -> direct_key event context)
  | Control_x_prefix when is_control event "s" ->
      (Direct, [ Model_effect.Request_save ])
  | Control_x_prefix when is_control event "f" ->
      (Direct, [ Model_effect.Request_workspace Model_effect.Open_buffer ])
  | Control_x_prefix when is_plain_text event "k" ->
      (Direct, [ Model_effect.Request_workspace Model_effect.Close_buffer ])
  | Control_x_prefix when is_plain_text event "2" ->
      ( Direct,
        [ Model_effect.Request_workspace Model_effect.Split_view_horizontal ] )
  | Control_x_prefix when is_plain_text event "3" ->
      ( Direct,
        [ Model_effect.Request_workspace Model_effect.Split_view_vertical ] )
  | Control_x_prefix when is_plain_text event "0" ->
      (Direct, [ Model_effect.Request_workspace Model_effect.Close_view ])
  | Control_x_prefix when is_plain_text event "1" ->
      (Direct, [ Model_effect.Request_workspace Model_effect.Keep_only_view ])
  | Control_x_prefix when is_plain_text event "o" ->
      (Direct, [ Model_effect.Request_workspace Model_effect.Focus_next_view ])
  | Control_x_prefix
    when is_named event Input_event.Escape || is_control event "g" ->
      (Direct, [])
  | Control_x_prefix -> (Direct, [])

let input_rule ?next_status ?selector_id ?transformation_id id pattern kind
    summary =
  static
    (Input_rule.create ~id ~pattern ~kind ~summary ?next_status ?selector_id
       ?transformation_id ())

let input_rules = function
  | Direct ->
      [
        input_rule "direct.insert" Input_rule.Text_input Input_rule.Catch_all
          "insert committed Unicode text immediately"
          ~transformation_id:"insert-text";
        input_rule "direct.navigation"
          (Input_rule.Text_range
             "arrows, Home/End, Ctrl-a/b/e/f/n/p, or Shift-arrows")
          Input_rule.Binding "move a caret or extend the primary selection";
        input_rule "direct.delete"
          (Input_rule.Text_range "Backspace, Delete, Ctrl-h, or Ctrl-d")
          Input_rule.Binding "delete a selection or adjacent text unit";
        input_rule "direct.clipboard" (Input_rule.Text_range "Ctrl-c or Ctrl-v")
          Input_rule.Binding "copy or paste through the shared clipboard";
        input_rule "direct.history" (Input_rule.Text_range "Ctrl-z or Ctrl-y")
          Input_rule.Binding "undo or redo shared semantic history";
        input_rule "direct.save" (Input_rule.Exact "Ctrl-s") Input_rule.Binding
          "request host save";
        input_rule "direct.control-x" (Input_rule.Exact "Ctrl-x")
          Input_rule.Prefix "begin an Emacs-style control-X command"
          ~next_status:"control-x-prefix";
      ]
  | Control_x_prefix ->
      [
        input_rule "direct.control-x.save" (Input_rule.Exact "Ctrl-s")
          Input_rule.Binding "request host save" ~next_status:"direct";
        input_rule "direct.control-x.workspace"
          (Input_rule.Text_range "Ctrl-f, k, 2, 3, 0, 1, or o")
          Input_rule.Binding "request file or workspace host operation"
          ~next_status:"direct";
        input_rule "direct.control-x.cancel"
          (Input_rule.Text_range "Escape or Ctrl-g") Input_rule.Binding
          "cancel control-X input" ~next_status:"direct";
      ]
