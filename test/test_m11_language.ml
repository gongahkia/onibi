module Language = Zenbu_language.Language
module Lsp = Zenbu_lsp.Client

let fail message =
  prerr_endline ("M11 test failure: " ^ message);
  exit 1

let expect condition message = if not condition then fail message
let must = function Ok value -> value | Error error -> fail error

let contains text fragment =
  let text_length = String.length text in
  let fragment_length = String.length fragment in
  let rec loop offset =
    if offset + fragment_length > text_length then false
    else if String.sub text offset fragment_length = fragment then true
    else loop (offset + 1)
  in
  fragment_length = 0 || loop 0

let fake_server () =
  let candidate =
    Filename.concat (Filename.dirname Sys.executable_name) "fake_lsp_server.exe"
  in
  if Sys.file_exists candidate then candidate
  else Filename.concat (Sys.getcwd ()) "_build/default/test/fake_lsp_server.exe"

let config arguments =
  Language.Server_config.create ~id:"test.fake" ~language_ids:[ "ocaml" ]
    ~extensions:[ ".ml" ] ~executable:(fake_server ()) ~argv:arguments
    ~root_markers:[ "dune-project" ] ()
  |> must

let wait_for client predicate =
  let deadline = Unix.gettimeofday () +. 3. in
  let rec loop events =
    let fresh = Lsp.drain client in
    let events = events @ fresh in
    if predicate events then events
    else if Unix.gettimeofday () >= deadline then
      let status = Lsp.status client in
      let event_names =
        events
        |> List.map (function
          | Lsp.Initialized -> "initialized"
          | Diagnostics _ -> "diagnostics"
          | Hover_result _ -> "hover"
          | Definition_result _ -> "definition"
          | Completion_result _ -> "completion"
          | Code_action_result _ -> "code-action"
          | Formatting_result _ -> "formatting"
          | Symbol_result _ -> "symbols"
          | Rename_result _ -> "rename"
          | Apply_edit _ -> "apply-edit"
          | Server_message _ -> "message"
          | Request_failed { reason; _ } -> "request-failed:" ^ reason
          | Server_failed _ -> "server-failed"
          | Server_exited _ -> "server-exited")
      in
      fail
        ("timed out waiting for fake LSP event; state="
        ^ Language.server_state_name status.state
        ^ " error="
        ^ Option.value ~default:"none" status.last_error
        ^ " events="
        ^ String.concat "," event_names)
    else (
      ignore (Unix.select [ Lsp.wakeup_fd client ] [] [] 0.05);
      loop events)
  in
  loop []

let start arguments contents =
  Lsp.start ~config:(config arguments) ~document_id:"m11-test"
    ~document_version:0
    ~file_path:(Filename.concat (Sys.getcwd ()) "test/fixtures/m11_language.ml")
    ~contents
    ~trace:(Zenbu_model_api.Trace.disabled ())
    ~profiler:(Zenbu_model_api.Profiler.disabled ())

let wait_ready client =
  ignore
    (wait_for client
       (List.exists (function Lsp.Initialized -> true | _ -> false)));
  expect
    ((Lsp.status client).state = Language.Ready)
    "fake server did not become ready"

let position_tests () =
  let contents = "Aé中😀e\204\129\t\n\nlast\r\nx" in
  let emoji_offset = String.length "Aé中" in
  let assert_position encoding expected =
    let actual =
      Language.Position.offset_to_position ~contents ~encoding
        ~byte_offset:emoji_offset
      |> must
    in
    expect (actual = expected)
      "position mapper produced the wrong emoji position";
    let offset =
      Language.Position.position_to_offset ~contents ~encoding actual |> must
    in
    expect (offset = emoji_offset)
      "position mapper did not round-trip emoji offset"
  in
  assert_position Language.Position.Utf8 { line = 0; character = 6 };
  assert_position Language.Position.Utf16 { line = 0; character = 3 };
  assert_position Language.Position.Utf32 { line = 0; character = 3 };
  expect
    (Result.is_error
       (Language.Position.position_to_offset ~contents
          ~encoding:Language.Position.Utf16
          { line = 0; character = 4 }))
    "UTF-16 mapper accepted a surrogate-pair split";
  let cr_offset = String.length "Aé中😀e\204\129\t\n\nlast\r" in
  expect
    (Result.is_error
       (Language.Position.offset_to_position ~contents
          ~encoding:Language.Position.Utf8 ~byte_offset:cr_offset))
    "mapper accepted an anchor splitting CRLF"

let sync_tests () =
  let source = "zero 😀\nalpha beta\n" in
  let beta =
    match String.index_from_opt source 0 'b' with
    | Some offset -> offset
    | None -> fail "sync fixture is malformed"
  in
  let expected = "ONE 😀\nalpha BETA\n" in
  let edits =
    [
      { Language.start_offset = 0; stop_offset = 4; replacement = "ONE" };
      { start_offset = beta; stop_offset = beta + 4; replacement = "BETA" };
    ]
  in
  List.iter
    (fun encoding ->
      let changes =
        Language.Sync.incremental_changes ~contents:source ~encoding ~edits
          ~expected
        |> must
      in
      let reconstructed =
        Language.Sync.apply_changes ~contents:source ~encoding changes |> must
      in
      expect
        (String.equal reconstructed expected)
        "incremental changes did not reconstruct the committed snapshot")
    [ Language.Position.Utf8; Utf16; Utf32 ]

let diagnostic_matches contents = function
  | Lsp.Diagnostics { document_version = Some 1; diagnostics } ->
      List.exists
        (fun (diagnostic : Language.diagnostic) ->
          String.equal diagnostic.message ("sync:" ^ contents))
        diagnostics
  | _ -> false

