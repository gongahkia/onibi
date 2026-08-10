open Zenbu_model_api

type mode = Navigate | Insert
type focus = { kind : string; selection_count : int }

type state = {
  mode : mode;
  syntax_available : bool;
  focus : focus option;
  shrink_stack : (int * int) list;
  shrink_version : int option;
}

let static = function
  | Ok value -> value
  | Error _ -> failwith "invalid static structural model declaration"

let provider =
  static
    (Zenbu_kernel.Provider.create ~id:"zenbu.models.structural"
       ~kind:Zenbu_kernel.Provider.Editing_model)

let descriptor =
  static
    (Editing_model.descriptor ~id:"zenbu.structural"
       ~title:"Structural editing model"
       ~description:
         "An AST-navigation grammar that derives ordinary Zenbu selections \
          from the optional public syntax snapshot."
       ~provider
       ())

let available context = Option.is_some (Editor_context.syntax context)

let idle context =
  {
    mode = Navigate;
    syntax_available = available context;
    focus = None;
    shrink_stack = [];
    shrink_version = None;
  }

let initialize = idle
let reset _state context = idle context

let status state =
  match state.mode with
  | Insert ->
      static
        (Model_status.create ~id:"struct-insert" ~label:"STRUCT INSERT"
           ~description:"text input uses the shared semantic insertion path"
           ~input_mode:Model_status.Text_entry ())
  | Navigate when not state.syntax_available ->
      static
        (Model_status.create ~id:"struct-no-syntax" ~label:"NO SYNTAX"
           ~description:
             "structural commands require a current Zenbu syntax snapshot"
           ())
  | Navigate ->
      let metadata =
        match state.focus with
        | None -> [ ("syntax", "ready") ]
        | Some { kind; selection_count } ->
            [ ("kind", kind); ("selections", string_of_int selection_count) ]
      in
      let label =
        match state.focus with
        | None -> "STRUCT"
        | Some { kind; selection_count = 1 } -> "STRUCT | " ^ kind
        | Some { kind; selection_count } ->
            Printf.sprintf "STRUCT | %s | %d" kind selection_count
      in
      static
        (Model_status.create ~id:"struct" ~label
           ~description:
             "navigate syntax nodes, then reuse shared transformations"
           ~metadata ())

let is_named event named =
  match Input_event.key event with
  | Some (Input_event.Named_key value) -> value = named
  | Some (Input_event.Logical_text _) | None -> false

let text_key event =
  match Input_event.key event with
  | Some (Input_event.Logical_text value) when Input_event.modifiers event = []
    ->
      Some value
  | Some (Input_event.Named_key _) | Some (Input_event.Logical_text _) | None ->
      None

let is_control event text =
  match Input_event.key event with
  | Some (Input_event.Logical_text value) ->
      Input_event.modifiers event = [ Input_event.Control ]
      && String.equal value text
  | Some (Input_event.Named_key _) | None -> false

let primary_selection context =
  let selections = Editor_context.selections context in
  List.nth selections.Editor_context.selections selections.primary_index

let resolve context selector =
  match Editor_context.syntax context with
  | None -> []
  | Some syntax ->
      let selection = primary_selection context in
      Zenbu_syntax.Syntax.Selector.resolve syntax
        ~anchor_offset:selection.Editor_context.anchor_offset
        ~head_offset:selection.Editor_context.head_offset selector

let selection_effect selector nodes =
  let selections =
    List.map
      (fun node ->
        ( Zenbu_syntax.Syntax.Snapshot.Node.start_offset node,
          Zenbu_syntax.Syntax.Snapshot.Node.stop_offset node ))
      nodes
  in
  match selections with
  | [] -> []
  | _ -> (
      match Model_intent.set_selections ~selections ~primary:0 with
      | Error _ -> []
  | Ok intent ->
      [
        Model_effect.execute
          ~selector_id:(Zenbu_syntax.Syntax.Selector.id selector)
          intent;
      ])

let focused_state state context nodes ~shrink_stack ~shrink_version =
  match nodes with
  | [] -> { state with syntax_available = available context }
  | node :: _ ->
      {
        mode = Navigate;
        syntax_available = true;
        focus =
          Some
            {
              kind =
                Zenbu_syntax.Syntax.Snapshot.Node.kind node
                |> Zenbu_syntax.Syntax.Kind.to_string;
              selection_count = List.length nodes;
            };
        shrink_stack;
        shrink_version;
      }

let navigation state context selector =
  let nodes = resolve context selector in
  let next =
    focused_state state context nodes ~shrink_stack:[] ~shrink_version:None
  in
  (next, selection_effect selector nodes)

let expand state context =
  let current = primary_selection context in
  let nodes = resolve context Zenbu_syntax.Syntax.Selector.Expand in
  let next =
    focused_state state context nodes
      ~shrink_stack:
        (( current.Editor_context.anchor_offset,
           current.Editor_context.head_offset )
        :: state.shrink_stack)
      ~shrink_version:(Some (Editor_context.document_version context + 1))
  in
  (next, selection_effect Zenbu_syntax.Syntax.Selector.Expand nodes)

let shrink state context =
  match (state.shrink_version, state.shrink_stack) with
  | Some version, (anchor_offset, head_offset) :: rest
    when version = Editor_context.document_version context -> (
      match
        Model_intent.set_selections
          ~selections:[ (anchor_offset, head_offset) ]
          ~primary:0
      with
      | Error _ -> (idle context, [])
      | Ok intent ->
          ( {
              state with
              shrink_stack = rest;
              shrink_version = Some (Editor_context.document_version context + 1);
            },
            [ Model_effect.Execute_intent intent ] ))
  | _ -> (idle context, [])

