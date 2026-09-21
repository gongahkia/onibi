module Editing_model = Zenbu_model_api.Editing_model
module Editor_context = Zenbu_model_api.Editor_context
module Input_event = Zenbu_model_api.Input_event
module Input_rule = Zenbu_model_api.Input_rule
module Model_effect = Zenbu_model_api.Model_effect
module Model_intent = Zenbu_model_api.Model_intent
module Model_status = Zenbu_model_api.Model_status

type state = {
  grammar : Compile.t;
  stable_state : int;
  cursor : Compile.node;
  pending_input : string list;
}

let stable compiled state_id =
  let compiled_state = Compile.state compiled state_id in
  {
    grammar = compiled;
    stable_state = state_id;
    cursor = compiled_state.root;
    pending_input = [];
  }

let initialize grammar = stable grammar grammar.ir.Ir.initial
let grammar state = state.grammar
let reset state = stable state.grammar state.stable_state
let descriptor_of_state state = state.grammar.descriptor

let effect_of_transition transition input = function
  | Ir.Apply { selector; selector_id; transformation; transformation_id } ->
      Model_effect.execute ~selector_id ~transformation_id
        (Model_intent.apply ~selector ~transformation)
  | Ir.Insert_capture _ -> (
      match Input_event.text input with
      | Some text -> Model_effect.execute (Model_intent.insert_text text)
      | None ->
          invalid_arg
            ("compiled `<text>` transition matched a non-text input: "
           ^ transition.Ir.pattern))

let matching_edge node input =
  Compile.view_node node |> fun view ->
  List.find_opt
    (fun edge -> Input_event.binding_pattern_matches edge.pattern input)
    view.edges

let handle_input state input (_context : Editor_context.t) =
  match matching_edge state.cursor input with
  | None -> if state.pending_input = [] then (state, []) else (reset state, [])
  | Some edge -> (
      match (Compile.view_node edge.next).complete with
      | Some transition ->
          let next_state = stable state.grammar transition.target in
          let effects =
            List.map (effect_of_transition transition input) transition.effects
          in
          (next_state, effects)
      | None ->
          ( {
              state with
              cursor = edge.next;
              pending_input = state.pending_input @ [ edge.token ];
            },
            [] ))

let status state =
  let compiled_state = Compile.state state.grammar state.stable_state in
  let pending_input =
    match state.pending_input with
    | [] -> None
    | values -> Some (String.concat " " values)
  in
  Model_status.create ~id:compiled_state.ir.name
    ~label:compiled_state.ir.status_label ?pending_input
    ~input_mode:compiled_state.ir.input_mode ()
  |> Result.get_ok

let input_pattern edge =
  match edge.Compile.pattern with
  | Input_event.Any_text_input -> Input_rule.Text_input
  | Input_event.Exact_event _ -> Input_rule.Exact edge.token

let effect_metadata transition =
  match transition.Ir.effects with
  | Ir.Apply { selector_id; transformation_id; _ } :: _ ->
      (Some selector_id, Some transformation_id)
  | _ -> (None, None)

let summary transition =
  match transition.Ir.effects with
  | [] -> "transition to " ^ transition.target_name
  | effect :: _ -> Compile.effect_description effect

let input_rules state =
  let compiled_state = Compile.state state.grammar state.stable_state in
  let node = Compile.view_node state.cursor in
  node.edges
  |> List.mapi (fun index edge ->
      let next = Compile.view_node edge.next in
      let kind =
        match (edge.pattern, next.edges) with
        | Input_event.Any_text_input, _ -> Input_rule.Catch_all
        | _, _ :: _ -> Input_rule.Prefix
        | _ -> Input_rule.Binding
      in
      let transition = next.complete in
      let summary =
        match transition with
        | Some transition -> summary transition
        | None -> "continue " ^ edge.token
      in
      let next_status =
        Option.map (fun transition -> transition.Ir.target_name) transition
      in
      let selector_id, transformation_id =
        match transition with
        | Some transition -> effect_metadata transition
        | None -> (None, None)
      in
      Input_rule.create
        ~id:
          (Printf.sprintf "%s.%s.%d" state.grammar.ir.model_id
             compiled_state.ir.name index)
        ~pattern:(input_pattern edge) ~kind ~summary ?next_status ?selector_id
        ?transformation_id ()
      |> Result.get_ok)

module Adapter = struct
  type nonrec state = state

  let configured : Compile.t option ref = ref None
  let configure grammar = configured := Some grammar
  let grammar = grammar
  let configure_state state = configure (grammar state)
  let clear () = configured := None

  let take_configured () =
    match !configured with
    | Some grammar ->
        configured := None;
        grammar
    | None -> failwith "no validated .zenmodel grammar is configured"

  let descriptor =
    Editing_model.descriptor ~id:"zenbu.model-dsl" ~title:"Zenbu model DSL"
      ~description:"Configured declarative .zenmodel grammar" ()
    |> Result.get_ok

  let descriptor_of_state = descriptor_of_state
  let initialize _ = initialize (take_configured ())
  let handle_input = handle_input
  let reset state _ = reset state
  let status = status
  let input_rules = input_rules
end
