module Editing_model = Zenbu_model_api.Editing_model
module Input_event = Zenbu_model_api.Input_event
module Provider = Zenbu_kernel.Provider

type node = { edges : edge list; complete : Ir.transition option }

and edge = {
  pattern : Input_event.binding_pattern;
  token : string;
  next : node;
}

type node_view = { edges : edge list; complete : Ir.transition option }

let view_node (node : node) : node_view =
  { edges = node.edges; complete = node.complete }

type compiled_state = { ir : Ir.state; root : node }

type t = {
  ir : Ir.t;
  source : string;
  source_fingerprint : string;
  descriptor : Editing_model.descriptor;
  states : compiled_state list;
}

let empty_node : node = { edges = []; complete = None }

let same_pattern left right =
  String.equal
    (Input_event.binding_pattern_to_string left)
    (Input_event.binding_pattern_to_string right)

let rec insert (node : node) patterns tokens transition =
  match (patterns, tokens) with
  | [], [] -> { node with complete = Some transition }
  | pattern :: remaining_patterns, token :: remaining_tokens ->
      let rec replace found (edges : edge list) =
        match edges with
        | [] ->
            let next : node =
              insert empty_node remaining_patterns remaining_tokens transition
            in
            (false, List.rev_append found [ { pattern; token; next } ])
        | edge :: remaining when same_pattern edge.pattern pattern ->
            let next : node =
              insert edge.next remaining_patterns remaining_tokens transition
            in
            (true, List.rev_append found ({ edge with next } :: remaining))
        | edge :: remaining -> replace (edge :: found) remaining
      in
      let _, edges = replace [] node.edges in
      { node with edges }
  | _ -> invalid_arg "validated input pattern has inconsistent token count"

let compile_state (ir : Ir.state) : compiled_state =
  let root : node =
    List.fold_left
      (fun root transition ->
        insert root transition.Ir.patterns
          (String.split_on_char ' ' transition.pattern)
          transition)
      empty_node ir.Ir.transitions
  in
  { ir; root }

let make_descriptor ir =
  let provider =
    Provider.create_with_source ~id:"zenbu.model-dsl"
      ~kind:Provider.Editing_model ~source:ir.Ir.source_name
    |> Result.get_ok
  in
  Editing_model.descriptor ~id:ir.model_id ~title:ir.title
    ~description:"Declarative .zenmodel editing grammar" ~provider ()
  |> Result.get_ok

let compile ~source_name ~source =
  match Lexer.lex ~source_name ~source with
  | Error diagnostics -> Error diagnostics
  | Ok tokens -> (
      match Parser.parse ~source_name ~source tokens with
      | Error diagnostics -> Error diagnostics
      | Ok ast -> (
          match Validate.validate ~source_name ~source ast with
          | Error diagnostics -> Error diagnostics
          | Ok (ir, warnings) ->
              Ok
                ( {
                    ir;
                    source;
                    source_fingerprint = Digest.to_hex (Digest.string source);
                    descriptor = make_descriptor ir;
                    states = List.map compile_state ir.states;
                  },
                  warnings )))

let state (compiled : t) state_id =
  match
    List.find_opt
      (fun (state : compiled_state) -> state.ir.Ir.id = state_id)
      compiled.states
  with
  | Some state -> state
  | None -> invalid_arg "compiled DSL model references an unknown state"

let prefixes (state : compiled_state) =
  let rec walk prefix (node : node) values =
    List.fold_left
      (fun values edge ->
        let prefix = prefix @ [ edge.token ] in
        let values =
          if edge.next.edges = [] then values
          else String.concat " " prefix :: values
        in
        walk prefix edge.next values)
      values node.edges
  in
  List.rev (walk [] state.root [])

let effect_description = function
  | Ir.Apply { selector_id; transformation_id; _ } ->
      Printf.sprintf "apply selector %S transform %S" selector_id
        transformation_id
  | Ir.Insert_capture name -> "insert $" ^ name