let has_structural_selection context =
  let selections = Editor_context.selections context in
  List.exists
    (fun selection ->
      selection.Editor_context.anchor_offset
      <> selection.Editor_context.head_offset)
    selections.Editor_context.selections

let warning text =
  match Model_effect.message ~level:Model_effect.Warning ~text with
  | Result.Ok message -> [ message ]
  | Result.Error _ -> []

let modify _state context ~enter_insert =
  if has_structural_selection context then
    ( { (idle context) with mode = (if enter_insert then Insert else Navigate) },
      [ Model_effect.Execute_intent Model_intent.delete_selected_ranges ] )
  else (idle context, warning "focus a syntax node before transforming it")

let navigate_input state event context =
  match text_key event with
  | Some "f" ->
      navigation state context Zenbu_syntax.Syntax.Selector.Focus_primary
  | Some "e" -> expand state context
  | Some "r" -> shrink state context
  | Some "m" ->
      navigation state context Zenbu_syntax.Syntax.Selector.Same_kind_siblings
  | Some "x" -> modify state context ~enter_insert:false
  | Some "c" -> modify state context ~enter_insert:true
  | Some "y" ->
      if has_structural_selection context then
        ( idle context,
          [
            Model_effect.Copy_to_clipboard
              {
                slot = Clipboard.unnamed;
                selector = Model_intent.Current_selections;
                kind = Clipboard.Characterwise;
              };
          ] )
      else (idle context, warning "focus a syntax node before copying it")
  | Some "p" ->
      ( idle context,
        [
          Model_effect.Paste_from_clipboard
            { slot = Clipboard.unnamed; placement = Clipboard.Replace };
        ] )
  | Some "i" -> ({ (idle context) with mode = Insert }, [])
  | Some "u" -> (idle context, [ Model_effect.Undo ])
  | Some "." -> (idle context, [ Model_effect.Repeat_last_edit ])
  | _ when is_named event Input_event.Arrow_up ->
      navigation state context Zenbu_syntax.Syntax.Selector.Parent
  | _ when is_named event Input_event.Arrow_down ->
      navigation state context Zenbu_syntax.Syntax.Selector.First_child
  | _ when is_named event Input_event.Arrow_right ->
      navigation state context Zenbu_syntax.Syntax.Selector.Next_sibling
  | _ when is_named event Input_event.Arrow_left ->
      navigation state context Zenbu_syntax.Syntax.Selector.Previous_sibling
  | _ when is_control event "r" -> (idle context, [ Model_effect.Redo ])
  | _ -> ({ state with syntax_available = available context }, [])

let insert_input event context =
  if is_named event Input_event.Escape then (idle context, [])
  else if is_named event Input_event.Backspace then
    ( { (idle context) with mode = Insert },
      [
        Model_effect.Execute_intent
          (Model_intent.apply ~selector:Model_intent.Current_selections
             ~transformation:Model_intent.Collapse_to_end);
        Model_effect.Execute_intent
          (Model_intent.apply ~selector:Model_intent.Previous_text_unit
             ~transformation:Model_intent.Delete);
      ] )
  else if is_named event Input_event.Enter then
    ( { (idle context) with mode = Insert },
      [ Model_effect.Execute_intent (Model_intent.insert_text "\n") ] )
  else
    match Input_event.text event with
    | Some text ->
        ( { (idle context) with mode = Insert },
          [ Model_effect.Execute_intent (Model_intent.insert_text text) ] )
    | None -> ({ (idle context) with mode = Insert }, [])

let handle_input state event context =
  match state.mode with
  | Navigate -> navigate_input state event context
  | Insert -> insert_input event context

let input_rule ?next_status ?selector_id ?transformation_id ?(requires_syntax = false)
    id pattern kind summary =
  static
    (Input_rule.create ~id ~pattern ~kind ~summary ?next_status ?selector_id
       ?transformation_id ~requires_syntax ())

let input_rules = function
  | { mode = Insert; _ } ->
      [
        input_rule "structural.insert.text" Input_rule.Text_input
          Input_rule.Catch_all "insert committed text" ~transformation_id:"replace-text";
        input_rule "structural.insert.escape" (Input_rule.Named "Escape")
          Input_rule.Binding "return to structural navigation" ~next_status:"struct";
      ]
  | { mode = Navigate; _ } ->
      [
        input_rule "structural.focus" (Input_rule.Exact "f") Input_rule.Binding
          "focus the smallest named syntax node" ~selector_id:"syntax.focus"
          ~transformation_id:"select" ~requires_syntax:true;
        input_rule "structural.parent" (Input_rule.Named "ArrowUp")
          Input_rule.Binding "select the parent syntax node" ~selector_id:"syntax.parent"
          ~transformation_id:"select" ~requires_syntax:true;
        input_rule "structural.child" (Input_rule.Named "ArrowDown")
          Input_rule.Binding "select the first child syntax node" ~selector_id:"syntax.child"
          ~transformation_id:"select" ~requires_syntax:true;
        input_rule "structural.siblings" (Input_rule.Exact "m") Input_rule.Binding
          "select same-kind syntax siblings" ~selector_id:"syntax.select-same-kind"
          ~transformation_id:"select" ~requires_syntax:true;
        input_rule "structural.delete" (Input_rule.Exact "x") Input_rule.Binding
          "delete visible structural selections" ~selector_id:"current-selections"
          ~transformation_id:"delete";
        input_rule "structural.insert" (Input_rule.Exact "i") Input_rule.Binding
          "enter committed text input" ~next_status:"struct-insert";
      ]
