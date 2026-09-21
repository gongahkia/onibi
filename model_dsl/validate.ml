module Input_event = Zenbu_model_api.Input_event
module Model_intent = Zenbu_model_api.Model_intent
module Model_status = Zenbu_model_api.Model_status

let error ~source_name ~source span message =
  Diagnostic.make ~severity:Diagnostic.Error ~message ~source_name ~source span

let add_error errors ~source_name ~source span message =
  errors := error ~source_name ~source span message :: !errors

let find_index name states =
  let rec find index = function
    | [] -> None
    | state :: rest -> if String.equal state.Ast.name name then Some index else find (index + 1) rest
  in
  find 0 states

let is_text_pattern = function [ Input_event.Any_text_input ] -> true | _ -> false

let has_text_pattern patterns =
  List.exists
    (function Input_event.Any_text_input -> true | Input_event.Exact_event _ -> false)
    patterns

let patterns_overlap left right =
  List.length left = List.length right
  && List.for_all2 Input_event.binding_patterns_overlap left right

let is_prefix prefix complete =
  let rec loop prefix complete =
    match (prefix, complete) with
    | [], _ -> true
    | _, [] -> false
    | left :: rest_left, right :: rest_right ->
        Input_event.binding_patterns_overlap left right && loop rest_left rest_right
  in
  List.length prefix < List.length complete && loop prefix complete

let input_mode = function
  | Ast.Keys -> Model_status.Key_commands
  | Ast.Text -> Model_status.Text_entry