let synchronization_test mode =
  let source = "abc 😀\ndef\n" in
  let client = start [ "--sync"; mode ] source in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      let expected = "ABC 😀\nD!\n" in
      let edits =
        [
          { Language.start_offset = 0; stop_offset = 3; replacement = "ABC" };
          {
            start_offset = String.length "abc 😀\n";
            stop_offset = String.length "abc 😀\ndef";
            replacement = "D!";
          };
        ]
      in
      Lsp.notify_change client ~source_contents:source ~contents:expected
        ~document_version:1 ~edits;
      ignore (wait_for client (List.exists (diagnostic_matches expected))))

let feature_test () =
  let client = start [ "--sync"; "incremental" ] "abc abc\n" in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      ignore (Lsp.request_hover client ~byte_offset:0 |> must);
      ignore
        (wait_for client
           (List.exists (function
             | Lsp.Hover_result { hover = Some _; _ } -> true
             | _ -> false)));
      ignore (Lsp.request_definition client ~byte_offset:0 |> must);
      ignore
        (wait_for client
           (List.exists (function
             | Lsp.Definition_result { targets = _ :: _; _ } -> true
             | _ -> false)));
      ignore (Lsp.request_completion client ~byte_offset:1 |> must);
      ignore
        (wait_for client
           (List.exists (function
             | Lsp.Completion_result { items = _ :: _; _ } -> true
             | _ -> false)));
      ignore
        (Lsp.request_code_actions client ~start_offset:0 ~stop_offset:0 |> must);
      ignore
        (wait_for client
           (List.exists (function
             | Lsp.Code_action_result { actions = _ :: _; _ } -> true
             | _ -> false)));
      ignore (Lsp.request_document_formatting client |> must);
      ignore
        (wait_for client
           (List.exists (function
             | Lsp.Formatting_result { scope = Lsp.Document; edits = _ :: _; _ }
               ->
                 true
             | _ -> false)));
      ignore
        (Lsp.request_range_formatting client ~start_offset:0 ~stop_offset:0
        |> must);
      ignore
        (wait_for client
           (List.exists (function
             | Lsp.Formatting_result { scope = Lsp.Range; edits = _ :: _; _ } ->
                 true
             | _ -> false)));
      ignore (Lsp.request_document_symbols client |> must);
      ignore
        (wait_for client
           (List.exists (function
             | Lsp.Symbol_result
                 { scope = Lsp.Document_symbols_scope; symbols = _ :: _; _ } ->
                 true
             | _ -> false)));
      ignore (Lsp.request_workspace_symbols client ~query:"fake" |> must);
      ignore
        (wait_for client
           (List.exists (function
             | Lsp.Symbol_result
                 { scope = Lsp.Workspace_symbols_scope; symbols = _ :: _; _ } ->
                 true
             | _ -> false)));
      ignore
        (Lsp.request_rename client ~byte_offset:0 ~new_name:"renamed" |> must);
      ignore
        (wait_for client
           (List.exists (function
             | Lsp.Rename_result { edits = _ :: _; _ } -> true
             | _ -> false))))

let stale_response_test () =
  let client = start [ "--delay-hover" ] "old" in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      ignore (Lsp.request_hover client ~byte_offset:0 |> must);
      Lsp.notify_change client ~source_contents:"old" ~contents:"new"
        ~document_version:1
        ~edits:
          [
            { Language.start_offset = 0; stop_offset = 3; replacement = "new" };
          ];
      let deadline = Unix.gettimeofday () +. 0.4 in
      let rec collect values =
        if Unix.gettimeofday () >= deadline then values
        else (
          ignore (Unix.select [ Lsp.wakeup_fd client ] [] [] 0.05);
          collect (values @ Lsp.drain client))
      in
      let events = collect [] in
      expect
        (not
           (List.exists
              (function Lsp.Hover_result _ -> true | _ -> false)
              events))
        "late hover response was not discarded after a document edit")

let stale_code_action_response_test () =
  let client = start [ "--delay-code-action" ] "old" in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      ignore
        (Lsp.request_code_actions client ~start_offset:0 ~stop_offset:0 |> must);
      Lsp.notify_change client ~source_contents:"old" ~contents:"new"
        ~document_version:1
        ~edits:
          [
            { Language.start_offset = 0; stop_offset = 3; replacement = "new" };
          ];
      let deadline = Unix.gettimeofday () +. 0.4 in
      let rec collect values =
        if Unix.gettimeofday () >= deadline then values
        else (
          ignore (Unix.select [ Lsp.wakeup_fd client ] [] [] 0.05);
          collect (values @ Lsp.drain client))
      in
      let events = collect [] in
      expect
        (not
           (List.exists
              (function Lsp.Code_action_result _ -> true | _ -> false)
              events))
        "late code-action response was not discarded after a document edit")

let stale_formatting_response_test () =
  let client = start [ "--delay-formatting" ] "old" in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      ignore (Lsp.request_document_formatting client |> must);
      Lsp.notify_change client ~source_contents:"old" ~contents:"new"
        ~document_version:1
        ~edits:
          [
            { Language.start_offset = 0; stop_offset = 3; replacement = "new" };
          ];
      let deadline = Unix.gettimeofday () +. 0.4 in
      let rec collect values =
        if Unix.gettimeofday () >= deadline then values
        else (
          ignore (Unix.select [ Lsp.wakeup_fd client ] [] [] 0.05);
          collect (values @ Lsp.drain client))
      in
      let events = collect [] in
      expect
        (not
           (List.exists
              (function Lsp.Formatting_result _ -> true | _ -> false)
              events))
        "late formatting response was not discarded after a document edit")

