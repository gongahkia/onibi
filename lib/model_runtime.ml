open Zenbu_kernel

module Make (Model : Editing_model.S) = struct
  type model_state = Model.state

  type t = {
    history : History.t;
    commands : Command_registry.t;
    state : Model.state;
    input_trace : Input_event.t list;
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

  let make_context history commands =
    Editor_context.from_snapshot
      ~snapshot:(Document.snapshot (History.current history))
      ~commands:(Command_registry.descriptors commands)

  let model_call call =
    try Ok (call ()) with exception_ -> Error (Error.Model_execution_failed (Printexc.to_string exception_))

  let create ?(commands = Command_registry.empty) ~document () =
    let history = History.create document in
    let context = make_context history commands in
    match model_call (fun () -> Model.initialize context) with
    | Error _ as error -> error
    | Ok state -> Ok { history; commands; state; input_trace = [] }

  let apply_intent history ?description intent =
    History.apply_intent ~source:Transaction.User ?description history
      (Model_intent.to_kernel intent)

  let change_id history =
    match History.current_change history with
    | Some change -> History.change_id change
    | None -> failwith "history invariant violated: committed intent has no change"

  let apply_intents history ?description intents =
    let rec loop history change_ids = function
      | [] -> Ok (history, List.rev change_ids)
      | intent :: rest -> (
          match apply_intent history ?description intent with
          | Error _ as error -> error
          | Ok history -> loop history (change_id history :: change_ids) rest)
    in
    loop history [] intents

  let interpret_effect commands history model_effect =
    match model_effect with
    | Model_effect.Execute_intent intent -> (
        match apply_intents history [ intent ] with
        | Error _ as error -> error
        | Ok (history, changes) -> Ok (history, [ intent ], changes, []))
    | Model_effect.Invoke_command invocation ->
        let context = make_context history commands in
        (match Command_registry.invoke commands ~context invocation with
        | Error _ as error -> error
        | Ok intents ->
            let description =
              "command " ^ Command_id.to_string (Command_invocation.id invocation)
            in
            (match apply_intents history ~description intents with
            | Error _ as error -> error
            | Ok (history, changes) -> Ok (history, intents, changes, [])))
    | Model_effect.Emit_message message -> Ok (history, [], [], [ message ])

  let interpret_effects commands history effects =
    let rec loop history intents changes messages = function
      | [] -> Ok (history, List.rev intents, List.rev changes, List.rev messages)
      | model_effect :: rest -> (
          match interpret_effect commands history model_effect with
          | Error _ as error -> error
          | Ok (history, effect_intents, effect_changes, effect_messages) ->
              loop history
                (List.rev_append effect_intents intents)
                (List.rev_append effect_changes changes)
                (List.rev_append effect_messages messages)
                rest)
    in
    loop history [] [] [] effects

  let handle_input runtime input =
    let context = make_context runtime.history runtime.commands in
    match model_call (fun () -> Model.handle_input runtime.state input context) with
    | Error _ as error -> error
    | Ok (state, effects) -> (
        match interpret_effects runtime.commands runtime.history effects with
        | Error _ as error -> error
        | Ok (history, intents, change_ids, messages) -> (
            match model_call (fun () -> Model.status state) with
            | Error _ as error -> error
            | Ok status ->
                let next =
                  {
                    history;
                    commands = runtime.commands;
                    state;
                    input_trace = runtime.input_trace @ [ input ];
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
                        Document_version.to_int (Document.version (History.current history));
                      status_after = status;
                    })))

  let reset runtime =
    let context = make_context runtime.history runtime.commands in
    match model_call (fun () -> Model.reset runtime.state context) with
    | Error _ as error -> error
    | Ok state -> Ok { runtime with state }

  let history runtime = runtime.history
  let context runtime = make_context runtime.history runtime.commands
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