let validate ~source_name ~source ast =
  let errors = ref [] in
  if ast.Ast.version <> 1 then
    add_error errors ~source_name ~source ast.version_span
      (Printf.sprintf "unsupported zenbu-model version %d (expected 1)" ast.version);
  if String.length ast.model.id = 0 then
    add_error errors ~source_name ~source ast.model.id_span "model ID must not be empty";
  let title =
    match ast.model.titles with
    | [ (title, _) ] when String.length title > 0 -> Some title
    | [] ->
        add_error errors ~source_name ~source ast.model.span "model requires exactly one `title`";
        None
    | [ (_, span) ] ->
        add_error errors ~source_name ~source span "model title must not be empty";
        None
    | (_, span) :: _ ->
        add_error errors ~source_name ~source span "model has more than one `title`";
        None
  in
  let seen_states = Hashtbl.create (List.length ast.model.states) in
  List.iter
    (fun state ->
      if Hashtbl.mem seen_states state.Ast.name then
        add_error errors ~source_name ~source state.name_span
          ("duplicate state `" ^ state.name ^ "`")
      else Hashtbl.add seen_states state.name state)
    ast.model.states;
  if ast.model.states = [] then
    add_error errors ~source_name ~source ast.model.span "model requires at least one state";
  let initial_name =
    match ast.model.initials with
    | [ (name, span) ] -> Some (name, span)
    | [] ->
        add_error errors ~source_name ~source ast.model.span "model requires exactly one `initial` state";
        None
    | (_, span) :: _ ->
        add_error errors ~source_name ~source span "model has more than one `initial` state";
        None
  in
  let initial =
    match initial_name with
    | Some (name, span) -> (
        match find_index name ast.model.states with
        | Some index -> Some index
        | None ->
            add_error errors ~source_name ~source span
              ("unknown initial state `" ^ name ^ "`");
            None)
    | None -> None
  in
  let validate_effect transition effect =
    match effect with
    | Ast.Apply { selector; selector_span; transformation; transformation_span; _ } ->
        let selector =
          match Model_intent.selector_of_string selector with
          | Ok selector -> Some selector
          | Error _ ->
              add_error errors ~source_name ~source selector_span
                ("unknown selector `" ^ selector ^ "`");
              None
        in
        let transformation =
          match Model_intent.transformation_of_string transformation with
          | Ok transformation -> Some transformation
          | Error _ ->
              add_error errors ~source_name ~source transformation_span
                ("unknown transformation `" ^ transformation ^ "`");
              None
        in
        (match (selector, transformation) with
        | Some selector, Some transformation ->
            Some
              (Ir.Apply
                 {
                   selector;
                   selector_id =
                     (match effect with Ast.Apply value -> value.selector | _ -> assert false);
                   transformation;
                   transformation_id =
                     (match effect with Ast.Apply value -> value.transformation | _ -> assert false);
                 })
        | _ -> None)
    | Ast.Insert_capture { name; name_span; _ } -> (
        match transition.Ast.capture with
        | Some (capture, _) when String.equal capture name -> Some (Ir.Insert_capture name)
        | _ ->
            add_error errors ~source_name ~source name_span
              ("undefined text capture `$" ^ name ^ "`");
            None)
  in
  let compiled_states =
    List.mapi
      (fun state_id state ->
        let status =
          match state.Ast.statuses with
          | [ status ] -> Some status
          | [] ->
              add_error errors ~source_name ~source state.span
                ("state `" ^ state.name ^ "` requires exactly one `status` block");
              None
          | _ :: duplicate :: _ ->
              add_error errors ~source_name ~source duplicate.span
                ("state `" ^ state.name ^ "` has more than one `status` block");
              None
        in
        let transitions =
          List.mapi
            (fun transition_id transition ->
              let patterns =
                match Input_event.binding_pattern_sequence_of_string transition.pattern with
                | Ok patterns -> Some patterns
                | Error error ->
                    add_error errors ~source_name ~source transition.pattern_span
                      ("invalid input sequence `" ^ transition.pattern ^ "`: "
                     ^ Zenbu_kernel.Error.to_string error);
                    None
              in
              let target =
                match find_index transition.target ast.model.states with
                | Some target -> Some target
                | None ->
                    add_error errors ~source_name ~source transition.target_span
                      ("unknown state `" ^ transition.target ^ "`");
                    None
              in
              (match (status, patterns) with
              | Some { Ast.input_mode = Ast.Keys; _ }, Some patterns
                when has_text_pattern patterns ->
                  add_error errors ~source_name ~source transition.pattern_span
                    "`<text>` is only valid in a state with `input text`"
              | Some { Ast.input_mode = Ast.Text; _ }, Some patterns
                when has_text_pattern patterns && not (is_text_pattern patterns) ->
                  add_error errors ~source_name ~source transition.pattern_span
                    "`<text>` must be the entire input sequence in DSL v1"
              | _ -> ());
              (match (transition.capture, patterns) with
              | Some (_, capture_span), Some patterns when not (is_text_pattern patterns) ->
                  add_error errors ~source_name ~source capture_span
                    "`as` captures are only valid for a singleton `<text>` transition"
              | _ -> ());
              let effects =
                List.filter_map (validate_effect transition) transition.effects
              in
              match (patterns, target) with
              | Some patterns, Some target ->
                  Some
                    {
                      Ir.id = transition_id;
                      pattern = transition.pattern;
                      patterns;
                      capture = Option.map fst transition.capture;
                      target;
                      target_name = transition.target;
                      effects;
                      span = transition.span;
                      pattern_span = transition.pattern_span;
                    }
              | _ -> None)
            state.transitions
          |> List.filter_map Fun.id
        in
        (match status with
        | Some status ->
            if String.length status.label = 0 then
              add_error errors ~source_name ~source status.span
                ("state `" ^ state.name ^ "` has an empty status label");
            Some
              {
                Ir.id = state_id;
                name = state.name;
                status_label = status.label;
                input_mode = input_mode status.input_mode;
                transitions;
                span = state.span;
              }
        | None -> None))
      ast.model.states
  in
  List.iter
    (fun state ->
      let transitions = state.Ast.transitions in
      let rec compare = function
        | [] -> ()
        | transition :: rest ->
            let left = Input_event.binding_pattern_sequence_of_string transition.pattern in
            List.iter
              (fun other ->
                match (left, Input_event.binding_pattern_sequence_of_string other.pattern) with
                | Ok left, Ok right when patterns_overlap left right ->
                    add_error errors ~source_name ~source other.pattern_span
                      ("duplicate or overlapping input sequence `" ^ other.pattern
                     ^ "` in state `" ^ state.name ^ "`")
                | Ok left, Ok right when is_prefix left right ->
                    add_error errors ~source_name ~source transition.pattern_span
                      ("input sequence `" ^ transition.pattern
                     ^ "` conflicts with longer sequence `" ^ other.pattern
                     ^ "`; use an explicit intermediate state")
                | Ok left, Ok right when is_prefix right left ->
                    add_error errors ~source_name ~source other.pattern_span
                      ("input sequence `" ^ other.pattern
                     ^ "` conflicts with longer sequence `" ^ transition.pattern
                     ^ "`; use an explicit intermediate state")
                | _ -> ())
              rest;
            compare rest
      in
      compare transitions)
    ast.model.states;
  match (!errors, title, initial) with
  | [], Some title, Some initial ->
      Ok
        ( {
            Ir.version = ast.version;
            model_id = ast.model.id;
            title;
            source_name;
            states = List.filter_map Fun.id compiled_states;
            initial;
          },
          [] )
  | errors, _, _ -> Error (List.rev errors)