let formatting_cancellation_test () =
  let client = start [ "--delay-formatting" ] "old" in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      ignore (Lsp.request_document_formatting client |> must);
      Lsp.cancel client Lsp.Document_formatting;
      let deadline = Unix.gettimeofday () +. 0.4 in
      let rec collect values =
        if Unix.gettimeofday () >= deadline then values
        else (
          ignore (Unix.select [ Lsp.wakeup_fd client ] [] [] 0.05);
          collect (values @ Lsp.drain client))
      in
      let events = collect [] in
      expect
        (not
           (List.exists
              (function Lsp.Formatting_result _ -> true | _ -> false)
              events))
        "cancelled formatting response reached the client")

let malformed_formatting_response_test () =
  let client = start [ "--formatting-malformed" ] "abc" in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      ignore (Lsp.request_document_formatting client |> must);
      let events =
        wait_for client
          (List.exists (function
            | Lsp.Request_failed { kind = Lsp.Document_formatting; _ } -> true
            | _ -> false))
      in
      expect
        (List.exists
           (function
             | Lsp.Request_failed { kind = Lsp.Document_formatting; reason; _ }
               ->
                 String.starts_with ~prefix:"invalid formatting result" reason
             | _ -> false)
           events)
        "a malformed formatting response was accepted")

let stale_symbol_response_test () =
  let client = start [ "--delay-symbols" ] "old" in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      ignore (Lsp.request_document_symbols client |> must);
      Lsp.notify_change client ~source_contents:"old" ~contents:"new"
        ~document_version:1
        ~edits:
          [
            { Language.start_offset = 0; stop_offset = 3; replacement = "new" };
          ];
      let deadline = Unix.gettimeofday () +. 0.4 in
      let rec collect values =
        if Unix.gettimeofday () >= deadline then values
        else (
          ignore (Unix.select [ Lsp.wakeup_fd client ] [] [] 0.05);
          collect (values @ Lsp.drain client))
      in
      expect
        (not
           (List.exists
              (function Lsp.Symbol_result _ -> true | _ -> false)
              (collect [])))
        "late symbol response was not discarded after a document edit")

let invalid_symbol_response_test () =
  let client = start [ "--invalid-symbols" ] "abc" in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      ignore (Lsp.request_document_symbols client |> must);
      let events =
        wait_for client
          (List.exists (function
            | Lsp.Request_failed { kind = Lsp.Document_symbols; _ } -> true
            | _ -> false))
      in
      expect
        (List.exists
           (function
             | Lsp.Request_failed { kind = Lsp.Document_symbols; _ } -> true
             | _ -> false)
           events)
        "an invalid symbol range was accepted")

let code_action_resource_rejection_test () =
  let client = start [ "--code-action-resource" ] "resource" in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      ignore
        (Lsp.request_code_actions client ~start_offset:0 ~stop_offset:0 |> must);
      let events =
        wait_for client
          (List.exists (function
            | Lsp.Request_failed { kind = Lsp.Code_action; _ } -> true
            | _ -> false))
      in
      expect
        (List.exists
           (function
             | Lsp.Request_failed { kind = Lsp.Code_action; reason; _ } ->
                 String.starts_with
                   ~prefix:
                     "workspace edit contains an unsupported resource operation"
                   reason
             | _ -> false)
           events)
        "a resource operation embedded in a code action was accepted")

let crash_restart_test () =
  let marker = Filename.temp_file "zenbu-m11-crash" ".marker" in
  (try Sys.remove marker with Sys_error _ -> ());
  let client = start [ "--crash-once"; marker ] "restart" in
  Fun.protect
    ~finally:(fun () ->
      Lsp.close client;
      try Sys.remove marker with Sys_error _ -> ())
    (fun () ->
      ignore
        (wait_for client
           (List.exists (function
             | Lsp.Server_failed _ | Server_exited _ -> true
             | _ -> false)));
      expect
        ((Lsp.status client).state = Language.Failed)
        "crashed server did not enter failed state";
      Lsp.restart client;
      wait_ready client)

let malformed_server_test () =
  let client = start [ "--malformed" ] "malformed" in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      ignore
        (wait_for client
           (List.exists (function Lsp.Server_failed _ -> true | _ -> false)));
      expect
        ((Lsp.status client).state = Language.Failed)
        "malformed protocol input did not fail the client")

let trace_attribution_test () =
  let trace =
    Zenbu_model_api.Trace.enabled ~capacity:32 |> function
    | Ok trace -> trace
    | Error error -> fail (Zenbu_kernel.Error.to_string error)
  in
  let client =
    Lsp.start ~config:(config []) ~document_id:"m11-trace" ~document_version:0
      ~file_path:
        (Filename.concat (Sys.getcwd ()) "test/fixtures/m11_language.ml")
      ~contents:"trace" ~trace
      ~profiler:(Zenbu_model_api.Profiler.disabled ())
  in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_ready client;
      Lsp.set_execution_id client ~execution_id:17;
      ignore (Lsp.request_hover client ~byte_offset:0 |> must);
      ignore
        (wait_for client
           (List.exists (function Lsp.Hover_result _ -> true | _ -> false)));
      let why = Zenbu_model_api.Inspector.why trace ~execution_id:17 in
      expect
        (Option.value ~default:[]
           (Option.map Zenbu_model_api.Inspector.why_events why)
        |> List.exists (function
          | Zenbu_model_api.Trace_event.Language_service _ -> true
          | _ -> false))
        "language-service tracing was not attributable to its execution")

let registry arguments =
  Language.Registry.register Language.Registry.empty (config arguments) |> must

let session ?trace arguments contents =
  Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim
    ~file_path:(Filename.concat (Sys.getcwd ()) "test/fixtures/m11_language.ml")
    ~contents ?trace ~language_registry:(registry arguments)
    ~dimensions:Zenbu_view.Renderer.{ columns = 80; rows = 12 }
    ()
  |> function
  | Ok session -> session
  | Error error -> fail (Zenbu_kernel.Error.to_string error)

