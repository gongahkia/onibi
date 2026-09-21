let location source_name source span =
  let line, column =
    Diagnostic.line_column source (Source_span.start_offset span)
  in
  Printf.sprintf "%s:%d:%d" source_name line column

let input_mode = function
  | Zenbu_model_api.Model_status.Key_commands -> "keys"
  | Zenbu_model_api.Model_status.Text_entry -> "text"

let add_transition (compiled : Compile.t) (lines : string list ref)
    (transition : Ir.transition) =
  let immediate =
    if List.length transition.Ir.patterns = 1 then "immediate"
    else "multi-event"
  in
  let line =
    Printf.sprintf "  transition: %S -> %s [%s] @ %s" transition.pattern
      transition.target_name immediate
      (location compiled.ir.source_name compiled.source transition.span)
  in
  let effects =
    match transition.effects with
    | [] -> [ "    effects: none" ]
    | effects ->
        List.map
          (fun action -> "    effect: " ^ Compile.effect_description action)
          effects
  in
  lines := List.rev_append effects (line :: !lines)

let render ~warnings (compiled : Compile.t) =
  let lines = ref [] in
  let add line = lines := line :: !lines in
  add "Zenbu editing-model DSL";
  add ("language version: " ^ string_of_int compiled.ir.version);
  add ("source: " ^ compiled.ir.source_name);
  add ("source fingerprint (MD5; non-security): " ^ compiled.source_fingerprint);
  add ("model: " ^ compiled.ir.model_id);
  add ("title: " ^ compiled.ir.title);
  let initial = Compile.state compiled compiled.ir.initial in
  add ("initial state: " ^ initial.ir.name);
  List.iter
    (fun (state : Compile.compiled_state) ->
      add ("state: " ^ state.Compile.ir.name);
      add
        (Printf.sprintf "  status: %s (%s)" state.ir.status_label
           (input_mode state.ir.input_mode));
      let transition_lines = ref [] in
      List.iter (add_transition compiled transition_lines) state.ir.transitions;
      List.rev !transition_lines |> List.iter add;
      List.iter
        (fun prefix -> add ("  prefix: " ^ prefix))
        (Compile.prefixes state))
    compiled.states;
  (match warnings with
  | [] -> add "warnings: none"
  | warnings ->
      List.iter
        (fun warning -> add ("warning: " ^ Diagnostic.format warning))
        warnings);
  String.concat "\n" (List.rev !lines) ^ "\n"
