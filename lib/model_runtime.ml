open Zenbu_kernel

type shared_state = {
  history : History.t;
  commands : Command_registry.t;
  semantic_behaviors : Semantic_behavior_registry.t;
  syntax_service : Zenbu_syntax.Syntax.Service.t option;
  clipboard : Clipboard.t;
  macro_recording_register : string option;
  input_trace : Input_event.t list;
  repeatable_intents : Model_intent.t list option;
  trace : Trace.t;
  profiler : Profiler.t;
  next_execution_id : int ref;
  last_execution : int option;
}

module Make (Model : Editing_model.S) = struct
  type model_state = Model.state

  type action = {
    intent : Model_intent.t option;
    provenance : Provenance.t;
    selector_id : string option;
    transformation_id : string option;
  }

  type t = {
    history : History.t;
    commands : Command_registry.t;
    semantic_behaviors : Semantic_behavior_registry.t;
    syntax_service : Zenbu_syntax.Syntax.Service.t option;
    clipboard : Clipboard.t;
    macro_recording_register : string option;
    model_descriptor : Editing_model.descriptor;
    state : Model.state;
    input_trace : Input_event.t list;
    repeatable_intents : Model_intent.t list option;
    trace : Trace.t;
    profiler : Profiler.t;
    next_execution_id : int ref;
    last_execution : int option;
    pending_interaction : (int * int * Input_event.t list) option;
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

  let extension_provider provider =
    match Provider.kind provider with
    | Provider.Script | Provider.Plugin -> true
    | Provider.Builtin | Provider.Editing_model | Provider.Syntax
    | Provider.Application ->
        false

  let trace_extension_callback runtime ~execution_id ~kind ~provider
      ?semantic_id ?reason outcome =
    if extension_provider provider then
      trace runtime.trace (fun () ->
          match Provider.kind provider with
          | Provider.Script ->
              Trace_event.Script_callback
                { execution_id; kind; provider; semantic_id; outcome; reason }
          | Provider.Plugin ->
              Trace_event.Extension_callback
                { execution_id; kind; provider; semantic_id; outcome; reason }
          | Provider.Builtin | Provider.Editing_model | Provider.Syntax
          | Provider.Application ->
              assert false)

  let trace_capability_denied runtime ~execution_id ~provider = function
    | Error.Extension_error
        {
          code = Error.Capability_denied;
          operation = Some operation;
          required = Some required;
          granted;
          _;
        } ->
        trace runtime.trace (fun () ->
            Trace_event.Capability_denied
              { execution_id; provider; operation; required; granted })
    | _ -> ()

  let extension_stage provider ~builtin ~script ~plugin =
    match Provider.kind provider with
    | Provider.Script -> script
    | Provider.Plugin -> plugin
    | Provider.Builtin | Provider.Editing_model | Provider.Syntax
    | Provider.Application ->
        builtin

  let emit_syntax trace execution_id syntax strategy =
    Trace.emit_lazy trace (fun () ->
        Trace_event.Syntax_refreshed
          {
            execution_id;
            language_id =
              Zenbu_syntax.Syntax.Language.id
                (Zenbu_syntax.Syntax.Snapshot.language syntax);
            document_version =
              Zenbu_syntax.Syntax.Snapshot.document_version syntax;
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

  let make_context ?execution_id ~trace ~profiler ?syntax_service
      ?macro_recording_register history commands clipboard =
    let snapshot = Document.snapshot (History.current history) in
    let syntax =
      match syntax_service with
      | None -> None
      | Some service ->
          refresh_syntax ?execution_id ~trace ~profiler service snapshot
    in
    Editor_context.from_snapshot ~snapshot ~clipboard ?macro_recording_register
      ~commands:(Command_registry.descriptors commands)
      ?syntax ()

  let model_call call =
    try Ok (call ())
    with exception_ ->
      Error (Error.Model_execution_failed (Printexc.to_string exception_))

  let create ?(commands = Command_registry.empty)
      ?(semantic_behaviors = Semantic_behavior_registry.empty) ?syntax_service
      ?trace ?profiler ~document () =
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
            semantic_behaviors;
            syntax_service;
            clipboard;
            macro_recording_register = None;
            model_descriptor = Model.descriptor_of_state state;
            state;
            input_trace = [];
            repeatable_intents = None;
            trace;
            profiler;
            next_execution_id = ref 1;
            last_execution = None;
            pending_interaction = None;
          }

  let shared_state runtime =
    {
      history = runtime.history;
      commands = runtime.commands;
      semantic_behaviors = runtime.semantic_behaviors;
      syntax_service = runtime.syntax_service;
      clipboard = runtime.clipboard;
      macro_recording_register = runtime.macro_recording_register;
      input_trace = runtime.input_trace;
      repeatable_intents = runtime.repeatable_intents;
      trace = runtime.trace;
      profiler = runtime.profiler;
      next_execution_id = runtime.next_execution_id;
      last_execution = runtime.last_execution;
    }

  let create_from_shared (shared : shared_state) =
    let context =
      make_context ~trace:shared.trace ~profiler:shared.profiler
        ?syntax_service:shared.syntax_service
        ?macro_recording_register:shared.macro_recording_register shared.history
        shared.commands shared.clipboard
    in
    match model_call (fun () -> Model.initialize context) with
    | Error _ as error -> error
    | Ok state ->
        Ok
          {
            history = shared.history;
            commands = shared.commands;
            semantic_behaviors = shared.semantic_behaviors;
            syntax_service = shared.syntax_service;
            clipboard = shared.clipboard;
            macro_recording_register = shared.macro_recording_register;
            model_descriptor = Model.descriptor_of_state state;
            state;
            input_trace = shared.input_trace;
            repeatable_intents = shared.repeatable_intents;
            trace = shared.trace;
            profiler = shared.profiler;
            next_execution_id = shared.next_execution_id;
            last_execution = shared.last_execution;
            pending_interaction = None;
          }

  let with_model_state runtime state =
    {
      runtime with
      model_descriptor = Model.descriptor_of_state state;
      state;
      pending_interaction = None;
    }

  let with_syntax_service runtime ~syntax_service =
    let shared = shared_state runtime in
    create_from_shared { shared with syntax_service }

  let with_macro_recording_register runtime macro_recording_register =
    { runtime with macro_recording_register }

  let with_kill_ring runtime kill_ring =
    {
      runtime with
      clipboard = Clipboard.with_kill_ring runtime.clipboard kill_ring;
    }

  let kill_ring runtime = Clipboard.kill_ring_entries runtime.clipboard

  let sync_syntax_after_commit runtime ~execution_id history =
    match (runtime.syntax_service, History.current_change history) with
    | Some service, Some change -> (
        let result =
          Profiler.measure runtime.profiler Profiler.Syntax_update (fun () ->
              Zenbu_syntax.Syntax.Service.update service
                ~before:(Document.snapshot (History.before change))
                ~transaction:(History.transaction change)
                ~after:(Document.snapshot (History.after change)))
        in
        match result with
        | Ok syntax ->
            emit_syntax runtime.trace execution_id syntax
              (syntax_strategy service)
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
        Document.snapshot (History.current history)
        |> Document_snapshot.selections |> Selection_set.to_list |> List.length

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

  let apply_transaction runtime ~execution_id history action transaction =
    let result =
      Profiler.measure runtime.profiler Profiler.Transaction_commit (fun () ->
          History.commit history transaction)
    in
    match result with
    | Error error ->
        trace runtime.trace (fun () ->
            Trace_event.Transaction_rejected
              { execution_id; reason = Error.to_string error });
        Error error
    | Ok history ->
        let change_id =
          record_transaction runtime ~execution_id action history
        in
        sync_syntax_after_commit runtime ~execution_id history;
        Ok (history, change_id)

  let apply_action runtime ~execution_id history action =
    match action.intent with
    | None ->
        Error
          (Error.Model_execution_failed
             "dynamic semantic action requires a resolved transaction")
    | Some intent ->
        let transaction =
          Intent.resolve ~source:Transaction.User ~provenance:action.provenance
            (Document.snapshot (History.current history))
            (Model_intent.to_kernel intent)
        in
        Result.bind transaction
          (apply_transaction runtime ~execution_id history action)

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
    match repeatable intents with
    | Some intents -> Some intents
    | None -> previous

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
      intent = Some intent;
      provenance =
        ( ( base |> fun value ->
            match selector_id with
            | None -> value
            | Some id -> Provenance.add value (Provenance.Selector id) )
        |> fun value ->
          match transformation_id with
          | None -> value
          | Some id -> Provenance.add value (Provenance.Transformation id) );
      selector_id;
      transformation_id;
    }

  let dynamic_action ~base ~selector_id ~transformation_id =
    {
      intent = None;
      provenance =
        ( base |> fun value ->
          Provenance.add value (Provenance.Selector selector_id) |> fun value ->
          Provenance.add value (Provenance.Transformation transformation_id) );
      selector_id = Some selector_id;
      transformation_id = Some transformation_id;
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

  let behavior_selection_set snapshot (value : Semantic_behavior.selection_set)
      =
    let rec selections values = function
      | [] -> Ok (List.rev values)
      | selection :: rest -> (
          match
            Document_snapshot.anchor snapshot
              ~byte_offset:selection.Semantic_behavior.anchor_offset
          with
          | Error _ as error -> error
          | Ok anchor -> (
              match
                Document_snapshot.anchor snapshot
                  ~byte_offset:selection.Semantic_behavior.head_offset
              with
              | Error _ as error -> error
              | Ok head -> (
                  match Selection.make ~anchor ~head with
                  | Error _ as error -> error
                  | Ok selection -> selections (selection :: values) rest)))
    in
    match selections [] value.Semantic_behavior.selections with
    | Error _ as error -> error
    | Ok selections -> Selection_set.create ~primary:value.primary selections

  let behavior_selections selections =
    {
      Semantic_behavior.selections =
        Selection_set.to_list selections
        |> List.map (fun selection ->
            {
              Semantic_behavior.anchor_offset =
                Selection.anchor selection |> Anchor.byte_offset;
              head_offset = Selection.head selection |> Anchor.byte_offset;
            });
      primary = Selection_set.primary_index selections;
    }

  let protected_behavior_call phase call =
    try call ()
    with exception_ ->
      Error
        (Error.Script_error
           {
             phase;
             source = None;
             line = None;
             message = Printexc.to_string exception_;
           })

  let resolve_operation_selector runtime ~execution_id history context selector
      =
    match selector with
    | Semantic_operation.Builtin_selector selector ->
        selection_set_for_selector history selector
    | Semantic_operation.Registered_selector { id; arguments } -> (
        match
          Semantic_behavior_registry.find_selector runtime.semantic_behaviors id
        with
        | None ->
            Error (Error.Invalid_selector ("unknown registered selector " ^ id))
        | Some entry ->
            let provider =
              Semantic_behavior.selector_descriptor entry
              |> Semantic_descriptor.provider
            in
            trace_extension_callback runtime ~execution_id ~kind:"selector"
              ~provider ~semantic_id:id "started";
            let result =
              Profiler.measure runtime.profiler
                ~model_id:
                  (if extension_provider provider then id else "builtin")
                (extension_stage provider ~builtin:Profiler.Selector_resolve
                   ~script:Profiler.Script_selector
                   ~plugin:Profiler.Extension_selector)
                (fun () ->
                  protected_behavior_call "selector" (fun () ->
                      Semantic_behavior.run_selector entry context ~arguments))
            in
            let resolved =
              Result.bind result
                (behavior_selection_set
                   (Document.snapshot (History.current history)))
            in
            (match resolved with
            | Ok _ ->
                trace_extension_callback runtime ~execution_id ~kind:"selector"
                  ~provider ~semantic_id:id "succeeded"
            | Error error ->
                trace_capability_denied runtime ~execution_id ~provider error;
                trace_extension_callback runtime ~execution_id ~kind:"selector"
                  ~provider ~semantic_id:id ~reason:(Error.to_string error)
                  "failed");
            resolved)

  let edit_for_behavior snapshot edit =
    match
      Document_snapshot.anchor snapshot
        ~byte_offset:edit.Semantic_behavior.start_offset
    with
    | Error _ as error -> error
    | Ok start -> (
        match
          Document_snapshot.anchor snapshot
            ~byte_offset:edit.Semantic_behavior.stop_offset
        with
        | Error _ as error -> error
        | Ok stop -> (
            match Range.make ~start ~stop with
            | Error _ as error -> error
            | Ok range -> Edit.replace range ~text:edit.replacement))

  let behavior_transaction snapshot ~provenance ~operation selections
      (result : Semantic_behavior.transformation_result) =
    let rec edits values = function
      | [] -> Ok (List.rev values)
      | edit :: rest -> (
          match edit_for_behavior snapshot edit with
          | Error _ as error -> error
          | Ok edit -> edits (edit :: values) rest)
    in
    let selection_change =
      match result.Semantic_behavior.selections with
      | None -> Ok selections
      | Some value -> behavior_selection_set snapshot value
    in
    match (edits [] result.Semantic_behavior.edits, selection_change) with
    | (Error _ as error), _ -> error
    | _, (Error _ as error) -> error
    | Ok edits, Ok selection_change ->
        let metadata =
          Transaction.metadata ~source:Transaction.User
            ~intent:(Semantic_operation.identity operation)
            ~provenance ()
        in
        Transaction.create
          ~document_id:(Document_snapshot.document_id snapshot)
          ~source_version:(Document_snapshot.version snapshot)
          ~edits ~selection_change ~metadata ()

  let resolve_operation_transaction runtime ~execution_id history context
      (operation : Semantic_operation.t) provenance =
    let snapshot = Document.snapshot (History.current history) in
    match
      resolve_operation_selector runtime ~execution_id history context
        operation.selector
    with
    | Error _ as error -> error
    | Ok selections -> (
        match operation.transformation with
        | Semantic_operation.Builtin_transformation transformation ->
            Intent.resolve_on_selections ~source:Transaction.User ~provenance
              ~intent:(Semantic_operation.identity operation)
              snapshot selections
              (Model_intent.transformation_to_kernel transformation)
        | Semantic_operation.Registered_transformation { id; arguments } -> (
            match
              Semantic_behavior_registry.find_transformation
                runtime.semantic_behaviors id
            with
            | None ->
                Error
                  (Error.Invalid_transformation
                     ("unknown registered transformation " ^ id))
            | Some entry ->
                let provider =
                  Semantic_behavior.transformation_descriptor entry
                  |> Semantic_descriptor.provider
                in
                trace_extension_callback runtime ~execution_id
                  ~kind:"transformation" ~provider ~semantic_id:id "started";
                let result =
                  Profiler.measure runtime.profiler
                    ~model_id:
                      (if extension_provider provider then id else "builtin")
                    (extension_stage provider
                       ~builtin:Profiler.Transformation_apply
                       ~script:Profiler.Script_transformation
                       ~plugin:Profiler.Extension_transformation)
                    (fun () ->
                      protected_behavior_call "transformation" (fun () ->
                          Semantic_behavior.run_transformation entry context
                            ~selections:(behavior_selections selections)
                            ~arguments))
                in
                let resolved =
                  Result.bind result
                    (behavior_transaction snapshot ~provenance ~operation
                       selections)
                in
                (match resolved with
                | Ok _ ->
                    trace_extension_callback runtime ~execution_id
                      ~kind:"transformation" ~provider ~semantic_id:id
                      "succeeded"
                | Error error ->
                    trace_capability_denied runtime ~execution_id ~provider
                      error;
                    trace_extension_callback runtime ~execution_id
                      ~kind:"transformation" ~provider ~semantic_id:id
                      ~reason:(Error.to_string error) "failed");
                resolved))

  let explicit_selection_set snapshot ~selections ~primary =
    match Model_intent.set_selections ~selections ~primary with
    | Error _ as error -> error
    | Ok intent -> (
        match
          Intent.resolve ~source:Transaction.User snapshot
            (Model_intent.to_kernel intent)
        with
        | Error _ as error -> error
        | Ok transaction -> (
            match Transaction.selection_change transaction with
            | Some selections -> Ok selections
            | None ->
                Error
                  (Error.Model_execution_failed
                     "explicit selections did not produce a selection set")))

  let copied_selection_contents snapshot selections =
    let contents = Document_snapshot.contents snapshot in
    Selection_set.to_list selections
    |> List.map (fun selection ->
        let range = Selection.range selection in
        let start = Anchor.byte_offset (Range.start range) in
        let stop = Anchor.byte_offset (Range.stop range) in
        String.sub contents start (stop - start))
    |> String.concat ""

  let copied_contents history selector =
    match selection_set_for_selector history selector with
    | Error _ as error -> error
    | Ok selections ->
        Ok
          (copied_selection_contents
             (Document.snapshot (History.current history))
             selections)

  let cut_to_clipboard runtime ~execution_id history clipboard base ~slot
      ~selector ~kind =
    let snapshot = Document.snapshot (History.current history) in
    match selection_set_for_selector history selector with
    | Error _ as error -> error
    | Ok selections -> (
        let contents = copied_selection_contents snapshot selections in
        if String.length contents = 0 then
          Error
            (Error.Invalid_command_arguments
               "cut requires at least one non-empty selected range")
        else
          match Clipboard.entry ~kind ~contents with
          | Error _ as error -> error
          | Ok entry -> (
              let selector_id =
                Model_intent.selector_to_kernel selector |> Selector.to_string
              in
              let transformation_id =
                Model_intent.transformation_to_kernel Model_intent.Delete
                |> Transformation.name
              in
              let action =
                dynamic_action ~base ~selector_id ~transformation_id
              in
              let transaction =
                Intent.resolve_on_selections ~source:Transaction.User
                  ~provenance:action.provenance ~intent:"cut-to-clipboard"
                  snapshot selections
                  (Model_intent.transformation_to_kernel Model_intent.Delete)
              in
              match transaction with
              | Error _ as error -> error
              | Ok transaction ->
                  apply_transaction runtime ~execution_id history action
                    transaction
                  |> Result.map (fun (history, change_id) ->
                      ( history,
                        Clipboard.store_kill clipboard ~slot ~entry,
                        change_id ))))

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
              match
                selection_set_for_selector history Model_intent.Current_line
              with
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

  let rec interpret_effect runtime ~execution_id history clipboard
      repeatable_intents base model_effect =
    trace runtime.trace (fun () ->
        Trace_event.Model_effect
          { execution_id; effect_id = Model_effect.identity model_effect });
    match model_effect with
    | Model_effect.Execute_intent intent
    | Model_effect.Execute_intent_with { intent; _ } -> (
        let base = effect_provenance base model_effect in
        let actions = direct_actions base model_effect [ intent ] in
        match apply_actions runtime ~execution_id history actions with
        | Error _ as error -> error
        | Ok (history, changes) ->
            Ok
              ( history,
                clipboard,
                actions,
                changes,
                [],
                retain_repeatable repeatable_intents [ intent ] ))
    | Model_effect.Execute_semantic_operation operation -> (
        let base = effect_provenance base model_effect in
        let selector_id = Semantic_operation.selector_id operation.selector in
        let transformation_id =
          Semantic_operation.transformation_id operation.transformation
        in
        let context =
          make_context ~execution_id ~trace:runtime.trace
            ~profiler:runtime.profiler ?syntax_service:runtime.syntax_service
            ?macro_recording_register:runtime.macro_recording_register history
            runtime.commands clipboard
        in
        let action = dynamic_action ~base ~selector_id ~transformation_id in
        match
          resolve_operation_transaction runtime ~execution_id history context
            operation action.provenance
        with
        | Error _ as error -> error
        | Ok transaction -> (
            match
              apply_transaction runtime ~execution_id history action transaction
            with
            | Error _ as error -> error
            | Ok (history, change_id) ->
                Ok
                  ( history,
                    clipboard,
                    [ action ],
                    [ change_id ],
                    [],
                    repeatable_intents )))
    | Model_effect.Apply_to_selections
        { selections; primary; selector_id; action = selection_action } -> (
        let snapshot = Document.snapshot (History.current history) in
        match explicit_selection_set snapshot ~selections ~primary with
        | Error _ as error -> error
        | Ok selected -> (
            match selection_action with
            | Model_effect.Copy { slot; kind } -> (
                let contents = copied_selection_contents snapshot selected in
                match Clipboard.entry ~kind ~contents with
                | Error _ as error -> error
                | Ok entry ->
                    Ok
                      ( history,
                        Clipboard.store clipboard ~slot ~entry,
                        [],
                        [],
                        [],
                        repeatable_intents ))
            | Model_effect.Transform transformation -> (
                let base = effect_provenance base model_effect in
                let transformation_id =
                  Zenbu_kernel.Transformation.name
                    (Model_intent.transformation_to_kernel transformation)
                in
                let action =
                  dynamic_action ~base ~selector_id ~transformation_id
                in
                let transaction =
                  Intent.resolve_on_selections ~source:Transaction.User
                    ~provenance:action.provenance
                    ~intent:(Model_effect.identity model_effect)
                    snapshot selected
                    (Model_intent.transformation_to_kernel transformation)
                in
                match transaction with
                | Error _ as error -> error
                | Ok transaction -> (
                    match
                      apply_transaction runtime ~execution_id history action
                        transaction
                    with
                    | Error _ as error -> error
                    | Ok (history, change_id) ->
                        Ok
                          ( history,
                            clipboard,
                            [ action ],
                            [ change_id ],
                            [],
                            repeatable_intents )))))
    | Model_effect.Invoke_command invocation -> (
        let command_id =
          Command_invocation.id invocation |> Command_id.to_string
        in
        let extension_source =
          match
            Command_registry.find runtime.commands
              (Command_invocation.id invocation)
          with
          | Error _ -> None
          | Ok command ->
              let provider =
                Command.descriptor command |> Command_descriptor.provider
              in
              if extension_provider provider then Some provider else None
        in
        trace runtime.trace (fun () ->
            Trace_event.Command_invoked { execution_id; command_id });
        Option.iter
          (fun provider ->
            trace_extension_callback runtime ~execution_id ~kind:"command"
              ~provider ~semantic_id:command_id "started")
          extension_source;
        let context =
          make_context ~execution_id ~trace:runtime.trace
            ~profiler:runtime.profiler ?syntax_service:runtime.syntax_service
            ?macro_recording_register:runtime.macro_recording_register history
            runtime.commands clipboard
        in
        let invoked =
          match extension_source with
          | None ->
              Command_registry.invoke_effects runtime.commands ~context
                invocation
          | Some provider ->
              Profiler.measure runtime.profiler ~model_id:command_id
                (extension_stage provider ~builtin:Profiler.Model_handle
                   ~script:Profiler.Script_command
                   ~plugin:Profiler.Extension_command) (fun () ->
                  Command_registry.invoke_effects runtime.commands ~context
                    invocation)
        in
        match invoked with
        | Error error ->
            Option.iter
              (fun provider ->
                trace_capability_denied runtime ~execution_id ~provider error;
                trace_extension_callback runtime ~execution_id ~kind:"command"
                  ~provider ~semantic_id:command_id
                  ~reason:(Error.to_string error) "failed")
              extension_source;
            Error error
        | Ok effects -> (
            let base =
              command_provenance runtime.commands
                (effect_provenance base model_effect)
                invocation
            in
            match
              interpret_effects runtime ~execution_id history clipboard
                repeatable_intents
                (fun () -> base)
                effects
            with
            | Error error ->
                Option.iter
                  (fun provider ->
                    trace_capability_denied runtime ~execution_id ~provider
                      error;
                    trace_extension_callback runtime ~execution_id
                      ~kind:"command" ~provider ~semantic_id:command_id
                      ~reason:(Error.to_string error) "failed")
                  extension_source;
                Error error
            | Ok
                ( history,
                  clipboard,
                  actions,
                  changes,
                  messages,
                  repeatable_intents ) ->
                Option.iter
                  (fun provider ->
                    trace_extension_callback runtime ~execution_id
                      ~kind:"command" ~provider ~semantic_id:command_id
                      "succeeded")
                  extension_source;
                Ok
                  ( history,
                    clipboard,
                    actions,
                    changes,
                    messages,
                    repeatable_intents )))
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
    | Model_effect.Cut_to_clipboard { slot; selector; kind } -> (
        let base = effect_provenance base model_effect in
        match
          cut_to_clipboard runtime ~execution_id history clipboard base ~slot
            ~selector ~kind
        with
        | Error _ as error -> error
        | Ok (history, clipboard, change_id) ->
            Ok
              ( history,
                clipboard,
                [],
                [ change_id ],
                [],
                retain_repeatable repeatable_intents
                  [ Model_intent.delete_selected_ranges ] ))
    | Model_effect.Paste_from_clipboard { slot; placement } -> (
        match Clipboard.find clipboard ~slot with
        | None -> Error (Error.Clipboard_slot_empty (Clipboard.slot_name slot))
        | Some entry -> (
            match paste_intents history entry placement with
            | Error _ as error -> error
            | Ok intents -> (
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
                        retain_repeatable repeatable_intents intents ))))
    | Model_effect.Paste_from_kill_ring { index; placement } -> (
        if index < 0 then
          Error
            (Error.Invalid_command_arguments
               "kill history index must not be negative")
        else
          match Clipboard.find_kill clipboard ~index with
          | None ->
              Error
                (Error.Clipboard_slot_empty
                   ("kill-ring[" ^ string_of_int index ^ "]"))
          | Some entry -> (
              match paste_intents history entry placement with
              | Error _ as error -> error
              | Ok intents -> (
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
                          retain_repeatable repeatable_intents intents ))))
    | Model_effect.Request_search _ | Model_effect.Repeat_search _
    | Model_effect.Request_macro _ | Model_effect.Request_location _
    | Model_effect.Request_jump _ | Model_effect.Request_workspace _
    | Model_effect.Request_viewport _ | Model_effect.Request_external_filter _
    | Model_effect.Request_background_process _ | Model_effect.Request_save ->
        Ok (history, clipboard, [], [], [], repeatable_intents)
    | Model_effect.Undo -> (
        match History.undo history with
        | Error _ as error -> error
        | Ok history ->
            trace runtime.trace (fun () ->
                Trace_event.History_changed
                  {
                    execution_id;
                    operation = "undo";
                    current_change =
                      Option.map History.change_id
                        (History.current_change history);
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
                    current_change =
                      Option.map History.change_id
                        (History.current_change history);
                  });
            Ok (history, clipboard, [], [], [], repeatable_intents))
    | Model_effect.Repeat_last_edit -> (
        match repeatable_intents with
        | None -> Error Error.No_repeatable_edit
        | Some intents -> (
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
                Ok (history, clipboard, actions, changes, [], repeatable_intents)
            ))

  and interpret_effects runtime ~execution_id history clipboard
      repeatable_intents base effects =
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
    let descriptor = runtime.model_descriptor in
    trace runtime.trace (fun () ->
        Trace_event.Input_received
          { execution_id; input = Input_event.to_string input });
    trace runtime.trace (fun () ->
        Trace_event.Model_before
          {
            execution_id;
            model_id = Editing_model.id descriptor;
            status_id = Model_status.id status_before;
            status_label = Model_status.label status_before;
          });
    let context =
      make_context ~execution_id ~trace:runtime.trace ~profiler:runtime.profiler
        ?syntax_service:runtime.syntax_service
        ?macro_recording_register:runtime.macro_recording_register
        runtime.history runtime.commands runtime.clipboard
    in
    let model_result =
      Profiler.measure runtime.profiler ~model_id:(Editing_model.id descriptor)
        Profiler.Model_handle (fun () ->
          model_call (fun () -> Model.handle_input runtime.state input context))
    in
    match model_result with
    | Error error ->
        trace_extension_callback runtime ~execution_id ~kind:"model"
          ~provider:(Editing_model.provider descriptor)
          ~semantic_id:(Editing_model.id descriptor)
          ~reason:(Error.to_string error) "failed";
        trace runtime.trace (fun () ->
            Trace_event.Error_reported
              { execution_id; reason = Error.to_string error });
        Error error
    | Ok (state, effects) -> (
        trace_extension_callback runtime ~execution_id ~kind:"model"
          ~provider:(Editing_model.provider descriptor)
          ~semantic_id:(Editing_model.id descriptor)
          "succeeded";
        let base () =
          Provenance.create ~execution_id
            ~model_id:(Editing_model.id descriptor)
            ~provider:(Editing_model.provider descriptor)
            ~input:(Input_event.to_string input)
          |> fun provenance ->
          match runtime.pending_interaction with
          | Some (interaction_id, _, _) ->
              Provenance.add provenance (Provenance.Interaction interaction_id)
          | None -> provenance
        in
        match
          interpret_effects runtime ~execution_id runtime.history
            runtime.clipboard runtime.repeatable_intents base effects
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
                let pending_interaction =
                  match
                    ( runtime.pending_interaction,
                      Model_status.pending_input status_after )
                  with
                  | None, None -> None
                  | Some (interaction_id, started_execution, inputs), Some _ ->
                      Some
                        (interaction_id, started_execution, inputs @ [ input ])
                  | None, Some _ ->
                      trace runtime.trace (fun () ->
                          Trace_event.Interaction_started
                            { execution_id; interaction_id = execution_id });
                      Some (execution_id, execution_id, [ input ])
                  | Some (interaction_id, started_execution, inputs), None ->
                      let inputs = inputs @ [ input ] in
                      trace runtime.trace (fun () ->
                          Trace_event.Interaction_completed
                            {
                              execution_id;
                              interaction_id;
                              started_execution;
                              inputs = List.map Input_event.to_string inputs;
                            });
                      None
                in
                trace runtime.trace (fun () ->
                    Trace_event.Model_transition
                      {
                        execution_id;
                        model_id = Editing_model.id descriptor;
                        previous_status = Model_status.id status_before;
                        next_status = Model_status.id status_after;
                      });
                let next =
                  {
                    history;
                    commands = runtime.commands;
                    semantic_behaviors = runtime.semantic_behaviors;
                    syntax_service = runtime.syntax_service;
                    clipboard;
                    model_descriptor = runtime.model_descriptor;
                    macro_recording_register = runtime.macro_recording_register;
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
                      intents =
                        List.filter_map (fun action -> action.intent) actions;
                      messages;
                      change_ids;
                      document_version =
                        Document_version.to_int
                          (Document.version (History.current history));
                      status_before;
                      status_after;
                    } )))

  let execute_effects runtime ?(augment_provenance = Fun.id) ~input effects =
    let execution_id = !(runtime.next_execution_id) in
    runtime.next_execution_id := execution_id + 1;
    let status_before = Model.status runtime.state in
    let descriptor = runtime.model_descriptor in
    trace runtime.trace (fun () ->
        Trace_event.Input_received
          { execution_id; input = Input_event.to_string input });
    let base () =
      Provenance.create ~execution_id
        ~model_id:(Editing_model.id descriptor)
        ~provider:(Editing_model.provider descriptor)
        ~input:(Input_event.to_string input)
      |> augment_provenance
    in
    match
      interpret_effects runtime ~execution_id runtime.history runtime.clipboard
        runtime.repeatable_intents base effects
    with
    | Error error ->
        trace runtime.trace (fun () ->
            Trace_event.Error_reported
              { execution_id; reason = Error.to_string error });
        Error error
    | Ok (history, clipboard, actions, change_ids, messages, repeatable_intents)
      ->
        let next =
          {
            runtime with
            history;
            clipboard;
            input_trace = bounded_inputs runtime.input_trace input;
            repeatable_intents;
            last_execution = Some execution_id;
          }
        in
        Ok
          ( next,
            {
              execution_id;
              input;
              effects;
              intents = List.filter_map (fun action -> action.intent) actions;
              messages;
              change_ids;
              document_version =
                Document_version.to_int
                  (Document.version (History.current history));
              status_before;
              status_after = status_before;
            } )

  let restore_selections runtime ~selections ~primary =
    match Model_intent.set_selections ~selections ~primary with
    | Error _ as error -> error
    | Ok intent ->
        let input =
          Input_event.key_press (Input_event.named_key Input_event.Escape)
        in
        execute_effects runtime
          ~augment_provenance:(fun provenance ->
            Provenance.add provenance
              (Provenance.Effect "workspace.view.restore"))
          ~input
          [ Model_effect.Execute_intent intent ]
        |> Result.map fst

  let invoke_command runtime ?augment_provenance ~input invocation =
    execute_effects runtime ?augment_provenance ~input
      [ Model_effect.Invoke_command invocation ]

  let reset runtime =
    let context =
      make_context ~trace:runtime.trace ~profiler:runtime.profiler
        ?syntax_service:runtime.syntax_service
        ?macro_recording_register:runtime.macro_recording_register
        runtime.history runtime.commands runtime.clipboard
    in
    match model_call (fun () -> Model.reset runtime.state context) with
    | Error _ as error -> error
    | Ok state ->
        Ok
          {
            runtime with
            state;
            model_descriptor = Model.descriptor_of_state state;
          }

  let history runtime = runtime.history
  let commands runtime = runtime.commands
  let semantic_behaviors runtime = runtime.semantic_behaviors

  let with_extensions runtime ~commands ~semantic_behaviors =
    { runtime with commands; semantic_behaviors }

  let context runtime =
    make_context ~trace:runtime.trace ~profiler:runtime.profiler
      ?syntax_service:runtime.syntax_service
      ?macro_recording_register:runtime.macro_recording_register runtime.history
      runtime.commands runtime.clipboard

  let status runtime = Model.status runtime.state
  let model_state runtime = runtime.state
  let model_descriptor runtime = runtime.model_descriptor
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