let host session command =
  match Zenbu_app.Session.handle_host session command with
  | Zenbu_app.Session.Continue session -> session
  | Zenbu_app.Session.Exit _ ->
      fail "language command unexpectedly exited session"

let enter =
  Zenbu_model_api.Input_event.key_press
    (Zenbu_model_api.Input_event.named_key Zenbu_model_api.Input_event.Enter)

let escape =
  Zenbu_model_api.Input_event.key_press
    (Zenbu_model_api.Input_event.named_key Zenbu_model_api.Input_event.Escape)

let text_input text =
  Zenbu_model_api.Input_event.text_input text |> function
  | Ok input -> input
  | Error error -> fail (Zenbu_kernel.Error.to_string error)

let logical_key text =
  Zenbu_model_api.Input_event.logical_text text |> function
  | Ok input -> Zenbu_model_api.Input_event.key_press input
  | Error error -> fail (Zenbu_kernel.Error.to_string error)

let frame_contains frame text =
  Zenbu_view.Frame.rows frame
  |> List.exists (fun row ->
      String.contains (Zenbu_view.Frame.row_text row) text.[0]
      && String.contains
           (Zenbu_view.Frame.row_text row)
           text.[String.length text - 1]
      && String.length text <= String.length (Zenbu_view.Frame.row_text row)
      &&
      let row = Zenbu_view.Frame.row_text row in
      let rec contains index =
        if index + String.length text > String.length row then false
        else if String.sub row index (String.length text) = text then true
        else contains (index + 1)
      in
      contains 0)

let wait_session session predicate =
  let deadline = Unix.gettimeofday () +. 3. in
  let rec loop session =
    let session = Zenbu_app.Session.poll_language session in
    if predicate session then session
    else if Unix.gettimeofday () >= deadline then
      fail
        ("timed out waiting for session language event: "
        ^ String.concat " | "
            (Zenbu_app.Session.inspect session Zenbu_app.Session.Language)
        ^ " scripts="
        ^ String.concat " | "
            (Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts))
    else (
      Option.iter
        (fun fd -> ignore (Unix.select [ fd ] [] [] 0.05))
        (Zenbu_app.Session.language_wakeup_fd session);
      loop session)
  in
  loop session

let session_integration_test () =
  let initial = "abc abc\n" in
  let session = session [ "--sync"; "incremental" ] initial in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session =
        wait_session session (fun session ->
            let _, frame = Zenbu_app.Session.render session in
            Zenbu_view.Frame.rows frame
            |> List.concat
            |> List.exists (fun cell ->
                cell.Zenbu_view.Frame.style = Zenbu_view.Frame.Diagnostic_error))
      in
      let session = host session Zenbu_app.Session.Language_diagnostic_next in
      let primary =
        Zenbu_model_api.Editor_context.selections
          (Zenbu_app.Session.context session)
        |> fun selections ->
        List.nth selections.selections selections.primary_index
      in
      expect
        (primary.anchor_offset = 0)
        "diagnostic navigation did not use selection semantics";
      let session = host session Zenbu_app.Session.Language_hover in
      let session =
        wait_session session (fun session ->
            let _, frame = Zenbu_app.Session.render session in
            frame_contains frame "Language hover")
      in
      let session = Zenbu_app.Session.handle_input session enter in
      let session = host session Zenbu_app.Session.Language_definition in
      let session =
        wait_session session (fun session ->
            let primary =
              Zenbu_model_api.Editor_context.selections
                (Zenbu_app.Session.context session)
              |> fun selections ->
              List.nth selections.selections selections.primary_index
            in
            primary.anchor_offset = 0 && primary.head_offset = 1)
      in
      let session = host session Zenbu_app.Session.Language_complete in
      let session =
        wait_session session (fun session ->
            let _, frame = Zenbu_app.Session.render session in
            frame_contains frame "Language completion")
      in
      let session =
        Zenbu_app.Session.handle_input session
          ( Zenbu_model_api.Input_event.text_input "fake" |> function
            | Ok input -> input
            | Error error -> fail (Zenbu_kernel.Error.to_string error) )
      in
      let _, completion_frame = Zenbu_app.Session.render session in
      expect
        (frame_contains completion_frame "filter: fake")
        "completion filter was not rendered";
      let session = Zenbu_app.Session.handle_input session enter in
      expect
        (String.contains (Zenbu_app.Session.contents session) 'f')
        "completion did not apply through the session";
      let session = host session Zenbu_app.Session.Language_rename in
      let session =
        List.fold_left
          (fun session character ->
            Zenbu_app.Session.handle_input session
              ( Zenbu_model_api.Input_event.text_input (String.make 1 character)
              |> function
                | Ok value -> value
                | Error error -> fail (Zenbu_kernel.Error.to_string error) ))
          session [ 'r'; 'e'; 'n' ]
      in
      let session = Zenbu_app.Session.handle_input session enter in
      let session =
        wait_session session (fun session ->
            String.starts_with ~prefix:"ren"
              (Zenbu_app.Session.contents session))
      in
      expect
        (String.starts_with ~prefix:"ren" (Zenbu_app.Session.contents session))
        "current-document rename was not applied atomically")

let document_formatting_session_test () =
  let trace =
    Zenbu_model_api.Trace.enabled ~capacity:64 |> function
    | Ok trace -> trace
    | Error error -> fail (Zenbu_kernel.Error.to_string error)
  in
  let session = session ~trace [] "abc\n" in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = host session Zenbu_app.Session.Language_format_document in
      let session =
        wait_session session (fun session ->
            String.starts_with ~prefix:"formatted"
              (Zenbu_app.Session.contents session))
      in
      expect
        (Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
        |> List.exists (String.equal "message: language formatting applied"))
        "document formatting did not publish a checked transaction";
      expect
        (Zenbu_app.Session.inspect session Zenbu_app.Session.Why
        |> List.exists (fun line -> contains line "language.format"))
        "formatting provenance was not visible through why")

