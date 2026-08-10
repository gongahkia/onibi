open Zenbu_kernel

module Make (Model : Editing_model.S) = struct
  type model_state = Model.state

  type action = {
    intent : Model_intent.t;
    provenance : Provenance.t;
    selector_id : string option;
    transformation_id : string option;
  }

  type t = {
    history : History.t;
    commands : Command_registry.t;
    syntax_service : Zenbu_syntax.Syntax.Service.t option;
    clipboard : Clipboard.t;
    state : Model.state;
    input_trace : Input_event.t list;
    repeatable_intents : Model_intent.t list option;
    trace : Trace.t;
    profiler : Profiler.t;
    next_execution_id : int ref;
    last_execution : int option;
    pending_interaction : (int * int * string list) option;
  }

  type step = {
    execution_id : int;
    input : Input_event.t;
    effects : Model_effect.t list;
    intents : Model_intent.t list;
    messages : Model_effect.message list;
    change_ids : int list;
    document_version : int;
    status_before : Model_status.t;
    status_after : Model_status.t;
  }

  let trace trace event = Trace.emit_lazy trace event

  let emit_syntax trace execution_id syntax strategy =
    Trace.emit_lazy trace (fun () ->
        Trace_event.Syntax_refreshed
          {
            execution_id;
            language_id =
              Zenbu_syntax.Syntax.Language.id
                (Zenbu_syntax.Syntax.Snapshot.language syntax);
            document_version = Zenbu_syntax.Syntax.Snapshot.document_version syntax;
            strategy;
            has_error = Zenbu_syntax.Syntax.Snapshot.has_error syntax;
          })

  let syntax_strategy service =
    Zenbu_syntax.Syntax.Service.status service
    |> Zenbu_syntax.Syntax.Service.status_last_strategy
    |> Option.map Zenbu_syntax.Syntax.Service.strategy_to_string
    |> Option.value ~default:"unknown"

  let refresh_syntax ?execution_id ~trace ~profiler service snapshot =
    let result =
      Profiler.measure profiler Profiler.Syntax_update (fun () ->
          Zenbu_syntax.Syntax.Service.refresh service snapshot)
    in
    match result with
    | Ok syntax ->
        Option.iter
          (fun execution_id ->
            emit_syntax trace execution_id syntax (syntax_strategy service))
          execution_id;
        Some syntax
    | Error error ->
        Option.iter
          (fun execution_id ->
            Trace.emit_lazy trace (fun () ->
                Trace_event.Error_reported
                  {
                    execution_id;
                    reason = Zenbu_syntax.Syntax.Error.to_string error;
                  }))
          execution_id;
        None

  let make_context ?execution_id ~trace ~profiler ?syntax_service history commands
      clipboard =
    let snapshot = Document.snapshot (History.current history) in
    let syntax =
      match syntax_service with
      | None -> None
      | Some service -> refresh_syntax ?execution_id ~trace ~profiler service snapshot
    in
    Editor_context.from_snapshot ~snapshot ~clipboard
      ~commands:(Command_registry.descriptors commands) ?syntax ()

  let model_call call =
    try Ok (call ())
    with exception_ ->
      Error (Error.Model_execution_failed (Printexc.to_string exception_))

  let create ?(commands = Command_registry.empty) ?syntax_service ?trace ?profiler
      ~document () =
    let history = History.create document in
    let clipboard = Clipboard.empty in
    let trace = Option.value trace ~default:(Trace.disabled ()) in
    let profiler = Option.value profiler ~default:(Profiler.disabled ()) in
    let context =
      make_context ~trace ~profiler ?syntax_service history commands clipboard
    in
    match model_call (fun () -> Model.initialize context) with
    | Error _ as error -> error
    | Ok state ->
        Ok
          {
            history;
            commands;
            syntax_service;
            clipboard;
            state;
            input_trace = [];
            repeatable_intents = None;
            trace;
            profiler;
            next_execution_id = ref 1;
            last_execution = None;
            pending_interaction = None;
          }

  let sync_syntax_after_commit runtime ~execution_id history =
    match (runtime.syntax_service, History.current_change history) with
    | Some service, Some change ->
        let result =
          Profiler.measure runtime.profiler Profiler.Syntax_update (fun () ->
              Zenbu_syntax.Syntax.Service.update service
                ~before:(Document.snapshot (History.before change))
                ~transaction:(History.transaction change)
                ~after:(Document.snapshot (History.after change)))
        in
        (match result with
        | Ok syntax ->
            emit_syntax runtime.trace execution_id syntax (syntax_strategy service)
        | Error error ->
            trace runtime.trace (fun () ->
                Trace_event.Error_reported
                  {
                    execution_id;
                    reason = Zenbu_syntax.Syntax.Error.to_string error;
                  }))
    | None, _ | _, None -> ()

  let selection_count transaction history =
    match Transaction.selection_change transaction with
    | Some selections -> List.length (Selection_set.to_list selections)
    | None ->
        Document.snapshot (History.current history) |> Document_snapshot.selections
        |> Selection_set.to_list |> List.length

  let record_transaction runtime ~execution_id action history =
    match History.current_change history with
    | None ->
        failwith "history invariant violated: committed action has no change"
    | Some change ->
        let transaction = History.transaction change in
        Option.iter
          (fun selector_id ->
            trace runtime.trace (fun () ->
                Trace_event.Selector_resolved
                  {
                    execution_id;
                    selector_id;
                    selection_count = selection_count transaction history;
                  }))
          action.selector_id;
        Option.iter
          (fun transformation_id ->
            trace runtime.trace (fun () ->
                Trace_event.Transformation_applied
                  { execution_id; transformation_id }))
          action.transformation_id;
        let source_version =
          Document_version.to_int (Document.version (History.before change))
        in
        let result_version =
          Document_version.to_int (Document.version (History.after change))
        in
        let edit_count = List.length (Transaction.edits transaction) in
        trace runtime.trace (fun () ->
            Trace_event.Transaction_created
              {
                execution_id;
                source_version;
                edit_count;
                provenance = action.provenance;
              });
        trace runtime.trace (fun () ->
            Trace_event.Transaction_committed
              {
                execution_id;
                change_id = History.change_id change;
                source_version;
                result_version;
                edit_count;
                provenance = action.provenance;
              });
        History.change_id change

  let apply_action runtime ~execution_id history action =
    let result =
      Profiler.measure runtime.profiler Profiler.Transaction_commit (fun () ->
          History.apply_intent ~source:Transaction.User
            ~provenance:action.provenance history
            (Model_intent.to_kernel action.intent))
    in
    match result with
    | Error error ->
        trace runtime.trace (fun () ->
            Trace_event.Transaction_rejected
              { execution_id; reason = Error.to_string error });
        Error error
    | Ok history ->
        let change_id = record_transaction runtime ~execution_id action history in
        sync_syntax_after_commit runtime ~execution_id history;
        Ok (history, change_id)

  let apply_actions runtime ~execution_id history actions =
    let rec loop history ids = function
      | [] -> Ok (history, List.rev ids)
      | action :: rest -> (
          match apply_action runtime ~execution_id history action with
          | Error _ as error -> error
          | Ok (history, change_id) -> loop history (change_id :: ids) rest)
    in
    loop history [] actions

  let repeatable intents =
    if List.exists Model_intent.is_textual intents then Some intents else None

  let retain_repeatable previous intents =
    match repeatable intents with Some intents -> Some intents | None -> previous

  let action ~base ?selector_id ?transformation_id intent =
    let inferred_selector, inferred_transformation =
      Model_intent.semantic_components intent
    in
    let selector_id =
      match selector_id with Some _ -> selector_id | None -> inferred_selector
    in
    let transformation_id =
      match transformation_id with
      | Some _ -> transformation_id
      | None -> inferred_transformation
    in
    {
      intent;
      provenance =
        base
        |> (fun value ->
             match selector_id with
             | None -> value
             | Some id -> Provenance.add value (Provenance.Selector id))
        |> (fun value ->
             match transformation_id with
             | None -> value
             | Some id -> Provenance.add value (Provenance.Transformation id));
      selector_id;
      transformation_id;
    }

  let selection_set_for_selector history selector =
    let snapshot = Document.snapshot (History.current history) in
    let select =
      Model_intent.apply ~selector ~transformation:Model_intent.Select
      |> Model_intent.to_kernel
    in
    match Intent.resolve ~source:Transaction.User snapshot select with
    | Error _ as error -> error
    | Ok transaction -> (
        match Transaction.selection_change transaction with
        | Some selections -> Ok selections
        | None ->
            Error
              (Error.Model_execution_failed
                 "selecting a semantic target did not produce selections"))

  let copied_contents history selector =
    match selection_set_for_selector history selector with
    | Error _ as error -> error
    | Ok selections ->
        let contents =
          Document_snapshot.contents (Document.snapshot (History.current history))
        in
        let text =
          Selection_set.to_list selections
          |> List.map (fun selection ->
                 let range = Selection.range selection in
                 let start = Anchor.byte_offset (Range.start range) in
                 let stop = Anchor.byte_offset (Range.stop range) in
                 String.sub contents start (stop - start))
          |> String.concat ""
        in
        Ok text

  let paste_intents history entry placement =
    let contents = Clipboard.contents entry in
    match placement with
    | Clipboard.Replace -> Ok [ Model_intent.replace_selected_ranges contents ]
    | Clipboard.Before | Clipboard.After -> (
        let offsets =
          match Clipboard.kind entry with
          | Clipboard.Characterwise ->
              let snapshot = Document.snapshot (History.current history) in
              let selections = Document_snapshot.selections snapshot in
              Ok
                ( Selection_set.primary_index selections,
                  Selection_set.to_list selections
                  |> List.map (fun selection ->
                         match placement with
                         | Clipboard.Before ->
                             Selection.range selection |> Range.start
                             |> Anchor.byte_offset
                         | Clipboard.After ->
                             Selection.range selection |> Range.stop
                             |> Anchor.byte_offset
                         | Clipboard.Replace -> assert false) )
          | Clipboard.Linewise -> (
              match selection_set_for_selector history Model_intent.Current_line with
              | Error _ as error -> error
              | Ok selections ->
                  Ok
                    ( Selection_set.primary_index selections,
                      Selection_set.to_list selections
                      |> List.map (fun selection ->
                             match placement with
                             | Clipboard.Before ->
                                 Selection.range selection |> Range.start
                                 |> Anchor.byte_offset
                             | Clipboard.After ->
                                 Selection.range selection |> Range.stop
                                 |> Anchor.byte_offset
                             | Clipboard.Replace -> assert false) ))
        in
        match offsets with
        | Error _ as error -> error
        | Ok (primary, offsets) -> (
            match
              Model_intent.set_selections
                ~selections:(List.map (fun offset -> (offset, offset)) offsets)
                ~primary
            with
            | Error _ as error -> error
            | Ok set -> Ok [ set; Model_intent.insert_text contents ]))

  let effect_provenance make_base model_effect =
    Provenance.add (make_base ())
      (Provenance.Effect (Model_effect.identity model_effect))

  let command_provenance commands base invocation =
    match Command_registry.find commands (Command_invocation.id invocation) with
    | Error _ -> base
    | Ok command ->
        let descriptor = Command.descriptor command in
        Provenance.add base
          (Provenance.Command
             {
               id = Command_descriptor.id descriptor |> Command_id.to_string;
               provider = Command_descriptor.provider descriptor;
             })

  let direct_actions base model_effect intents =
    let selector_id = Model_effect.selector_id model_effect in
    let transformation_id = Model_effect.transformation_id model_effect in
    List.map (action ~base ?selector_id ?transformation_id) intents

  let interpret_effect runtime ~execution_id history clipboard repeatable_intents
      base model_effect =
    trace runtime.trace (fun () ->
        Trace_event.Model_effect
          { execution_id; effect_id = Model_effect.identity model_effect });
    match model_effect with
    | Model_effect.Execute_intent intent
    | Model_effect.Execute_intent_with { intent; _ } ->
        let base = effect_provenance base model_effect in
        let actions = direct_actions base model_effect [ intent ] in
        (match apply_actions runtime ~execution_id history actions with
        | Error _ as error -> error
        | Ok (history, changes) ->
            Ok
              ( history,
                clipboard,
                actions,
                changes,
                [],
                retain_repeatable repeatable_intents [ intent ] ))
    | Model_effect.Invoke_command invocation ->
        trace runtime.trace (fun () ->
            Trace_event.Command_invoked
              {
                execution_id;
                command_id = Command_invocation.id invocation |> Command_id.to_string;
              });
        let context =
          make_context ~execution_id ~trace:runtime.trace ~profiler:runtime.profiler
            ?syntax_service:runtime.syntax_service history runtime.commands clipboard
        in
        (match Command_registry.invoke runtime.commands ~context invocation with
        | Error _ as error -> error
        | Ok intents ->
            let base =
              command_provenance runtime.commands
                (effect_provenance base model_effect) invocation
            in
            let actions = List.map (action ~base) intents in
            match apply_actions runtime ~execution_id history actions with
            | Error _ as error -> error
            | Ok (history, changes) ->
                Ok
                  ( history,
                    clipboard,
                    actions,
                    changes,
                    [],
                    retain_repeatable repeatable_intents intents ))
    | Model_effect.Emit_message message ->
        Ok (history, clipboard, [], [], [ message ], repeatable_intents)
    | Model_effect.Copy_to_clipboard { slot; selector; kind } -> (
        match copied_contents history selector with
        | Error _ as error -> error
        | Ok contents -> (
            match Clipboard.entry ~kind ~contents with
            | Error _ as error -> error
            | Ok entry ->
                Ok
                  ( history,
                    Clipboard.store clipboard ~slot ~entry,
                    [],
                    [],
                    [],
                    repeatable_intents )))
    | Model_effect.Paste_from_clipboard { slot; placement } -> (
        match Clipboard.find clipboard ~slot with
        | None -> Error (Error.Clipboard_slot_empty (Clipboard.slot_name slot))
        | Some entry -> (
            match paste_intents history entry placement with
            | Error _ as error -> error
            | Ok intents ->
                let base = effect_provenance base model_effect in
                let actions = List.map (action ~base) intents in
                match apply_actions runtime ~execution_id history actions with
                | Error _ as error -> error
                | Ok (history, changes) ->
                    Ok
                      ( history,
                        clipboard,
                        actions,
                        changes,
                        [],
                        retain_repeatable repeatable_intents intents )))
    | Model_effect.Undo -> (
        match History.undo history with
        | Error _ as error -> error
        | Ok history ->
            trace runtime.trace (fun () ->
                Trace_event.History_changed
                  {
                    execution_id;
                    operation = "undo";
                    current_change = Option.map History.change_id (History.current_change history);
                  });
            Ok (history, clipboard, [], [], [], repeatable_intents))
    | Model_effect.Redo -> (
        match History.redo history with
        | Error _ as error -> error
        | Ok history ->
            trace runtime.trace (fun () ->
                Trace_event.History_changed
                  {
                    execution_id;
                    operation = "redo";
                    current_change = Option.map History.change_id (History.current_change history);
                  });
            Ok (history, clipboard, [], [], [], repeatable_intents))
    | Model_effect.Repeat_last_edit -> (
        match repeatable_intents with
        | None -> Error Error.No_repeatable_edit
        | Some intents ->
            let repeated =
              match intents with
              | [] -> "semantic edit"
              | intent :: _ -> Model_intent.identity intent
            in
            let base =
              effect_provenance base model_effect |> fun value ->
              Provenance.add value (Provenance.Repeat repeated)
            in
            let actions = List.map (action ~base) intents in
            match apply_actions runtime ~execution_id history actions with
            | Error _ as error -> error
            | Ok (history, changes) ->
                Ok (history, clipboard, actions, changes, [], repeatable_intents))

  let interpret_effects runtime ~execution_id history clipboard repeatable_intents
      base effects =
    let rec loop history clipboard actions changes messages repeatable_intents =
      function
      | [] ->
          Ok
            ( history,
              clipboard,
              List.rev actions,
              List.rev changes,
              List.rev messages,
              repeatable_intents )
      | model_effect :: rest -> (
          match
            interpret_effect runtime ~execution_id history clipboard
              repeatable_intents base model_effect
          with
          | Error _ as error -> error
          | Ok
              ( history,
                clipboard,
                effect_actions,
                effect_changes,
                effect_messages,
                repeatable_intents ) ->
              loop history clipboard
                (List.rev_append effect_actions actions)
                (List.rev_append effect_changes changes)
                (List.rev_append effect_messages messages)
                repeatable_intents rest)
    in
    loop history clipboard [] [] [] repeatable_intents effects

  let bounded_inputs inputs input =
    let limit = 1024 in
    let rec drop count = function
      | [] -> []
      | _ :: rest when count > 0 -> drop (count - 1) rest
      | values -> values
    in
    let excess = List.length inputs - limit + 1 in
    drop (max 0 excess) inputs @ [ input ]

  let handle_input runtime input =
    let execution_id = !(runtime.next_execution_id) in
    runtime.next_execution_id := execution_id + 1;
    let status_before = Model.status runtime.state in
    trace runtime.trace (fun () ->
        Trace_event.Input_received
          { execution_id; input = Input_event.to_string input });
    trace runtime.trace (fun () ->
        Trace_event.Model_before
          {
            execution_id;
            model_id = Editing_model.id Model.descriptor;
            status_id = Model_status.id status_before;
            status_label = Model_status.label status_before;
          });
    let context =
      make_context ~execution_id ~trace:runtime.trace ~profiler:runtime.profiler
        ?syntax_service:runtime.syntax_service runtime.history runtime.commands
        runtime.clipboard
    in
    let model_result =
      Profiler.measure runtime.profiler
        ~model_id:(Editing_model.id Model.descriptor) Profiler.Model_handle
        (fun () -> model_call (fun () -> Model.handle_input runtime.state input context))
    in
    match model_result with
    | Error error ->
        trace runtime.trace (fun () ->
            Trace_event.Error_reported
              { execution_id; reason = Error.to_string error });
        Error error
    | Ok (state, effects) ->
        let base () =
          Provenance.create ~execution_id
            ~model_id:(Editing_model.id Model.descriptor)
            ~provider:(Editing_model.provider Model.descriptor)
            ~input:(Input_event.to_string input)
          |> fun provenance ->
          match runtime.pending_interaction with
          | Some (interaction_id, _, _) ->
              Provenance.add provenance (Provenance.Interaction interaction_id)
          | None -> provenance
        in
        (match
           interpret_effects runtime ~execution_id runtime.history runtime.clipboard
             runtime.repeatable_intents base effects
         with
        | Error error ->
            trace runtime.trace (fun () ->
                Trace_event.Error_reported
                  { execution_id; reason = Error.to_string error });
            Error error
        | Ok
            ( history,
              clipboard,
              actions,
              change_ids,
              messages,
              repeatable_intents ) -> (
            match model_call (fun () -> Model.status state) with
            | Error error ->
                trace runtime.trace (fun () ->
                    Trace_event.Error_reported
                      { execution_id; reason = Error.to_string error });
                Error error
            | Ok status_after ->
                let interaction_id, started_execution, interaction_inputs =
                  match runtime.pending_interaction with
                  | Some (id, started_execution, inputs) ->
                      ( id,
                        started_execution,
                        inputs @ [ Input_event.to_string input ] )
                  | None -> (execution_id, execution_id, [ Input_event.to_string input ])
                in
                let pending_interaction =
                  match Model_status.pending_input status_after with
                  | Some _ ->
                      trace runtime.trace (fun () ->
                          Trace_event.Interaction_started
                            { execution_id; interaction_id });
                      Some (interaction_id, started_execution, interaction_inputs)
                  | None ->
                      if List.length interaction_inputs > 1 then
                        trace runtime.trace (fun () ->
                            Trace_event.Interaction_completed
                              {
                                execution_id;
                                interaction_id;
                                started_execution;
                                inputs = interaction_inputs;
                              });
                      None
                in
                trace runtime.trace (fun () ->
                    Trace_event.Model_transition
                      {
                        execution_id;
                        model_id = Editing_model.id Model.descriptor;
                        previous_status = Model_status.id status_before;
                        next_status = Model_status.id status_after;
                      });
                let next =
                  {
                    history;
                    commands = runtime.commands;
                    syntax_service = runtime.syntax_service;
                    clipboard;
                    state;
                    input_trace = bounded_inputs runtime.input_trace input;
                    repeatable_intents;
                    trace = runtime.trace;
                    profiler = runtime.profiler;
                    next_execution_id = runtime.next_execution_id;
                    last_execution = Some execution_id;
                    pending_interaction;
                  }
                in
                Ok
                  ( next,
                    {
                      execution_id;
                      input;
                      effects;
                      intents = List.map (fun action -> action.intent) actions;
                      messages;
                      change_ids;
                      document_version =
                        Document_version.to_int
                          (Document.version (History.current history));
                      status_before;
                      status_after;
                    } )))

  let reset runtime =
    let context =
      make_context ~trace:runtime.trace ~profiler:runtime.profiler
        ?syntax_service:runtime.syntax_service runtime.history runtime.commands
        runtime.clipboard
    in
    match model_call (fun () -> Model.reset runtime.state context) with
    | Error _ as error -> error
    | Ok state -> Ok { runtime with state }

  let history runtime = runtime.history
  let commands runtime = runtime.commands

  let context runtime =
    make_context ~trace:runtime.trace ~profiler:runtime.profiler
      ?syntax_service:runtime.syntax_service runtime.history runtime.commands
      runtime.clipboard

  let status runtime = Model.status runtime.state
  let model_descriptor _ = Model.descriptor
  let input_trace runtime = runtime.input_trace
  let trace runtime = runtime.trace
  let profiler runtime = runtime.profiler
  let last_execution runtime = runtime.last_execution
  let input_rules runtime = Model.input_rules runtime.state
  let execution_id step = step.execution_id
  let input step = step.input
  let effects step = step.effects
  let intents step = step.intents
  let messages step = step.messages
  let change_ids step = step.change_ids
  let document_version step = step.document_version
  let status_before step = step.status_before
  let status_after step = step.status_after
end
