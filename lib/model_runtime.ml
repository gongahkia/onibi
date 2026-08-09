open Zenbu_kernel

module Make (Model : Editing_model.S) = struct
  type model_state = Model.state

  type t = {
    history : History.t;
    commands : Command_registry.t;
    clipboard : Clipboard.t;
    state : Model.state;
    input_trace : Input_event.t list;
    repeatable_intents : Model_intent.t list option;
  }

  type step = {
    input : Input_event.t;
    effects : Model_effect.t list;
    intents : Model_intent.t list;
    messages : Model_effect.message list;
    change_ids : int list;
    document_version : int;
    status_after : Model_status.t;
  }

  let make_context history commands clipboard =
    Editor_context.from_snapshot
      ~snapshot:(Document.snapshot (History.current history)) ~clipboard
      ~commands:(Command_registry.descriptors commands) ()

  let model_call call =
    try Ok (call ())
    with exception_ ->
      Error (Error.Model_execution_failed (Printexc.to_string exception_))

  let create ?(commands = Command_registry.empty) ~document () =
    let history = History.create document in
    let clipboard = Clipboard.empty in
    let context = make_context history commands clipboard in
    match model_call (fun () -> Model.initialize context) with
    | Error _ as error -> error
    | Ok state ->
        Ok
          {
            history;
            commands;
            clipboard;
            state;
            input_trace = [];
            repeatable_intents = None;
          }

  let apply_intent history ?description intent =
    History.apply_intent ~source:Transaction.User ?description history
      (Model_intent.to_kernel intent)

  let change_id history =
    match History.current_change history with
    | Some change -> History.change_id change
    | None ->
        failwith "history invariant violated: committed intent has no change"

  let apply_intents history ?description intents =
    let rec loop history change_ids = function
      | [] -> Ok (history, List.rev change_ids)
      | intent :: rest -> (
          match apply_intent history ?description intent with
          | Error _ as error -> error
          | Ok history -> loop history (change_id history :: change_ids) rest)
    in
    loop history [] intents

  let repeatable intents =
    if List.exists Model_intent.is_textual intents then Some intents else None

  let retain_repeatable previous intents =
    match repeatable intents with Some intents -> Some intents | None -> previous

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
    | Clipboard.Before | Clipboard.After ->
        let offsets =
          match Clipboard.kind entry with
          | Clipboard.Characterwise ->
              let context =
                make_context history Command_registry.empty Clipboard.empty
              in
              let selections = Editor_context.selections context in
              Ok
                ( selections.Editor_context.primary_index,
                  List.map
                    (fun selection ->
                      match placement with
                      | Clipboard.Before ->
                          min selection.Editor_context.anchor_offset
                            selection.Editor_context.head_offset
                      | Clipboard.After ->
                          max selection.Editor_context.anchor_offset
                            selection.Editor_context.head_offset
                      | Clipboard.Replace -> assert false)
                    selections.Editor_context.selections )
          | Clipboard.Linewise -> (
              match selection_set_for_selector history Model_intent.Current_line with
              | Error _ as error -> error
              | Ok selections ->
                  Ok
                    ( Selection_set.primary_index selections,
                      List.map
                        (fun selection ->
                          let range = Selection.range selection in
                          match placement with
                          | Clipboard.Before ->
                              Anchor.byte_offset (Range.start range)
                          | Clipboard.After ->
                              Anchor.byte_offset (Range.stop range)
                          | Clipboard.Replace -> assert false)
                        (Selection_set.to_list selections) ))
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
            | Ok set -> Ok [ set; Model_intent.insert_text contents ])

  let interpret_effect commands history clipboard repeatable_intents model_effect =
    match model_effect with
    | Model_effect.Execute_intent intent -> (
        match apply_intents history [ intent ] with
        | Error _ as error -> error
        | Ok (history, changes) ->
            Ok
              ( history,
                clipboard,
                [ intent ],
                changes,
                [],
                retain_repeatable repeatable_intents [ intent] ))
    | Model_effect.Invoke_command invocation -> (
        let context = make_context history commands clipboard in
        match Command_registry.invoke commands ~context invocation with
        | Error _ as error -> error
        | Ok intents -> (
            let description =
              "command "
              ^ Command_id.to_string (Command_invocation.id invocation)
            in
            match apply_intents history ~description intents with
            | Error _ as error -> error
            | Ok (history, changes) ->
                Ok
                  ( history,
                    clipboard,
                    intents,
                    changes,
                    [],
                    retain_repeatable repeatable_intents intents )))
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
            | Ok intents -> (
                match apply_intents history ~description:"paste" intents with
                | Error _ as error -> error
                | Ok (history, changes) ->
                    Ok
                      ( history,
                        clipboard,
                        intents,
                        changes,
                        [],
                        repeatable_intents ))))
    | Model_effect.Undo -> (
        match History.undo history with
        | Error _ as error -> error
        | Ok history -> Ok (history, clipboard, [], [], [], repeatable_intents))
    | Model_effect.Redo -> (
        match History.redo history with
        | Error _ as error -> error
        | Ok history -> Ok (history, clipboard, [], [], [], repeatable_intents))
    | Model_effect.Repeat_last_edit -> (
        match repeatable_intents with
        | None -> Error Error.No_repeatable_edit
        | Some intents -> (
            match apply_intents history ~description:"repeat semantic edit" intents with
            | Error _ as error -> error
            | Ok (history, changes) ->
                Ok
                  ( history,
                    clipboard,
                    intents,
                    changes,
                    [],
                    repeatable_intents )))

  let interpret_effects commands history clipboard repeatable_intents effects =
    let rec loop history clipboard intents changes messages repeatable_intents =
      function
      | [] ->
          Ok
            ( history,
              clipboard,
              List.rev intents,
              List.rev changes,
              List.rev messages,
              repeatable_intents )
      | model_effect :: rest -> (
          match
            interpret_effect commands history clipboard repeatable_intents
              model_effect
          with
          | Error _ as error -> error
          | Ok
              ( history,
                clipboard,
                effect_intents,
                effect_changes,
                effect_messages,
                repeatable_intents ) ->
              loop history clipboard
                (List.rev_append effect_intents intents)
                (List.rev_append effect_changes changes)
                (List.rev_append effect_messages messages)
                repeatable_intents rest)
    in
    loop history clipboard [] [] [] repeatable_intents effects

  let handle_input runtime input =
    let context = make_context runtime.history runtime.commands runtime.clipboard in
    match
      model_call (fun () -> Model.handle_input runtime.state input context)
    with
    | Error _ as error -> error
    | Ok (state, effects) -> (
        match
          interpret_effects runtime.commands runtime.history runtime.clipboard
            runtime.repeatable_intents effects
        with
        | Error _ as error -> error
        | Ok
            ( history,
              clipboard,
              intents,
              change_ids,
              messages,
              repeatable_intents ) -> (
            match model_call (fun () -> Model.status state) with
            | Error _ as error -> error
            | Ok status ->
                let next =
                  {
                    history;
                    commands = runtime.commands;
                    clipboard;
                    state;
                    input_trace = runtime.input_trace @ [ input ];
                    repeatable_intents;
                  }
                in
                Ok
                  ( next,
                    {
                      input;
                      effects;
                      intents;
                      messages;
                      change_ids;
                      document_version =
                        Document_version.to_int
                          (Document.version (History.current history));
                      status_after = status;
                    } )))

  let reset runtime =
    let context = make_context runtime.history runtime.commands runtime.clipboard in
    match model_call (fun () -> Model.reset runtime.state context) with
    | Error _ as error -> error
    | Ok state -> Ok { runtime with state }

  let history runtime = runtime.history
  let context runtime = make_context runtime.history runtime.commands runtime.clipboard
  let status runtime = Model.status runtime.state
  let model_descriptor _ = Model.descriptor
  let input_trace runtime = runtime.input_trace
  let input step = step.input
  let effects step = step.effects
  let intents step = step.intents
  let messages step = step.messages
  let change_ids step = step.change_ids
  let document_version step = step.document_version
  let status_after step = step.status_after
end