let range_formatting_session_test () =
  let session = session [] "abc\n" in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = host session Zenbu_app.Session.Language_format_selection in
      let session =
        wait_session session (fun session ->
            String.starts_with ~prefix:"range-formatted"
              (Zenbu_app.Session.contents session))
      in
      expect
        (Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
        |> List.exists
             (String.equal "message: language range formatting applied"))
        "range formatting did not use the selected source range")

let formatting_noop_session_test () =
  let contents = "abc\n" in
  let session = session [ "--formatting-noop" ] contents in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = host session Zenbu_app.Session.Language_format_document in
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
            |> List.exists
                 (String.equal "message: language formatting made no changes"))
      in
      expect
        (String.equal (Zenbu_app.Session.contents session) contents)
        "a no-op formatting result changed the document")

let formatting_dirty_invalid_syntax_test () =
  let session = session [] "let =" in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = Zenbu_app.Session.handle_input session (logical_key "i") in
      let session = Zenbu_app.Session.handle_input session (text_input "!") in
      let session = Zenbu_app.Session.handle_input session escape in
      let session = host session Zenbu_app.Session.Language_format_document in
      let session =
        wait_session session (fun session ->
            String.starts_with ~prefix:"formatted"
              (Zenbu_app.Session.contents session))
      in
      expect
        (Zenbu_app.Session.contents session <> "let =")
        "formatting did not define behavior for a dirty invalid-syntax buffer")

let formatting_conflict_session_test () =
  let contents = "abc\n" in
  let session = session [ "--formatting-conflict" ] contents in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = host session Zenbu_app.Session.Language_format_document in
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
            |> List.exists
                 (String.starts_with
                    ~prefix:"message: language formatting rejected:"))
      in
      expect
        (String.equal (Zenbu_app.Session.contents session) contents)
        "overlapping formatting edits partially changed the document")

let formatting_server_failure_test () =
  let contents = "abc\n" in
  let session = session [ "--formatting-error" ] contents in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = host session Zenbu_app.Session.Language_format_document in
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
            |> List.exists
                 (String.equal
                    "message: language formatting failed: fake formatting \
                     failure"))
      in
      expect
        (String.equal (Zenbu_app.Session.contents session) contents)
        "a formatting server failure did not leave the document unchanged")

let document_symbol_session_test () =
  let session = session [] "abc\n" in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = host session Zenbu_app.Session.Language_document_symbols in
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Symbols
            |> List.exists (String.equal "symbols: 2"))
      in
      let session = Zenbu_app.Session.handle_input session enter in
      let primary =
        Zenbu_model_api.Editor_context.selections
          (Zenbu_app.Session.context session)
        |> fun selections ->
        List.nth selections.selections selections.primary_index
      in
      expect
        (primary.anchor_offset = 0 && primary.head_offset = 1)
        "selecting a document symbol did not navigate through host selection")

let code_action_session_test () =
  let session = session [] "abc abc\n" in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = host session Zenbu_app.Session.Language_code_action in
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Code_actions
            |> List.exists (String.equal "actions: 1"))
      in
      expect
        (Zenbu_app.Session.inspect session Zenbu_app.Session.Code_actions
        |> List.exists (String.equal "commands: deny all"))
        "code-action results were not inspectable with their command policy";
      let session = Zenbu_app.Session.handle_input session enter in
      expect
        (String.starts_with ~prefix:"action"
           (Zenbu_app.Session.contents session))
        ("a selected code action did not apply its checked workspace edit: "
        ^ String.concat " | "
            (Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts)))

let code_action_cancellation_test () =
  let contents = "abc\n" in
  let session = session [ "--delay-code-action" ] contents in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = host session Zenbu_app.Session.Language_code_action in
      expect
        (Zenbu_app.Session.inspect session Zenbu_app.Session.Code_actions
        |> List.exists (String.equal "actions: 0"))
        "an in-flight code action was not inspectable";
      let session = Zenbu_app.Session.handle_input session escape in
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "pending requests: 0"))
      in
      let _, frame = Zenbu_app.Session.render session in
      expect
        ((not (frame_contains frame "Language code actions"))
        && String.equal (Zenbu_app.Session.contents session) contents)
        "cancelling an in-flight code action left a selectable or applied \
         result")

let code_action_command_denial_test () =
  let contents = "abc\n" in
  let session = session [ "--code-action-command" ] contents in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = host session Zenbu_app.Session.Language_code_action in
      let session =
        wait_session session (fun session ->
            let _, frame = Zenbu_app.Session.render session in
            frame_contains frame "command denied: fake.execute")
      in
      let session = Zenbu_app.Session.handle_input session enter in
      expect
        (String.equal (Zenbu_app.Session.contents session) contents
        && Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
           |> List.exists
                (String.equal
                   "message: code action rejected: server command is denied \
                    (fake.execute)"))
        "a server command attached to a code action was not denied")

let code_action_server_failure_test () =
  let contents = "abc\n" in
  let session = session [ "--code-action-error" ] contents in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session = host session Zenbu_app.Session.Language_code_action in
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
            |> List.exists
                 (String.equal
                    "message: language code action failed: fake code action \
                     failure"))
      in
      expect
        (String.equal (Zenbu_app.Session.contents session) contents
        && Zenbu_app.Session.inspect session Zenbu_app.Session.Code_actions
           |> List.exists (String.equal "state: none"))
        "a code-action server failure did not fail closed")

