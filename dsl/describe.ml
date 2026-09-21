let location source_name source span =
  let line, column =
    Diagnostic.line_column source (Source_span.start_offset span)
  in
  Printf.sprintf "%s:%d:%d" source_name line column

let input_mode = function
  | Zenbu_model_api.Model_status.Key_commands -> "keys"
  | Zenbu_model_api.Model_status.Text_entry -> "text"

let guard_name = function
  | Ir.Always -> None
  | Ir.When_selection_any_nonempty -> Some "when selection.any_nonempty"
  | Ir.Else -> Some "else"

let add_action lines indent action =
  lines :=
    (indent ^ "effect: " ^ Compile_internal.effect_description action) :: !lines

let add_transition (compiled : Compile.t) (lines : string list ref)
    (transition : Ir.transition) =
  let immediate =
    if List.length transition.Ir.patterns = 1 then "immediate"
    else "multi-event"
  in
  let line =
    Printf.sprintf "  transition: %S [%s] @ %s" transition.pattern immediate
      (location compiled.Compile_internal.ir.source_name
         compiled.Compile_internal.source transition.span)
  in
  lines := line :: !lines;
  List.iter
    (fun arm ->
      let arm_line =
        match guard_name arm.Ir.guard with
        | None -> Printf.sprintf "    -> %s" arm.target_name
        | Some guard -> Printf.sprintf "    %s -> %s" guard arm.target_name
      in
      lines := arm_line :: !lines;
      match arm.effects with
      | [] -> lines := "      effects: none" :: !lines
      | effects -> List.iter (add_action lines "      ") effects)
    transition.arms

let render ~warnings (compiled : Compile.t) =
  let lines = ref [] in
  let add line = lines := line :: !lines in
  add "Zenbu editing-model DSL";
  add ("language version: " ^ string_of_int compiled.Compile_internal.ir.version);
  add ("source: " ^ compiled.Compile_internal.ir.source_name);
  add
    ("source fingerprint (MD5; non-security): "
   ^ compiled.Compile_internal.source_fingerprint);
  add ("model: " ^ compiled.Compile_internal.ir.model_id);
  add ("title: " ^ compiled.Compile_internal.ir.title);
  List.iter
    (fun (declaration : Ir.action_declaration) ->
      add ("action: " ^ declaration.Ir.name);
      match declaration.effects with
      | [] -> add "  effects: none"
      | effects ->
          List.iter
            (fun action ->
              add ("  effect: " ^ Compile_internal.effect_description action))
            effects)
    compiled.Compile_internal.ir.actions;
  let initial =
    Compile_internal.state compiled compiled.Compile_internal.ir.initial
  in
  add ("initial state: " ^ initial.ir.name);
  List.iter
    (fun (state : Compile_internal.compiled_state) ->
      add ("state: " ^ state.ir.name);
      add
        (Printf.sprintf "  status: %s (%s)" state.ir.status_label
           (input_mode state.ir.input_mode));
      let transition_lines = ref [] in
      List.iter (add_transition compiled transition_lines) state.ir.transitions;
      List.rev !transition_lines |> List.iter add;
      List.iter
        (fun prefix -> add ("  prefix: " ^ prefix))
        (Compile_internal.prefixes state))
    compiled.Compile_internal.states;
  (match warnings with
  | [] -> add "warnings: none"
  | warnings ->
      List.iter
        (fun warning -> add ("warning: " ^ Diagnostic.format warning))
        warnings);
  String.concat "\n" (List.rev !lines) ^ "\n"
