module Command = Zenbu_model_api.Command
module Command_descriptor = Zenbu_model_api.Command_descriptor
module Command_id = Zenbu_model_api.Command_id
module Command_invocation = Zenbu_model_api.Command_invocation
module Command_registry = Zenbu_model_api.Command_registry
module Input_event = Zenbu_model_api.Input_event
module Model_intent = Zenbu_model_api.Model_intent
module Model_status = Zenbu_model_api.Model_status
module Provider = Zenbu_kernel.Provider

type transition_candidate = {
  ast : Ast.transition;
  patterns : Input_event.binding_pattern list;
  target : int;
  effects : Ir.action list;
}

type transition_group = {
  patterns : Input_event.binding_pattern list;
  mutable members : transition_candidate list;
}

let error ~source_name ~source span message =
  Diagnostic.make ~severity:Diagnostic.Error ~message ~source_name ~source span

let add_error errors ~source_name ~source span message =
  errors := error ~source_name ~source span message :: !errors

let find_index name states =
  let rec find index = function
    | [] -> None
    | state :: rest ->
        if String.equal state.Ast.name name then Some index
        else find (index + 1) rest
  in
  find 0 states

let is_text_pattern = function
  | [ Input_event.Any_text_input ] -> true
  | _ -> false

let has_text_pattern patterns =
  List.exists
    (function
      | Input_event.Any_text_input -> true | Input_event.Exact_event _ -> false)
    patterns

let patterns_overlap left right =
  List.length left = List.length right
  && List.for_all2 Input_event.binding_patterns_overlap left right

let same_patterns left right =
  List.length left = List.length right
  && List.for_all2
       (fun left right ->
         String.equal
           (Input_event.binding_pattern_to_string left)
           (Input_event.binding_pattern_to_string right))
       left right

let is_prefix prefix complete =
  let rec loop prefix complete =
    match (prefix, complete) with
    | [], _ -> true
    | _, [] -> false
    | left :: rest_left, right :: rest_right ->
        Input_event.binding_patterns_overlap left right
        && loop rest_left rest_right
  in
  List.length prefix < List.length complete && loop prefix complete

let input_mode = function
  | Ast.Keys -> Model_status.Key_commands
  | Ast.Text -> Model_status.Text_entry

let guard = function
  | Ast.Always -> Ir.Always
  | Ast.When_selection_any_nonempty _ -> Ir.When_selection_any_nonempty
  | Ast.Else _ -> Ir.Else

let is_model_safe_command descriptor =
  Command_descriptor.category descriptor = Some "selection"
  && Provider.kind (Command_descriptor.provider descriptor)
     = Provider.Editing_model

let command_action ~commands ~errors ~source_name ~source id id_span =
  match Command_id.of_string id with
  | Error command_error ->
      add_error errors ~source_name ~source id_span
        ("invalid command ID `" ^ id ^ "`: "
        ^ Zenbu_kernel.Error.to_string command_error);
      []
  | Ok command_id -> (
      match commands with
      | None ->
          add_error errors ~source_name ~source id_span
            ("command `" ^ id
           ^ "` requires a host command registry before grammar activation");
          []
      | Some commands -> (
          match Command_registry.find commands command_id with
          | Error _ ->
              add_error errors ~source_name ~source id_span
                ("unknown command `" ^ id ^ "`");
              []
          | Ok command -> (
              let descriptor = Command.descriptor command in
              if not (is_model_safe_command descriptor) then (
                add_error errors ~source_name ~source id_span
                  ("command `" ^ id
                 ^ "` is not eligible for .zenmodel; only registered "
                 ^ "selection commands from editing-model providers are allowed"
                  );
                [])
              else if
                List.exists
                  (fun parameter -> parameter.Command_descriptor.required)
                  (Command_descriptor.parameters descriptor)
              then (
                add_error errors ~source_name ~source id_span
                  ("command `" ^ id
                 ^ "` requires arguments; .zenmodel supports only "
                 ^ "no-argument commands");
                [])
              else
                match
                  Command_invocation.create ~id:command_id ~arguments:[]
                with
                | Ok invocation ->
                    [ Ir.Invoke_command { invocation; command_id = id } ]
                | Error command_error ->
                    add_error errors ~source_name ~source id_span
                      ("cannot prepare command `" ^ id ^ "`: "
                      ^ Zenbu_kernel.Error.to_string command_error);
                    [])))