let code_action_unopened_target_test () =
  let target = Filename.temp_file "zenbu-m11-code-action-closed" ".ml" in
  Fun.protect
    ~finally:(fun () -> try Sys.remove target with Sys_error _ -> ())
    (fun () ->
      (match
         Zenbu_app.File_io.save_atomic ~path:target ~contents:"let target = 1\n"
       with
      | Ok () -> ()
      | Error error -> fail (Zenbu_app.File_io.to_string error));
      let contents = "let source = 1\n" in
      let session = session [ "--code-action-path"; target ] contents in
      Fun.protect
        ~finally:(fun () -> Zenbu_app.Session.close session)
        (fun () ->
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready"))
          in
          let session = host session Zenbu_app.Session.Language_code_action in
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
                |> List.exists
                     (String.starts_with
                        ~prefix:
                          "message: language code action failed: workspace \
                           edit has no buffer snapshot"))
          in
          expect
            (String.equal (Zenbu_app.Session.contents session) contents)
            "a code action targeting an unopened file changed the source buffer"))

let code_action_workspace_failure_test () =
  let open_target_in_split session target =
    let session = host session Zenbu_app.Session.Split_vertical in
    let session = host session Zenbu_app.Session.Open_buffer in
    let session = Zenbu_app.Session.handle_input session (text_input target) in
    Zenbu_app.Session.handle_input session enter
  in
  let target = Filename.temp_file "zenbu-m11-code-action-conflict" ".ml" in
  Fun.protect
    ~finally:(fun () -> try Sys.remove target with Sys_error _ -> ())
    (fun () ->
      (match
         Zenbu_app.File_io.save_atomic ~path:target ~contents:"let target = 1\n"
       with
      | Ok () -> ()
      | Error error -> fail (Zenbu_app.File_io.to_string error));
      let source = "let source = 1\n" in
      let session = session [ "--code-action-conflict-path"; target ] source in
      let current = ref session in
      Fun.protect
        ~finally:(fun () -> Zenbu_app.Session.close !current)
        (fun () ->
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready"))
          in
          let session = open_target_in_split session target in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          let session = host session Zenbu_app.Session.Language_code_action in
          let session =
            wait_session session (fun session ->
                let _, frame = Zenbu_app.Session.render session in
                frame_contains frame "apply fake code action")
          in
          let session = Zenbu_app.Session.handle_input session enter in
          expect
            (String.equal (Zenbu_app.Session.contents session) source
            && Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
               |> List.exists
                    (String.starts_with ~prefix:"message: code action rejected:")
            )
            "a rejected multi-buffer code action partially changed its source";
          let session = host session Zenbu_app.Session.Focus_next_pane in
          current := session;
          expect
            (String.equal
               (Zenbu_app.Session.contents session)
               "let target = 1\n")
            "a rejected multi-buffer code action partially changed its target"))

let cross_file_definition_session_test () =
  let target = Filename.temp_file "zenbu-m11-definition" ".ml" in
  Fun.protect
    ~finally:(fun () -> try Sys.remove target with Sys_error _ -> ())
    (fun () ->
      (match
         Zenbu_app.File_io.save_atomic ~path:target ~contents:"let target = 1\n"
       with
      | Ok () -> ()
      | Error error -> fail (Zenbu_app.File_io.to_string error));
      let session =
        session [ "--definition-path"; target ] "let source = 1\n"
      in
      let current = ref session in
      Fun.protect
        ~finally:(fun () -> Zenbu_app.Session.close !current)
        (fun () ->
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready"))
          in
          let session = host session Zenbu_app.Session.Language_definition in
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.file_path session = Some target)
          in
          current := session;
          expect
            (Zenbu_app.Session.buffer_count session = 2)
            "cross-file definition did not retain both buffers";
          expect
            (String.equal
               (Zenbu_app.Session.contents session)
               "let target = 1\n")
            "cross-file definition did not open the definition target";
          let selections =
            Zenbu_model_api.Editor_context.selections
              (Zenbu_app.Session.context session)
          in
          let primary =
            List.nth selections.selections selections.primary_index
          in
          expect
            (primary.anchor_offset = 0 && primary.head_offset = 1)
            "cross-file definition did not select the target range"))

let background_buffer_language_poll_test () =
  let target = Filename.temp_file "zenbu-m11-background" ".ml" in
  Fun.protect
    ~finally:(fun () -> try Sys.remove target with Sys_error _ -> ())
    (fun () ->
      (match
         Zenbu_app.File_io.save_atomic ~path:target ~contents:"let target = 1\n"
       with
      | Ok () -> ()
      | Error error -> fail (Zenbu_app.File_io.to_string error));
      let session = session [] "let source = 1\n" in
      let current = ref session in
      Fun.protect
        ~finally:(fun () -> Zenbu_app.Session.close !current)
        (fun () ->
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready"))
          in
          let session = host session Zenbu_app.Session.Split_vertical in
          let session = host session Zenbu_app.Session.Open_buffer in
          let session =
            Zenbu_app.Session.handle_input session
              ( Zenbu_model_api.Input_event.text_input target |> function
                | Ok input -> input
                | Error error -> fail (Zenbu_kernel.Error.to_string error) )
            |> fun session -> Zenbu_app.Session.handle_input session enter
          in
          expect
            (List.length (Zenbu_app.Session.language_wakeup_fds session) = 2)
            "workspace did not expose both buffer language wakeups";
          let session = host session Zenbu_app.Session.Focus_next_pane in
          let session =
            Zenbu_app.Session.handle_input session
              ( Zenbu_model_api.Input_event.logical_text "i" |> function
                | Ok input -> Zenbu_model_api.Input_event.key_press input
                | Error error -> fail (Zenbu_kernel.Error.to_string error) )
            |> fun session ->
            Zenbu_app.Session.handle_input session
              ( Zenbu_model_api.Input_event.text_input "!" |> function
                | Ok input -> input
                | Error error -> fail (Zenbu_kernel.Error.to_string error) )
            |> fun session -> Zenbu_app.Session.handle_input session escape
          in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready"))
          in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          current := session;
          expect
            (Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "diagnostics: 1"))
            "background buffer diagnostics were not drained into its buffer"))

let open_target_in_split session target =
  let session = host session Zenbu_app.Session.Split_vertical in
  let session = host session Zenbu_app.Session.Open_buffer in
  let session = Zenbu_app.Session.handle_input session (text_input target) in
  Zenbu_app.Session.handle_input session enter

let cross_file_workspace_rename_test () =
  let target = Filename.temp_file "zenbu-m11-rename" ".ml" in
  Fun.protect
    ~finally:(fun () -> try Sys.remove target with Sys_error _ -> ())
    (fun () ->
      (match
         Zenbu_app.File_io.save_atomic ~path:target ~contents:"let target = 1\n"
       with
      | Ok () -> ()
      | Error error -> fail (Zenbu_app.File_io.to_string error));
      let session = session [ "--rename-path"; target ] "let source = 1\n" in
      let current = ref session in
      Fun.protect
        ~finally:(fun () -> Zenbu_app.Session.close !current)
        (fun () ->
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready"))
          in
          let session = open_target_in_split session target in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          let session = host session Zenbu_app.Session.Language_rename in
          let session =
            Zenbu_app.Session.handle_input session (text_input "ren")
          in
          let session = Zenbu_app.Session.handle_input session enter in
          let session =
            wait_session session (fun session ->
                String.starts_with ~prefix:"ren"
                  (Zenbu_app.Session.contents session))
          in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          current := session;
          if
            not
              (String.starts_with ~prefix:"ren"
                 (Zenbu_app.Session.contents session))
          then
            fail
              ("cross-file rename did not apply to the second open buffer: "
              ^ Zenbu_app.Session.contents session)))

let cross_file_workspace_rename_requires_open_target_test () =
  let target = Filename.temp_file "zenbu-m11-rename-closed" ".ml" in
  Fun.protect
    ~finally:(fun () -> try Sys.remove target with Sys_error _ -> ())
    (fun () ->
      (match
         Zenbu_app.File_io.save_atomic ~path:target ~contents:"let target = 1\n"
       with
      | Ok () -> ()
      | Error error -> fail (Zenbu_app.File_io.to_string error));
      let session = session [ "--rename-path"; target ] "let source = 1\n" in
      let current = ref session in
      Fun.protect
        ~finally:(fun () -> Zenbu_app.Session.close !current)
        (fun () ->
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready"))
          in
          let session = host session Zenbu_app.Session.Language_rename in
          let session =
            Zenbu_app.Session.handle_input session (text_input "ren")
          in
          let session = Zenbu_app.Session.handle_input session enter in
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
                |> List.exists
                     (String.equal
                        ("message: language rename failed: workspace edit has \
                          no buffer snapshot for file://" ^ target)))
          in
          current := session;
          expect
            (String.equal
               (Zenbu_app.Session.contents session)
               "let source = 1\n")
            "a rejected cross-file rename partially changed the source buffer"))

let cross_file_workspace_rename_rejects_stale_target_test () =
  let target = Filename.temp_file "zenbu-m11-rename-stale" ".ml" in
  Fun.protect
    ~finally:(fun () -> try Sys.remove target with Sys_error _ -> ())
    (fun () ->
      (match
         Zenbu_app.File_io.save_atomic ~path:target ~contents:"let target = 1\n"
       with
      | Ok () -> ()
      | Error error -> fail (Zenbu_app.File_io.to_string error));
      let session =
        session [ "--rename-path"; target; "--delay-rename" ] "let source = 1\n"
      in
      let current = ref session in
      Fun.protect
        ~finally:(fun () -> Zenbu_app.Session.close !current)
        (fun () ->
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready"))
          in
          let session = open_target_in_split session target in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          let session = host session Zenbu_app.Session.Language_rename in
          let session =
            Zenbu_app.Session.handle_input session (text_input "ren")
          in
          let session = Zenbu_app.Session.handle_input session enter in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          let session =
            Zenbu_app.Session.handle_input session (logical_key "i")
          in
          let session =
            Zenbu_app.Session.handle_input session (text_input "!")
          in
          let session = Zenbu_app.Session.handle_input session escape in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
                |> List.exists
                     (String.equal
                        ("message: rename rejected: workspace edit source \
                          changed for file://" ^ target)))
          in
          current := session;
          expect
            (String.equal
               (Zenbu_app.Session.contents session)
               "let source = 1\n")
            "a stale cross-file rename partially changed the source buffer"))

let cross_file_workspace_rename_is_all_or_none_test () =
  let target = Filename.temp_file "zenbu-m11-rename-conflict" ".ml" in
  Fun.protect
    ~finally:(fun () -> try Sys.remove target with Sys_error _ -> ())
    (fun () ->
      (match
         Zenbu_app.File_io.save_atomic ~path:target ~contents:"let target = 1\n"
       with
      | Ok () -> ()
      | Error error -> fail (Zenbu_app.File_io.to_string error));
      let session =
        session [ "--rename-conflict-path"; target ] "let source = 1\n"
      in
      let current = ref session in
      Fun.protect
        ~finally:(fun () -> Zenbu_app.Session.close !current)
        (fun () ->
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready"))
          in
          let session = open_target_in_split session target in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          let session = host session Zenbu_app.Session.Language_rename in
          let session =
            Zenbu_app.Session.handle_input session (text_input "ren")
          in
          let session = Zenbu_app.Session.handle_input session enter in
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
                |> List.exists
                     (String.starts_with ~prefix:"message: rename rejected:"))
          in
          expect
            (String.equal
               (Zenbu_app.Session.contents session)
               "let source = 1\n")
            "a failed cross-file rename partially changed the source buffer";
          let session = host session Zenbu_app.Session.Focus_next_pane in
          current := session;
          expect
            (String.equal
               (Zenbu_app.Session.contents session)
               "let target = 1\n")
            "a failed cross-file rename partially changed the target buffer"))