let validate ?commands ~source_name ~source ast =
  let errors = ref [] in
  if ast.Ast.version <> 1 then
    add_error errors ~source_name ~source ast.version_span
      (Printf.sprintf "unsupported zenbu-model version %d (expected 1)"
         ast.version);
  if String.length ast.model.id = 0 then
    add_error errors ~source_name ~source ast.model.id_span
      "model ID must not be empty";
  let title =
    match ast.model.titles with
    | [ (title, _) ] when String.length title > 0 -> Some title
    | [] ->
        add_error errors ~source_name ~source ast.model.span
          "model requires exactly one `title`";
        None
    | [ (_, span) ] ->
        add_error errors ~source_name ~source span
          "model title must not be empty";
        None
    | (_, span) :: _ ->
        add_error errors ~source_name ~source span
          "model has more than one `title`";
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
    add_error errors ~source_name ~source ast.model.span
      "model requires at least one state";
  let initial_name =
    match ast.model.initials with
    | [ (name, span) ] -> Some (name, span)
    | [] ->
        add_error errors ~source_name ~source ast.model.span
          "model requires exactly one `initial` state";
        None
    | (_, span) :: _ ->
        add_error errors ~source_name ~source span
          "model has more than one `initial` state";
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
  let action_declarations = Hashtbl.create (List.length ast.model.actions) in
  List.iter
    (fun (declaration : Ast.action_decl) ->
      if Hashtbl.mem action_declarations declaration.Ast.name then
        add_error errors ~source_name ~source declaration.name_span
          ("duplicate action `" ^ declaration.name ^ "`")
      else Hashtbl.add action_declarations declaration.name declaration)
    ast.model.actions;
  let compile_apply selector selector_span transformation transformation_span =
    match
      ( Model_intent.selector_of_string selector,
        Model_intent.transformation_of_string transformation )
    with
    | Ok selector_value, Ok transformation_value ->
        Some
          (Ir.Apply
             {
               selector = selector_value;
               selector_id = selector;
               transformation = transformation_value;
               transformation_id = transformation;
             })
    | selector_result, transformation_result ->
        (match selector_result with
        | Error _ ->
            add_error errors ~source_name ~source selector_span
              ("unknown selector `" ^ selector ^ "`")
        | Ok _ -> ());
        (match transformation_result with
        | Error _ ->
            add_error errors ~source_name ~source transformation_span
              ("unknown transformation `" ^ transformation ^ "`")
        | Ok _ -> ());
        None
  in
  let action_cache = Hashtbl.create (List.length ast.model.actions) in
  let rec expand_action_definition visiting (declaration : Ast.action_decl) =
    match Hashtbl.find_opt action_cache declaration.Ast.name with
    | Some actions -> actions
    | None ->
        let actions =
          List.concat_map
            (compile_action_definition (declaration.Ast.name :: visiting))
            declaration.effects
        in
        Hashtbl.add action_cache declaration.name actions;
        actions
  and expand_action_reference visiting name name_span =
    if List.mem name visiting then (
      add_error errors ~source_name ~source name_span
        ("recursive action reference `" ^ name ^ "`");
      [])
    else
      match Hashtbl.find_opt action_declarations name with
      | None ->
          add_error errors ~source_name ~source name_span
            ("unknown action `" ^ name ^ "`");
          []
      | Some declaration ->
          expand_action_definition (name :: visiting) declaration
  and compile_action_definition visiting = function
    | Ast.Apply
        { selector; selector_span; transformation; transformation_span; _ } ->
        Option.to_list
          (compile_apply selector selector_span transformation
             transformation_span)
    | Ast.Insert_capture { name_span; _ } ->
        add_error errors ~source_name ~source name_span
          "actions may not reference transition captures in zenbu-model 1";
        []
    | Ast.Do { name; name_span; _ } ->
        expand_action_reference visiting name name_span
    | Ast.Command { id; id_span; _ } ->
        command_action ~commands ~errors ~source_name ~source id id_span
  in
  List.iter
    (fun (declaration : Ast.action_decl) ->
      if declaration.Ast.effects = [] then
        add_error errors ~source_name ~source declaration.span
          ("action `" ^ declaration.name ^ "` must not be empty")
      else ignore (expand_action_definition [] declaration))
    ast.model.actions;
  let compiled_action_declarations =
    List.map
      (fun (declaration : Ast.action_decl) ->
        {
          Ir.name = declaration.name;
          effects =
            Option.value ~default:[]
              (Hashtbl.find_opt action_cache declaration.name);
          span = declaration.span;
        })
      ast.model.actions
  in
  let compile_transition_action capture = function
    | Ast.Apply
        { selector; selector_span; transformation; transformation_span; _ } ->
        Option.to_list
          (compile_apply selector selector_span transformation
             transformation_span)
    | Ast.Insert_capture { name; name_span; _ } -> (
        match capture with
        | Some (capture, _) when String.equal capture name ->
            [ Ir.Insert_capture name ]
        | _ ->
            add_error errors ~source_name ~source name_span
              ("undefined text capture `$" ^ name ^ "`");
            [])
    | Ast.Do { name; name_span; _ } -> expand_action_reference [] name name_span
    | Ast.Command { id; id_span; _ } ->
        command_action ~commands ~errors ~source_name ~source id id_span
  in
  let compiled_states =
    List.mapi
      (fun state_id (state : Ast.state) ->
        let status =
          match state.Ast.statuses with
          | [ status ] -> Some status
          | [] ->
              add_error errors ~source_name ~source state.span
                ("state `" ^ state.name
               ^ "` requires exactly one `status` block");
              None
          | _ :: duplicate :: _ ->
              add_error errors ~source_name ~source duplicate.span
                ("state `" ^ state.name ^ "` has more than one `status` block");
              None
        in
        let candidates =
          List.filter_map
            (fun (transition : Ast.transition) ->
              let patterns =
                match
                  Input_event.binding_pattern_sequence_of_string
                    transition.pattern
                with
                | Ok patterns -> Some patterns
                | Error input_error ->
                    add_error errors ~source_name ~source
                      transition.pattern_span
                      ("invalid input sequence `" ^ transition.pattern ^ "`: "
                      ^ Zenbu_kernel.Error.to_string input_error);
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
                when has_text_pattern patterns && not (is_text_pattern patterns)
                ->
                  add_error errors ~source_name ~source transition.pattern_span
                    "`<text>` must be the entire input sequence in DSL v1"
              | _ -> ());
              (match (transition.capture, patterns) with
              | Some (_, capture_span), Some patterns
                when not (is_text_pattern patterns) ->
                  add_error errors ~source_name ~source capture_span
                    "`as` captures are only valid for a singleton `<text>` \
                     transition"
              | _ -> ());
              match (patterns, target) with
              | Some patterns, Some target ->
                  let effects =
                    List.concat_map
                      (compile_transition_action transition.capture)
                      transition.effects
                  in
                  Some { ast = transition; patterns; target; effects }
              | _ -> None)
            state.transitions
        in
        let groups =
          List.fold_left
            (fun groups (candidate : transition_candidate) ->
              match
                List.find_opt
                  (fun group -> same_patterns candidate.patterns group.patterns)
                  groups
              with
              | Some group ->
                  group.members <- group.members @ [ candidate ];
                  groups
              | None ->
                  groups
                  @ [
                      { patterns = candidate.patterns; members = [ candidate ] };
                    ])
            [] candidates
        in
        let transitions =
          List.mapi
            (fun id group ->
              let members = group.members in
              let has_always =
                List.exists
                  (fun candidate ->
                    match candidate.ast.guard with
                    | Ast.Always -> true
                    | _ -> false)
                  members
              in
              if has_always && List.length members > 1 then
                if
                  List.for_all
                    (fun candidate ->
                      match candidate.ast.guard with
                      | Ast.Always -> true
                      | Ast.When_selection_any_nonempty _ | Ast.Else _ -> false)
                    members
                then
                  List.tl members
                  |> List.iter (fun candidate ->
                      add_error errors ~source_name ~source
                        candidate.ast.pattern_span
                        ("duplicate or overlapping input sequence `"
                       ^ candidate.ast.pattern ^ "` in state `" ^ state.name
                       ^ "`"))
                else
                  List.iter
                    (fun candidate ->
                      add_error errors ~source_name ~source candidate.ast.span
                        "an unguarded transition cannot coexist with guarded \
                         arms for the same input sequence")
                    members;
              let when_seen = ref false in
              let else_seen = ref false in
              List.iteri
                (fun index candidate ->
                  match candidate.ast.guard with
                  | Ast.When_selection_any_nonempty span ->
                      if !when_seen then
                        add_error errors ~source_name ~source span
                          "duplicate guard `selection.any_nonempty` for the \
                           same input sequence"
                      else when_seen := true
                  | Ast.Else span ->
                      if !else_seen then
                        add_error errors ~source_name ~source span
                          "duplicate `else` arm for the same input sequence"
                      else else_seen := true;
                      if index <> List.length members - 1 then
                        add_error errors ~source_name ~source span
                          "`else` must be the final arm for an input sequence"
                  | Ast.Always -> ())
                members;
              if (not has_always) && not !when_seen then
                add_error errors ~source_name ~source (List.hd members).ast.span
                  "a guarded transition group requires at least one `when` arm";
              let first = List.hd members in
              {
                Ir.id;
                pattern = first.ast.pattern;
                patterns = group.patterns;
                arms =
                  List.map
                    (fun candidate ->
                      {
                        Ir.guard = guard candidate.ast.guard;
                        target = candidate.target;
                        target_name = candidate.ast.target;
                        effects = candidate.effects;
                        span = candidate.ast.span;
                      })
                    members;
                span = first.ast.span;
                pattern_span = first.ast.pattern_span;
              })
            groups
        in
        match status with
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
        | None -> None)
      ast.model.states
  in
  List.iter
    (fun (state : Ast.state) ->
      let rec compare = function
        | [] -> ()
        | (transition : Ast.transition) :: rest ->
            let left =
              Input_event.binding_pattern_sequence_of_string transition.pattern
            in
            List.iter
              (fun (other : Ast.transition) ->
                match
                  ( left,
                    Input_event.binding_pattern_sequence_of_string other.pattern
                  )
                with
                | Ok left, Ok right when patterns_overlap left right ->
                    if same_patterns left right then ()
                    else
                      add_error errors ~source_name ~source other.pattern_span
                        ("overlapping input sequence `" ^ other.pattern
                       ^ "` in state `" ^ state.name ^ "`")
                | Ok left, Ok right when is_prefix left right ->
                    add_error errors ~source_name ~source
                      transition.pattern_span
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
      compare state.transitions)
    ast.model.states;
  match (!errors, title, initial) with
  | [], Some title, Some initial ->
      Ok
        ( {
            Ir.version = ast.version;
            model_id = ast.model.id;
            title;
            source_name;
            actions = compiled_action_declarations;
            states = List.filter_map Fun.id compiled_states;
            initial;
          },
          [] )
  | errors, _, _ -> Error (List.rev errors)