let cross_file_workspace_apply_edit_test () =
  let target = Filename.temp_file "zenbu-m11-apply-edit" ".ml" in
  Fun.protect
    ~finally:(fun () -> try Sys.remove target with Sys_error _ -> ())
    (fun () ->
      (match
         Zenbu_app.File_io.save_atomic ~path:target ~contents:"let target = 1\n"
       with
      | Ok () -> ()
      | Error error -> fail (Zenbu_app.File_io.to_string error));
      let session =
        session [ "--apply-edit-path"; target ] "let source = 1\n"
      in
      let current = ref session in
      Fun.protect
        ~finally:(fun () -> Zenbu_app.Session.close !current)
        (fun () ->
          let session =
            wait_session session (fun session ->
                Zenbu_app.Session.inspect session Zenbu_app.Session.Language
                |> List.exists (String.equal "state: ready"))
          in
          let session = open_target_in_split session target in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          let session =
            Zenbu_app.Session.handle_input session (logical_key "i")
          in
          let session =
            Zenbu_app.Session.handle_input session (text_input "!")
          in
          let session = Zenbu_app.Session.handle_input session escape in
          let session =
            wait_session session (fun session ->
                String.starts_with ~prefix:"X"
                  (Zenbu_app.Session.contents session))
          in
          let session = host session Zenbu_app.Session.Focus_next_pane in
          current := session;
          expect
            (String.starts_with ~prefix:"Y"
               (Zenbu_app.Session.contents session))
            "workspace/applyEdit did not apply to the second open buffer"))

let apply_edit_session_test () =
  let session = session [ "--apply-edit" ] "abc\n" in
  Fun.protect
    ~finally:(fun () -> Zenbu_app.Session.close session)
    (fun () ->
      let session =
        wait_session session (fun session ->
            String.starts_with ~prefix:"X" (Zenbu_app.Session.contents session))
      in
      expect
        (String.starts_with ~prefix:"X" (Zenbu_app.Session.contents session))
        "workspace/applyEdit current-document request was not applied")

let save_as_activation_test () =
  let path = Filename.temp_file "zenbu-m11-save-as" ".ml" in
  (try Sys.remove path with Sys_error _ -> ());
  let session =
    Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim
      ~contents:"let x = 1\n" ~language_registry:(registry [])
      ~dimensions:Zenbu_view.Renderer.{ columns = 80; rows = 12 }
      ()
    |> function
    | Ok session -> session
    | Error error -> fail (Zenbu_kernel.Error.to_string error)
  in
  let current = ref session in
  Fun.protect
    ~finally:(fun () ->
      Zenbu_app.Session.close !current;
      try Sys.remove path with Sys_error _ -> ())
    (fun () ->
      let session = host session Zenbu_app.Session.Save_as in
      let session =
        String.to_seq path |> List.of_seq
        |> List.fold_left
             (fun session character ->
               Zenbu_app.Session.handle_input session
                 ( Zenbu_model_api.Input_event.text_input
                     (String.make 1 character)
                 |> function
                   | Ok input -> input
                   | Error error -> fail (Zenbu_kernel.Error.to_string error) ))
             session
      in
      let session = Zenbu_app.Session.handle_input session enter in
      current := session;
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Language
            |> List.exists (String.equal "state: ready"))
      in
      let session =
        wait_session session (fun session ->
            Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
            |> List.exists (String.equal "message: language: fake saved"))
      in
      expect
        (Zenbu_app.Session.inspect session Zenbu_app.Session.Syntax
        |> List.exists (String.starts_with ~prefix:"language: ocaml"))
        "save-as did not rebind syntax for the new path")

let () =
  position_tests ();
  sync_tests ();
  synchronization_test "full";
  synchronization_test "incremental";
  feature_test ();
  stale_response_test ();
  stale_code_action_response_test ();
  stale_formatting_response_test ();
  formatting_cancellation_test ();
  malformed_formatting_response_test ();
  stale_symbol_response_test ();
  invalid_symbol_response_test ();
  code_action_resource_rejection_test ();
  crash_restart_test ();
  malformed_server_test ();
  trace_attribution_test ();
  session_integration_test ();
  document_formatting_session_test ();
  range_formatting_session_test ();
  formatting_noop_session_test ();
  formatting_dirty_invalid_syntax_test ();
  formatting_conflict_session_test ();
  formatting_server_failure_test ();
  document_symbol_session_test ();
  code_action_session_test ();
  code_action_cancellation_test ();
  code_action_command_denial_test ();
  code_action_server_failure_test ();
  code_action_unopened_target_test ();
  code_action_workspace_failure_test ();
  cross_file_definition_session_test ();
  background_buffer_language_poll_test ();
  cross_file_workspace_rename_test ();
  cross_file_workspace_rename_requires_open_target_test ();
  cross_file_workspace_rename_rejects_stale_target_test ();
  cross_file_workspace_rename_is_all_or_none_test ();
  cross_file_workspace_apply_edit_test ();
  apply_edit_session_test ();
  save_as_activation_test ();
  print_endline "M11 language tests passed"
