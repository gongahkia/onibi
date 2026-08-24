open Zenbu_language
module Language = Language
module Trace = Zenbu_model_api.Trace
module Trace_event = Zenbu_model_api.Trace_event
module Profiler = Zenbu_model_api.Profiler

type request_kind = Hover | Definition | Completion | Code_action | Rename

type workspace_edit = {
  uri : string;
  source_contents : string;
  edits : Language.text_edit list;
}

type code_action = {
  title : string;
  edits : workspace_edit list option;
  command : string option;
  disabled_reason : string option;
}

type event =
  | Initialized
  | Diagnostics of {
      document_version : int option;
      diagnostics : Language.diagnostic list;
    }
  | Hover_result of {
      request_id : int;
      document_version : int;
      byte_offset : int;
      hover : Language.hover option;
    }
  | Definition_result of {
      request_id : int;
      document_version : int;
      byte_offset : int;
      targets : Language.definition_target list;
    }
  | Completion_result of {
      request_id : int;
      document_version : int;
      byte_offset : int;
      items : Language.completion list;
    }
  | Code_action_result of {
      request_id : int;
      document_version : int;
      start_offset : int;
      stop_offset : int;
      actions : code_action list;
    }
  | Rename_result of {
      request_id : int;
      document_version : int;
      byte_offset : int;
      edits : workspace_edit list;
    }
  | Apply_edit of { request_id : int; edits : workspace_edit list }
  | Server_message of string
  | Request_failed of {
      request_id : int;
      kind : request_kind;
      document_version : int;
      reason : string;
    }
  | Server_failed of string
  | Server_exited of string

type sync_kind = No_sync | Full | Incremental
type pending_kind = Initialize | Shutdown | Feature of request_kind

type pending = {
  id : int;
  kind : pending_kind;
  document_version : int;
  lsp_version : int;
  contents : string;
  workspace_documents : (string * string) list;
  byte_offset : int option;
  stop_offset : int option;
  mutable cancelled : bool;
  started_at : float;
}

type process = {
  generation : int;
  pid : int;
  stdin : Unix.file_descr;
  stdout : in_channel;
  stderr : in_channel;
  mutable terminating : bool;
}

type t = {
  config : Language.Server_config.t;
  document_id : string;
  uri : string;
  workspace_root : string;
  trace : Trace.t;
  profiler : Profiler.t;
  lock : Mutex.t;
  writer_lock : Mutex.t;
  wake_read : Unix.file_descr;
  wake_write : Unix.file_descr;
  events : event Queue.t;
  mutable state : Language.server_state;
  mutable process : process option;
  mutable next_request_id : int;
  mutable next_inbound_id : int;
  mutable pending : pending list;
  mutable inbound : (int * Jsonrpc.Id.t) list;
  mutable current_contents : string;
  mutable workspace_documents : (string * string) list;
  mutable document_version : int;
  mutable lsp_version : int;
  mutable lsp_versions : (int * int * string) list;
  mutable position_encoding : Language.Position.encoding;
  mutable sync_kind : sync_kind;
  mutable save_includes_text : bool;
  mutable pending_save : bool;
  mutable opened : bool;
  mutable generation : int;
  mutable diagnostic_count : int;
  mutable last_error : string option;
  mutable trace_execution_id : int;
  mutable disposed : bool;
  stderr_buffer : Buffer.t;
}

let max_header_bytes = 32 * 1024
let max_message_bytes = 8 * 1024 * 1024
let max_hover_bytes = 16 * 1024
let max_completion_items = 1_024
let max_code_action_items = 128
let max_diagnostics = 1_024
let max_stderr_bytes = 16 * 1024
let max_message_bytes_for_ui = 2_048
let sigpipe_ignored = ref false

let ignore_sigpipe () =
  if not !sigpipe_ignored then (
    Sys.set_signal Sys.sigpipe Sys.Signal_ignore;
    sigpipe_ignored := true)

let request_kind_name = function
  | Hover -> "hover"
  | Definition -> "definition"
  | Completion -> "completion"
  | Code_action -> "code-action"
  | Rename -> "rename"

let sync_name = function
  | No_sync -> "none"
  | Full -> "full"
  | Incremental -> "incremental"

let trace t ?request_id ?document_version ~stage ~outcome ?detail () =
  Mutex.lock t.lock;
  let execution_id = t.trace_execution_id in
  Mutex.unlock t.lock;
  Trace.emit_lazy t.trace (fun () ->
      Trace_event.Language_service
        {
          execution_id;
          stage;
          server_id = Language.Server_config.id t.config;
          request_id;
          document_version;
          outcome;
          detail;
        })

let trim_text limit text =
  let buffer = Buffer.create (min limit (String.length text)) in
  let rec loop index =
    if index >= String.length text || Buffer.length buffer >= limit then ()
    else
      let character = text.[index] in
      let code = Char.code character in
      if code < 32 && character <> '\n' && character <> '\t' then
        Buffer.add_char buffer ' '
      else Buffer.add_char buffer character;
      loop (index + 1)
  in
  loop 0;
  let value = Buffer.contents buffer in
  if String.length text > String.length value then value ^ "…" else value

let push_event t event =
  Mutex.lock t.lock;
  Queue.add event t.events;
  Mutex.unlock t.lock;
  try ignore (Unix.write_substring t.wake_write "x" 0 1)
  with Unix.Unix_error _ -> ()

let drain_wakeup fd =
  let bytes = Bytes.create 256 in
  let rec loop () =
    try
      match Unix.read fd bytes 0 (Bytes.length bytes) with
      | 0 -> ()
      | _ -> loop ()
    with Unix.Unix_error ((Unix.EAGAIN | Unix.EWOULDBLOCK), _, _) -> ()
  in
  loop ()

let data_to_json value =
  let rec convert = function
    | Language.Data.Null -> `Null
    | Bool value -> `Bool value
    | Number value -> `Float value
    | Text value -> `String value
    | List values -> `List (List.map convert values)
    | Object fields ->
        `Assoc (List.map (fun (key, value) -> (key, convert value)) fields)
  in
  convert value

let assoc fields = (`Assoc fields : Yojson.Safe.t)
let structured fields = (`Assoc fields : Jsonrpc.Structured.t)

let response_id = function
  | `Int value -> Some value
  | `String value -> int_of_string_opt value

let json_of_structured value = Jsonrpc.Structured.yojson_of_t value

let position_json position =
  assoc
    [
      ("line", `Int position.Language.Position.line);
      ("character", `Int position.character);
    ]

let range_json range =
  assoc
    [
      ("start", position_json range.Language.Position.start_);
      ("end", position_json range.end_);
    ]

let packet_to_string packet =
  Jsonrpc.Packet.yojson_of_t packet |> Yojson.Safe.to_string

let write_all fd text =
  let rec loop offset =
    if offset < String.length text then
      let written =
        Unix.write_substring fd text offset (String.length text - offset)
      in
      if written = 0 then raise (Failure "short LSP protocol write")
      else loop (offset + written)
  in
  loop 0

let send_packet t packet =
  let process =
    Mutex.lock t.lock;
    let value =
      match (t.state, t.process) with
      | ( ( Language.Starting | Language.Initializing | Language.Ready
          | Language.Shutting_down ),
          Some process ) ->
          Ok process
      | _ -> Error "language server is unavailable"
    in
    Mutex.unlock t.lock;
    value
  in
  Result.bind process (fun process ->
      let body = packet_to_string packet in
      if String.length body > max_message_bytes then
        Error "outgoing LSP message exceeds the configured size limit"
      else
        let header =
          Printf.sprintf "Content-Length: %d\r\n\r\n" (String.length body)
        in
        try
          Mutex.lock t.writer_lock;
          Fun.protect
            ~finally:(fun () -> Mutex.unlock t.writer_lock)
            (fun () ->
              write_all process.stdin header;
              write_all process.stdin body);
          Ok ()
        with
        | Unix.Unix_error (error, _, _) -> Error (Unix.error_message error)
        | Failure error -> Error error)

let notify t ~method_ ~params =
  send_packet t
    (Jsonrpc.Packet.Notification
       (Jsonrpc.Notification.create ~method_ ~params:(structured params) ()))

let append_stderr t text =
  Mutex.lock t.lock;
  let remaining = max_stderr_bytes - Buffer.length t.stderr_buffer in
  if remaining > 0 then
    Buffer.add_string t.stderr_buffer (trim_text remaining text);
  Mutex.unlock t.lock

let record_failure t ?(event = true) reason =
  let changed =
    Mutex.lock t.lock;
    let changed = t.state <> Language.Failed in
    t.state <- Language.Failed;
    t.last_error <- Some reason;
    Mutex.unlock t.lock;
    changed
  in
  trace t ~stage:"server-failure" ~outcome:"failed" ~detail:reason ();
  if event && changed then push_event t (Server_failed reason)

let reap_process_bounded process =
  let deadline = Unix.gettimeofday () +. 1.0 in
  let rec wait () =
    try
      match Unix.waitpid [ Unix.WNOHANG ] process.pid with
      | 0, _ when Unix.gettimeofday () < deadline ->
          ignore (Unix.select [] [] [] 0.025);
          wait ()
      | 0, _ -> (
          (try Unix.kill process.pid Sys.sigkill with Unix.Unix_error _ -> ());
          let kill_deadline = Unix.gettimeofday () +. 1.0 in
          let rec after_kill () =
            match Unix.waitpid [ Unix.WNOHANG ] process.pid with
            | 0, _ when Unix.gettimeofday () < kill_deadline ->
                ignore (Unix.select [] [] [] 0.025);
                after_kill ()
            | _ -> ()
          in
          try after_kill () with Unix.Unix_error (Unix.ECHILD, _, _) -> ())
      | _ -> ()
    with Unix.Unix_error (Unix.ECHILD, _, _) -> ()
  in
  (try Unix.kill process.pid Sys.sigterm with Unix.Unix_error _ -> ());
  wait ()

let terminate_process t process =
  let owns_cleanup =
    Mutex.lock t.lock;
    let owns_cleanup = not process.terminating in
    if owns_cleanup then process.terminating <- true;
    Mutex.unlock t.lock;
    owns_cleanup
  in
  if owns_cleanup then (
    (try Unix.close process.stdin with Unix.Unix_error _ -> ());
    reap_process_bounded process;
    (* Reaping the child closes its write ends, so a concurrent reader has an
       EOF/error available before these channel closes acquire its lock. *)
    (try close_in_noerr process.stdout with _ -> ());
    try close_in_noerr process.stderr with _ -> ())

let content_length headers =
  headers
  |> List.find_map (fun line ->
      match String.index_opt line ':' with
      | None -> None
      | Some index ->
          let key =
            String.sub line 0 index |> String.trim |> String.lowercase_ascii
          in
          if String.equal key "content-length" then
            String.sub line (index + 1) (String.length line - index - 1)
            |> String.trim |> int_of_string_opt
          else None)

let read_packet channel =
  let rec first_header () =
    match input_line channel with
    | line when String.trim line = "" -> first_header ()
    | line -> line
  in
  let first = first_header () in
  let rec headers total values =
    if total > max_header_bytes then
      Error "LSP header exceeds the configured size limit"
    else
      let line = input_line channel in
      if String.trim line = "" then Ok (List.rev values)
      else headers (total + String.length line) (line :: values)
  in
  Result.bind
    (headers (String.length first) [ first ])
    (fun headers ->
      match content_length headers with
      | None -> Error "LSP message has no valid Content-Length header"
      | Some length when length < 0 -> Error "LSP Content-Length is negative"
      | Some length when length > max_message_bytes ->
          Error "LSP message exceeds the configured size limit"
      | Some length -> (
          let body = really_input_string channel length in
          try
            let json = Yojson.Safe.from_string body in
            Ok (Jsonrpc.Packet.t_of_yojson json)
          with
          | Jsonrpc.Json.Of_json (message, _) ->
              Error ("invalid JSON-RPC packet: " ^ message)
          | Yojson.Json_error message -> Error ("invalid JSON: " ^ message)
          | exception_ ->
              Error ("invalid JSON-RPC packet: " ^ Printexc.to_string exception_)
          ))

let range_of_lsp ~contents ~encoding range =
  Language.Position.range_to_offsets ~contents ~encoding
    {
      Language.Position.start_ =
        {
          line = range.Lsp.Types.Range.start.line;
          character = range.start.character;
        };
      end_ = { line = range.end_.line; character = range.end_.character };
    }

let uri_of_document_uri uri =
  Lsp.Types.DocumentUri.yojson_of_t uri |> Yojson.Safe.Util.to_string

let diagnostic_severity = function
  | Some Lsp.Types.DiagnosticSeverity.Error -> Language.Error
  | Some Warning -> Language.Warning
  | Some Information -> Language.Information
  | Some Hint | None -> Language.Hint

let diagnostic_message = function
  | `String text -> text
  | `MarkupContent content -> content.Lsp.Types.MarkupContent.value

let diagnostic_of_lsp ~document_id ~document_version ~contents ~encoding value =
  Result.map
    (fun (start_offset, stop_offset) ->
      {
        Language.start_offset;
        stop_offset;
        severity = diagnostic_severity value.Lsp.Types.Diagnostic.severity;
        message =
          trim_text max_message_bytes_for_ui
            (diagnostic_message value.Lsp.Types.Diagnostic.message);
        source = value.Lsp.Types.Diagnostic.source;
        code =
          Option.map
            (function
              | `Int value -> string_of_int value | `String value -> value)
            value.Lsp.Types.Diagnostic.code;
        document_id;
        document_version;
      })
    (range_of_lsp ~contents ~encoding value.Lsp.Types.Diagnostic.range)

let hover_text hover =
  match hover.Lsp.Types.Hover.contents with
  | `MarkupContent content -> content.Lsp.Types.MarkupContent.value
  | `MarkedString value ->
      Option.value ~default:value.Lsp.Types.MarkedString.value
        (Option.map
           (fun language ->
             language ^ "\n" ^ value.Lsp.Types.MarkedString.value)
           value.Lsp.Types.MarkedString.language)
  | `List values ->
      values
      |> List.map (fun value ->
          Option.value ~default:value.Lsp.Types.MarkedString.value
            (Option.map
               (fun language ->
                 language ^ "\n" ^ value.Lsp.Types.MarkedString.value)
               value.Lsp.Types.MarkedString.language))
      |> String.concat "\n\n"

let hover_of_lsp ~contents ~encoding value =
  let offsets =
    Option.bind value.Lsp.Types.Hover.range (fun range ->
        range_of_lsp ~contents ~encoding range |> Result.to_option)
  in
  let start_offset, stop_offset =
    Option.value ~default:(None, None)
      (Option.map
         (fun (start_offset, stop_offset) ->
           (Some start_offset, Some stop_offset))
         offsets)
  in
  {
    Language.text = trim_text max_hover_bytes (hover_text value);
    start_offset;
    stop_offset;
  }

let text_edit_of_lsp ~contents ~encoding value =
  Result.map
    (fun (start_offset, stop_offset) ->
      {
        Language.start_offset;
        stop_offset;
        replacement = value.Lsp.Types.TextEdit.newText;
      })
    (range_of_lsp ~contents ~encoding value.range)

let completion_of_lsp ~contents ~encoding value =
  let text_edit =
    match value.Lsp.Types.CompletionItem.textEdit with
    | None -> Ok None
    | Some (`TextEdit edit) ->
        Result.map Option.some (text_edit_of_lsp ~contents ~encoding edit)
    | Some (`InsertReplaceEdit edit) ->
        Result.map Option.some
          (text_edit_of_lsp ~contents ~encoding
             (Lsp.Types.TextEdit.create ~newText:edit.newText
                ~range:edit.replace))
  in
  Result.bind text_edit (fun text_edit ->
      let additional_text_edits =
        Option.value ~default:[]
          value.Lsp.Types.CompletionItem.additionalTextEdits
        |> List.map (text_edit_of_lsp ~contents ~encoding)
      in
      let rec collect values result =
        match values with
        | [] -> Ok (List.rev result)
        | value :: rest ->
            Result.bind value (fun value -> collect rest (value :: result))
      in
      Result.map
        (fun additional_text_edits ->
          {
            Language.label = trim_text 512 value.Lsp.Types.CompletionItem.label;
            detail =
              Option.map (trim_text 512) value.Lsp.Types.CompletionItem.detail;
            documentation =
              Option.map
                (function
                  | `String text -> trim_text max_hover_bytes text
                  | `MarkupContent content ->
                      trim_text max_hover_bytes
                        content.Lsp.Types.MarkupContent.value)
                value.Lsp.Types.CompletionItem.documentation;
            sort_text = value.Lsp.Types.CompletionItem.sortText;
            filter_text = value.Lsp.Types.CompletionItem.filterText;
            insert_text = value.Lsp.Types.CompletionItem.insertText;
            text_edit;
            additional_text_edits;
            snippet =
              value.Lsp.Types.CompletionItem.insertTextFormat
              = Some Lsp.Types.InsertTextFormat.Snippet;
            deprecated =
              Option.value ~default:false
                value.Lsp.Types.CompletionItem.deprecated;
          })
        (collect additional_text_edits []))

let targets_of_lsp ~contents ~encoding value =
  let values =
    match value with
    | `Location locations ->
        locations
        |> List.map (fun location ->
            Result.map
              (fun (start_offset, stop_offset) ->
                {
                  Language.uri =
                    uri_of_document_uri location.Lsp.Types.Location.uri;
                  start_offset;
                  stop_offset;
                })
              (range_of_lsp ~contents ~encoding
                 location.Lsp.Types.Location.range))
    | `LocationLink links ->
        links
        |> List.map (fun link ->
            Result.map
              (fun (start_offset, stop_offset) ->
                {
                  Language.uri =
                    uri_of_document_uri link.Lsp.Types.LocationLink.targetUri;
                  start_offset;
                  stop_offset;
                })
              (range_of_lsp ~contents ~encoding
                 link.Lsp.Types.LocationLink.targetSelectionRange))
  in
  let rec collect values result =
    match values with
    | [] -> Ok (List.rev result)
    | value :: rest ->
        Result.bind value (fun value -> collect rest (value :: result))
  in
  collect values []

let field name = function
  | `Assoc fields -> List.assoc_opt name fields
  | _ -> None

let list_field name json =
  match field name json with Some (`List values) -> values | _ -> []

let workspace_edits_of_json ~current_uri ~contents ~workspace_documents
    ~encoding json =
  let edits_for_uri uri text_edits =
    let contents =
      if String.equal uri current_uri then Some contents
      else List.assoc_opt uri workspace_documents
    in
    match contents with
    | None -> Error ("workspace edit has no buffer snapshot for " ^ uri)
    | Some contents ->
        let parsed = List.map Lsp.Types.TextEdit.t_of_yojson text_edits in
        let rec collect values result =
          match values with
          | [] ->
              Ok { uri; source_contents = contents; edits = List.rev result }
          | value :: rest ->
              Result.bind (text_edit_of_lsp ~contents ~encoding value)
                (fun value -> collect rest (value :: result))
        in
        collect parsed []
  in
  try
    let from_changes =
      match field "changes" json with
      | Some (`Assoc values) ->
          List.map
            (fun (uri, edits) ->
              edits_for_uri uri
                (match edits with `List values -> values | _ -> []))
            values
      | _ -> []
    in
    let from_document_changes =
      list_field "documentChanges" json
      |> List.map (fun change ->
          match (field "textDocument" change, field "edits" change) with
          | Some text_document, Some (`List edits) -> (
              match field "uri" text_document with
              | Some (`String uri) -> edits_for_uri uri edits
              | _ -> Error "workspace edit text document has no URI")
          | _ ->
              Error "workspace edit contains an unsupported resource operation")
    in
    let rec collect values result =
      match values with
      | [] -> Ok (List.rev result)
      | value :: rest ->
          Result.bind value (fun value -> collect rest (value :: result))
    in
    collect (from_changes @ from_document_changes) []
  with Jsonrpc.Json.Of_json (message, _) ->
    Error ("invalid workspace edit: " ^ message)

let code_action_of_lsp ~current_uri ~contents ~workspace_documents ~encoding =
  function
  | `Command command ->
      let title = trim_text 512 command.Lsp.Types.Command.title in
      if title = "" then Error "code action command has an empty title"
      else
        Ok
          {
            title;
            edits = None;
            command = Some command.command;
            disabled_reason = None;
          }
  | `CodeAction action ->
      let title = trim_text 512 action.Lsp.Types.CodeAction.title in
      if title = "" then Error "code action has an empty title"
      else
        let edits =
          match action.edit with
          | None -> Ok None
          | Some edit ->
              workspace_edits_of_json ~current_uri ~contents
                ~workspace_documents ~encoding
                (Lsp.Types.WorkspaceEdit.yojson_of_t edit)
              |> Result.map Option.some
        in
        Result.map
          (fun edits ->
            {
              title;
              edits;
              command =
                Option.map
                  (fun (command : Lsp.Types.Command.t) -> command.command)
                  action.command;
              disabled_reason =
                Option.map
                  (fun (disabled : Lsp.Types.CodeAction.disabled) ->
                    disabled.reason)
                  action.disabled;
            })
          edits

let code_actions_of_json ~current_uri ~contents ~workspace_documents ~encoding
    json =
  try
    let values =
      match json with
      | `Null -> []
      | _ ->
          Lsp.Types.CodeActionResult.t_of_yojson json
          |> Option.value ~default:[]
    in
    let values =
      if List.length values > max_code_action_items then
        List.filteri (fun index _ -> index < max_code_action_items) values
      else values
    in
    let rec collect values result =
      match values with
      | [] -> Ok (List.rev result)
      | value :: rest ->
          Result.bind
            (code_action_of_lsp ~current_uri ~contents ~workspace_documents
               ~encoding value) (fun value -> collect rest (value :: result))
    in
    collect values []
  with Jsonrpc.Json.Of_json (message, _) -> Error message

let client_capabilities () =
  assoc
    [
      ( "general",
        assoc
          [
            ( "positionEncodings",
              `List [ `String "utf-8"; `String "utf-16"; `String "utf-32" ] );
          ] );
      ( "textDocument",
        assoc
          [
            ( "synchronization",
              assoc
                [
                  ("didSave", `Bool true); ("dynamicRegistration", `Bool false);
                ] );
            ( "hover",
              assoc
                [
                  ( "contentFormat",
                    `List [ `String "plaintext"; `String "markdown" ] );
                ] );
            ("definition", assoc [ ("linkSupport", `Bool true) ]);
            ( "completion",
              assoc
                [
                  ( "completionItem",
                    assoc
                      [
                        ("snippetSupport", `Bool false);
                        ( "documentationFormat",
                          `List [ `String "plaintext"; `String "markdown" ] );
                        ("deprecatedSupport", `Bool true);
                      ] );
                ] );
            ("rename", assoc [ ("prepareSupport", `Bool true) ]);
            ( "codeAction",
              assoc
                [
                  ("dynamicRegistration", `Bool false);
                  ("isPreferredSupport", `Bool false);
                  ("disabledSupport", `Bool true);
                ] );
            ("publishDiagnostics", assoc [ ("relatedInformation", `Bool false) ]);
          ] );
      ( "workspace",
        assoc
          [
            ("configuration", `Bool true);
            ("workspaceEdit", assoc [ ("documentChanges", `Bool false) ]);
          ] );
    ]

let initialize_params t =
  let config_options =
    Option.value ~default:`Null
      (Option.map data_to_json
         (Language.Server_config.initialization_options t.config))
  in
  [
    ("processId", `Int (Unix.getpid ()));
    ("rootUri", `String (Language.Uri.file_of_path t.workspace_root));
    ("capabilities", client_capabilities ());
    ("initializationOptions", config_options);
    ("clientInfo", assoc [ ("name", `String "Zenbu") ]);
  ]

let did_open_params t =
  [
    ( "textDocument",
      assoc
        [
          ("uri", `String t.uri);
          ( "languageId",
            `String (List.hd (Language.Server_config.language_ids t.config)) );
          ("version", `Int t.lsp_version);
          ("text", `String t.current_contents);
        ] );
  ]

let did_change_full t =
  [
    ( "textDocument",
      assoc [ ("uri", `String t.uri); ("version", `Int t.lsp_version) ] );
    ("contentChanges", `List [ assoc [ ("text", `String t.current_contents) ] ]);
  ]

let did_change_incremental t changes =
  [
    ( "textDocument",
      assoc [ ("uri", `String t.uri); ("version", `Int t.lsp_version) ] );
    ( "contentChanges",
      `List
        (List.map
           (fun (change : Language.Sync.content_change) ->
             assoc
               [
                 ("range", range_json change.range);
                 ("text", `String change.text);
               ])
           changes) );
  ]

let flush_pending_save t =
  Mutex.lock t.lock;
  let ready = t.state = Language.Ready && t.opened in
  let pending = t.pending_save in
  let include_text = t.save_includes_text in
  let contents = t.current_contents in
  Mutex.unlock t.lock;
  if not (ready && pending) then Ok false
  else
    let params =
      [ ("textDocument", assoc [ ("uri", `String t.uri) ]) ]
      @ if include_text then [ ("text", `String contents) ] else []
    in
    Result.map
      (fun () ->
        Mutex.lock t.lock;
        t.pending_save <- false;
        Mutex.unlock t.lock;
        true)
      (notify t ~method_:"textDocument/didSave" ~params)

let send_configuration t =
  match Language.Server_config.settings t.config with
  | None -> Ok ()
  | Some settings ->
      notify t ~method_:"workspace/didChangeConfiguration"
        ~params:[ ("settings", data_to_json settings) ]

let send_request ?byte_offset ?stop_offset t kind ~document_version ~contents
    ~params =
  let pending =
    Mutex.lock t.lock;
    let result =
      match t.state with
      | Language.Initializing | Language.Ready | Language.Shutting_down ->
          let id = t.next_request_id in
          t.next_request_id <- id + 1;
          let pending =
            {
              id;
              kind;
              document_version;
              lsp_version = t.lsp_version;
              contents;
              workspace_documents = t.workspace_documents;
              byte_offset;
              stop_offset;
              cancelled = false;
              started_at = Unix.gettimeofday ();
            }
          in
          t.pending <- pending :: t.pending;
          Ok pending
      | _ -> Error "language server is unavailable"
    in
    Mutex.unlock t.lock;
    result
  in
  Result.bind pending (fun pending ->
      let method_ =
        match kind with
        | Initialize -> "initialize"
        | Shutdown -> "shutdown"
        | Feature Hover -> "textDocument/hover"
        | Feature Definition -> "textDocument/definition"
        | Feature Completion -> "textDocument/completion"
        | Feature Code_action -> "textDocument/codeAction"
        | Feature Rename -> "textDocument/rename"
      in
      match
        send_packet t
          (Jsonrpc.Packet.Request
             (Jsonrpc.Request.create ~id:(`Int pending.id) ~method_
                ~params:(structured params) ()))
      with
      | Ok () ->
          trace t ~request_id:pending.id ~document_version ~stage:"request"
            ~outcome:"sent" ~detail:method_ ();
          Ok pending.id
      | Error reason ->
          Mutex.lock t.lock;
          t.pending <- List.filter (fun item -> item.id <> pending.id) t.pending;
          Mutex.unlock t.lock;
          record_failure t reason;
          Error reason)

let send_initialized_and_open t =
  match notify t ~method_:"initialized" ~params:[] with
  | Error reason -> record_failure t reason
  | Ok () -> (
      match
        notify t ~method_:"textDocument/didOpen" ~params:(did_open_params t)
      with
      | Error reason -> record_failure t reason
      | Ok () -> (
          match send_configuration t with
          | Error reason -> record_failure t reason
          | Ok () -> (
              Mutex.lock t.lock;
              t.opened <- true;
              t.state <- Language.Ready;
              t.lsp_versions <-
                [ (t.lsp_version, t.document_version, t.current_contents) ];
              Mutex.unlock t.lock;
              match flush_pending_save t with
              | Error reason -> record_failure t reason
              | Ok _ ->
                  trace t ~document_version:t.document_version ~stage:"did-open"
                    ~outcome:"sent" ();
                  push_event t Initialized)))

let configure_from_initialize t result =
  let capabilities = result.Lsp.Types.InitializeResult.capabilities in
  let encoding =
    match capabilities.positionEncoding with
    | Some Lsp.Types.PositionEncodingKind.UTF8 -> Language.Position.Utf8
    | Some UTF32 -> Language.Position.Utf32
    | Some UTF16 | Some (Other _) | None -> Language.Position.Utf16
  in
  let sync_kind, save_includes_text =
    match capabilities.textDocumentSync with
    | Some (`TextDocumentSyncKind Lsp.Types.TextDocumentSyncKind.Full) ->
        (Full, false)
    | Some (`TextDocumentSyncKind Incremental) -> (Incremental, false)
    | Some (`TextDocumentSyncKind None) -> (No_sync, false)
    | Some (`TextDocumentSyncOptions options) ->
        let sync =
          match options.Lsp.Types.TextDocumentSyncOptions.change with
          | Some Lsp.Types.TextDocumentSyncKind.Incremental -> Incremental
          | Some Full -> Full
          | Some None | None -> No_sync
        in
        let save_includes_text =
          match options.Lsp.Types.TextDocumentSyncOptions.save with
          | Some (`SaveOptions value) ->
              Option.value ~default:false
                value.Lsp.Types.SaveOptions.includeText
          | Some (`Bool _) | None -> false
        in
        (sync, save_includes_text)
    | None -> (No_sync, false)
  in
  Mutex.lock t.lock;
  t.position_encoding <- encoding;
  t.sync_kind <- sync_kind;
  t.save_includes_text <- save_includes_text;
  Mutex.unlock t.lock;
  trace t ~stage:"initialized" ~outcome:"succeeded"
    ~detail:
      ("position="
      ^ Language.Position.encoding_name encoding
      ^ " sync=" ^ sync_name sync_kind)
    ()

let remove_pending t id =
  Mutex.lock t.lock;
  let pending = List.find_opt (fun pending -> pending.id = id) t.pending in
  t.pending <- List.filter (fun pending -> pending.id <> id) t.pending;
  Mutex.unlock t.lock;
  pending

let feature_stage = function
  | Hover -> Profiler.Language_hover
  | Definition -> Profiler.Language_definition
  | Completion -> Profiler.Language_completion
  | Code_action -> Profiler.Language_code_action
  | Rename -> Profiler.Language_rename

let feature_response t pending result =
  let elapsed = max 0. (Unix.gettimeofday () -. pending.started_at) in
  match pending.kind with
  | Feature kind -> (
      Profiler.record t.profiler (feature_stage kind) ~seconds:elapsed;
      let request_detail =
        request_kind_name kind ^ " lsp-version="
        ^ string_of_int pending.lsp_version
      in
      if pending.cancelled then
        trace t ~request_id:pending.id
          ~document_version:pending.document_version ~stage:"stale-response"
          ~outcome:"discarded" ()
      else
        match result with
        | Error error ->
            let reason =
              trim_text max_message_bytes_for_ui
                error.Jsonrpc.Response.Error.message
            in
            trace t ~request_id:pending.id
              ~document_version:pending.document_version ~stage:"response"
              ~outcome:"failed"
              ~detail:(request_detail ^ ": " ^ reason)
              ();
            push_event t
              (Request_failed
                 {
                   request_id = pending.id;
                   kind;
                   document_version = pending.document_version;
                   reason;
                 })
        | Ok json -> (
            let decode () =
              try
                match kind with
                | Hover ->
                    let hover =
                      match json with
                      | `Null -> Ok None
                      | _ ->
                          let value = Lsp.Types.Hover.t_of_yojson json in
                          Ok
                            (Some
                               (hover_of_lsp ~contents:pending.contents
                                  ~encoding:t.position_encoding value))
                    in
                    Result.map
                      (fun hover ->
                        Hover_result
                          {
                            request_id = pending.id;
                            document_version = pending.document_version;
                            byte_offset =
                              Option.value ~default:(-1) pending.byte_offset;
                            hover;
                          })
                      hover
                | Definition ->
                    let targets =
                      match json with
                      | `Null -> Ok []
                      | _ ->
                          Lsp.Types.Locations.t_of_yojson json
                          |> targets_of_lsp ~contents:pending.contents
                               ~encoding:t.position_encoding
                    in
                    Result.map
                      (fun targets ->
                        Definition_result
                          {
                            request_id = pending.id;
                            document_version = pending.document_version;
                            byte_offset =
                              Option.value ~default:(-1) pending.byte_offset;
                            targets;
                          })
                      targets
                | Completion ->
                    let values =
                      match json with
                      | `Null -> []
                      | `List values ->
                          List.map Lsp.Types.CompletionItem.t_of_yojson values
                      | _ -> (Lsp.Types.CompletionList.t_of_yojson json).items
                    in
                    let values =
                      if List.length values > max_completion_items then
                        List.filteri
                          (fun index _ -> index < max_completion_items)
                          values
                      else values
                    in
                    let rec collect values result =
                      match values with
                      | [] -> Ok (List.rev result)
                      | value :: rest ->
                          Result.bind
                            (completion_of_lsp ~contents:pending.contents
                               ~encoding:t.position_encoding value)
                            (fun value -> collect rest (value :: result))
                    in
                    Result.map
                      (fun items ->
                        Completion_result
                          {
                            request_id = pending.id;
                            document_version = pending.document_version;
                            byte_offset =
                              Option.value ~default:(-1) pending.byte_offset;
                            items;
                          })
                      (collect values [])
                | Code_action ->
                    Result.map
                      (fun actions ->
                        Code_action_result
                          {
                            request_id = pending.id;
                            document_version = pending.document_version;
                            start_offset =
                              Option.value ~default:(-1) pending.byte_offset;
                            stop_offset =
                              Option.value ~default:(-1) pending.stop_offset;
                            actions;
                          })
                      (code_actions_of_json ~current_uri:t.uri
                         ~contents:pending.contents
                         ~workspace_documents:pending.workspace_documents
                         ~encoding:t.position_encoding json)
                | Rename ->
                    Result.map
                      (fun edits ->
                        Rename_result
                          {
                            request_id = pending.id;
                            document_version = pending.document_version;
                            byte_offset =
                              Option.value ~default:(-1) pending.byte_offset;
                            edits;
                          })
                      (workspace_edits_of_json ~current_uri:t.uri
                         ~contents:pending.contents
                         ~workspace_documents:pending.workspace_documents
                         ~encoding:t.position_encoding json)
              with
              | Jsonrpc.Json.Of_json (message, _) -> Error message
              | exception_ -> Error (Printexc.to_string exception_)
            in
            let decoded =
              Profiler.measure t.profiler Profiler.Lsp_decode decode
            in
            match decoded with
            | Error reason ->
                trace t ~request_id:pending.id
                  ~document_version:pending.document_version ~stage:"response"
                  ~outcome:"invalid"
                  ~detail:(request_detail ^ ": " ^ reason)
                  ();
                push_event t
                  (Request_failed
                     {
                       request_id = pending.id;
                       kind;
                       document_version = pending.document_version;
                       reason;
                     })
            | Ok event ->
                trace t ~request_id:pending.id
                  ~document_version:pending.document_version ~stage:"response"
                  ~outcome:"succeeded" ~detail:request_detail ();
                push_event t event))
  | Initialize | Shutdown -> assert false

let handle_response t response =
  match response_id response.Jsonrpc.Response.id with
  | None ->
      trace t ~stage:"response" ~outcome:"ignored"
        ~detail:"non-integer response ID" ()
  | Some id -> (
      match remove_pending t id with
      | None ->
          trace t ~request_id:id ~stage:"response" ~outcome:"ignored"
            ~detail:"unknown response ID" ()
      | Some pending -> (
          match pending.kind with
          | Initialize -> (
              match response.result with
              | Error error -> record_failure t error.message
              | Ok json -> (
                  try
                    let result = Lsp.Types.InitializeResult.t_of_yojson json in
                    configure_from_initialize t result;
                    send_initialized_and_open t
                  with
                  | Jsonrpc.Json.Of_json (message, _) ->
                      record_failure t
                        ("invalid initialize response: " ^ message)
                  | exception_ ->
                      record_failure t
                        ("invalid initialize response: "
                        ^ Printexc.to_string exception_)))
          | Shutdown ->
              trace t ~request_id:id ~stage:"shutdown" ~outcome:"acknowledged"
                ()
          | Feature _ -> feature_response t pending response.result))

let immediate_response t id result =
  ignore (send_packet t (Jsonrpc.Packet.Response { id; result }))

let parse_apply_edit t params =
  try
    let parameters = Lsp.Types.ApplyWorkspaceEditParams.t_of_yojson params in
    Mutex.lock t.lock;
    let contents = t.current_contents in
    let workspace_documents = t.workspace_documents in
    let encoding = t.position_encoding in
    Mutex.unlock t.lock;
    workspace_edits_of_json ~current_uri:t.uri ~contents ~workspace_documents
      ~encoding
      (Lsp.Types.WorkspaceEdit.yojson_of_t
         parameters.Lsp.Types.ApplyWorkspaceEditParams.edit)
  with
  | Jsonrpc.Json.Of_json (message, _) -> Error message
  | exception_ -> Error (Printexc.to_string exception_)

let handle_server_request t request =
  let params =
    Option.value ~default:`Null
      (Option.map json_of_structured request.Jsonrpc.Request.params)
  in
  match request.method_ with
  | "workspace/applyEdit" -> (
      match parse_apply_edit t params with
      | Error reason ->
          immediate_response t request.id
            (Error
               (Jsonrpc.Response.Error.make
                  ~code:Jsonrpc.Response.Error.Code.InvalidParams
                  ~message:reason ()));
          trace t ~stage:"server-request" ~outcome:"rejected" ~detail:reason ()
      | Ok edits ->
          Mutex.lock t.lock;
          let id = t.next_inbound_id in
          t.next_inbound_id <- id + 1;
          t.inbound <- (id, request.id) :: t.inbound;
          Mutex.unlock t.lock;
          push_event t (Apply_edit { request_id = id; edits }))
  | "workspace/configuration" ->
      let settings =
        Option.value ~default:Language.Data.Null
          (Language.Server_config.settings t.config)
        |> data_to_json
      in
      let values =
        match field "items" params with
        | Some (`List items) -> `List (List.map (fun _ -> settings) items)
        | _ -> `List []
      in
      immediate_response t request.id (Ok values)
  | "window/showMessageRequest" -> immediate_response t request.id (Ok `Null)
  | "client/registerCapability" | "client/unregisterCapability" ->
      immediate_response t request.id (Ok `Null)
  | method_ ->
      immediate_response t request.id
        (Error
           (Jsonrpc.Response.Error.make
              ~code:Jsonrpc.Response.Error.Code.MethodNotFound
              ~message:("unsupported language-server request: " ^ method_)
              ()));
      trace t ~stage:"server-request" ~outcome:"rejected" ~detail:method_ ()

let handle_notification t notification =
  let params =
    Option.value ~default:`Null
      (Option.map json_of_structured notification.Jsonrpc.Notification.params)
  in
  match notification.method_ with
  | "textDocument/publishDiagnostics" -> (
      try
        let value = Lsp.Types.PublishDiagnosticsParams.t_of_yojson params in
        let uri =
          uri_of_document_uri value.Lsp.Types.PublishDiagnosticsParams.uri
        in
        if not (String.equal uri t.uri) then ()
        else
          let lsp_version = value.Lsp.Types.PublishDiagnosticsParams.version in
          let document_version, contents =
            match lsp_version with
            | Some lsp_version -> (
                match
                  List.find_opt
                    (fun (candidate, _, _) -> candidate = lsp_version)
                    t.lsp_versions
                with
                | Some (_, document_version, contents) ->
                    (Some document_version, contents)
                | None -> (None, t.current_contents))
            | None -> (None, t.current_contents)
          in
          let diagnostics =
            value.Lsp.Types.PublishDiagnosticsParams.diagnostics
            |> List.filteri (fun index _ -> index < max_diagnostics)
            |> List.filter_map (fun value ->
                diagnostic_of_lsp ~document_id:t.document_id
                  ~document_version:
                    (Option.value ~default:t.document_version document_version)
                  ~contents ~encoding:t.position_encoding value
                |> Result.to_option)
          in
          Mutex.lock t.lock;
          t.diagnostic_count <- List.length diagnostics;
          Mutex.unlock t.lock;
          trace t ?document_version ~stage:"diagnostics" ~outcome:"received"
            ~detail:(string_of_int (List.length diagnostics) ^ " diagnostics")
            ();
          push_event t (Diagnostics { document_version; diagnostics })
      with
      | Jsonrpc.Json.Of_json (message, _) ->
          trace t ~stage:"notification" ~outcome:"invalid" ~detail:message ()
      | exception_ ->
          trace t ~stage:"notification" ~outcome:"invalid"
            ~detail:(Printexc.to_string exception_)
            ())
  | "window/showMessage" | "window/logMessage" ->
      let message =
        match field "message" params with
        | Some (`String message) -> trim_text max_message_bytes_for_ui message
        | _ -> "language server message"
      in
      trace t ~stage:"notification" ~outcome:"received"
        ~detail:notification.method_ ();
      push_event t (Server_message message)
  | "$/progress" -> trace t ~stage:"progress" ~outcome:"ignored" ()
  | _ ->
      trace t ~stage:"notification" ~outcome:"ignored"
        ~detail:notification.method_ ()

let reader_loop t process =
  let rec loop () =
    match read_packet process.stdout with
    | Error reason ->
        record_failure t reason;
        terminate_process t process
    | Ok (Jsonrpc.Packet.Response response) ->
        handle_response t response;
        loop ()
    | Ok (Jsonrpc.Packet.Notification notification) ->
        handle_notification t notification;
        loop ()
    | Ok (Jsonrpc.Packet.Request request) ->
        handle_server_request t request;
        loop ()
    | Ok (Jsonrpc.Packet.Batch_response responses) ->
        List.iter (handle_response t) responses;
        loop ()
    | Ok (Jsonrpc.Packet.Batch_call calls) ->
        List.iter
          (function
            | `Request request -> handle_server_request t request
            | `Notification notification -> handle_notification t notification)
          calls;
        loop ()
  in
  try loop () with
  | End_of_file ->
      Mutex.lock t.lock;
      let current_generation = t.generation in
      let shutting_down = t.state = Language.Shutting_down in
      if process.generation = current_generation then
        t.state <- (if shutting_down then Language.Stopped else Language.Failed);
      Mutex.unlock t.lock;
      if shutting_down then trace t ~stage:"server-exit" ~outcome:"stopped" ()
      else
        let reason =
          let status =
            try
              match Unix.waitpid [ Unix.WNOHANG ] process.pid with
              | 0, _ -> ""
              | _, Unix.WEXITED status -> " exit=" ^ string_of_int status
              | _, Unix.WSIGNALED signal -> " signal=" ^ string_of_int signal
              | _, Unix.WSTOPPED signal -> " stopped=" ^ string_of_int signal
            with Unix.Unix_error _ -> ""
          in
          let stderr = Buffer.contents t.stderr_buffer |> trim_text 1024 in
          "language server exited" ^ status
          ^ if String.length stderr = 0 then "" else ": " ^ stderr
        in
        trace t ~stage:"server-exit" ~outcome:"failed" ~detail:reason ();
        push_event t (Server_exited reason)
  | exception_ ->
      record_failure t ("LSP reader failed: " ^ Printexc.to_string exception_);
      terminate_process t process

let stderr_loop t process =
  try
    while true do
      append_stderr t (input_line process.stderr ^ "\n")
    done
  with
  | End_of_file -> ()
  | _ -> ()

let path_environment overrides =
  let inherited = Unix.environment () |> Array.to_list in
  let replace values (key, value) =
    (key ^ "=" ^ value)
    :: List.filter
         (fun entry ->
           match String.index_opt entry '=' with
           | None -> true
           | Some index -> not (String.equal key (String.sub entry 0 index)))
         values
  in
  List.fold_left replace inherited overrides |> Array.of_list

let executable_path environment executable =
  if not (Filename.is_implicit executable) then executable
  else
    let path =
      Array.to_list environment
      |> List.find_map (fun entry ->
          if String.starts_with ~prefix:"PATH=" entry then
            Some (String.sub entry 5 (String.length entry - 5))
          else None)
      |> Option.value ~default:""
    in
    path |> String.split_on_char ':'
    |> List.map (fun directory ->
        Filename.concat
          (if String.length directory = 0 then "." else directory)
          executable)
    |> List.find_opt (fun candidate -> Sys.file_exists candidate)
    |> Option.value ~default:executable

let spawn t =
  let environment =
    path_environment (Language.Server_config.environment t.config)
  in
  let executable =
    executable_path environment (Language.Server_config.executable t.config)
  in
  let stdin_read, stdin_write = Unix.pipe () in
  let stdout_read, stdout_write = Unix.pipe () in
  let stderr_read, stderr_write = Unix.pipe () in
  try
    let argv =
      Array.of_list (executable :: Language.Server_config.argv t.config)
    in
    let pid =
      Unix.create_process_env executable argv environment stdin_read
        stdout_write stderr_write
    in
    Unix.close stdin_read;
    Unix.close stdout_write;
    Unix.close stderr_write;
    Mutex.lock t.lock;
    t.generation <- t.generation + 1;
    let process =
      {
        generation = t.generation;
        pid;
        stdin = stdin_write;
        stdout = Unix.in_channel_of_descr stdout_read;
        stderr = Unix.in_channel_of_descr stderr_read;
        terminating = false;
      }
    in
    t.process <- Some process;
    t.state <- Language.Initializing;
    t.last_error <- None;
    t.opened <- false;
    t.lsp_version <- 1;
    t.lsp_versions <- [];
    t.sync_kind <- No_sync;
    t.position_encoding <- Language.Position.Utf16;
    t.pending_save <- false;
    Mutex.unlock t.lock;
    trace t ~stage:"server-start" ~outcome:"started" ~detail:executable ();
    ignore (Thread.create (reader_loop t) process);
    ignore (Thread.create (stderr_loop t) process);
    ignore
      (send_request t Initialize ~document_version:t.document_version
         ~contents:t.current_contents ~params:(initialize_params t));
    Ok ()
  with
  | Unix.Unix_error (error, _, _) ->
      List.iter
        (fun fd -> try Unix.close fd with Unix.Unix_error _ -> ())
        [
          stdin_read;
          stdin_write;
          stdout_read;
          stdout_write;
          stderr_read;
          stderr_write;
        ];
      Error ("could not start " ^ executable ^ ": " ^ Unix.error_message error)
  | exception_ ->
      List.iter
        (fun fd -> try Unix.close fd with Unix.Unix_error _ -> ())
        [
          stdin_read;
          stdin_write;
          stdout_read;
          stdout_write;
          stderr_read;
          stderr_write;
        ];
      Error
        ("could not start " ^ executable ^ ": " ^ Printexc.to_string exception_)

let start ~config ~document_id ~document_version ~file_path ~contents
    ~trace:trace_value ~profiler =
  ignore_sigpipe ();
  let wake_read, wake_write = Unix.pipe () in
  Unix.set_nonblock wake_read;
  Unix.set_nonblock wake_write;
  let workspace_root =
    Language.Workspace.discover_root
      ~markers:(Language.Server_config.root_markers config)
      ~file_path
  in
  let value =
    {
      config;
      document_id;
      uri = Language.Uri.file_of_path file_path;
      workspace_root;
      trace = trace_value;
      profiler;
      lock = Mutex.create ();
      writer_lock = Mutex.create ();
      wake_read;
      wake_write;
      events = Queue.create ();
      state = Language.Starting;
      process = None;
      next_request_id = 1;
      next_inbound_id = 1;
      pending = [];
      inbound = [];
      current_contents = contents;
      workspace_documents = [ (Language.Uri.file_of_path file_path, contents) ];
      document_version;
      lsp_version = 1;
      lsp_versions = [];
      position_encoding = Language.Position.Utf16;
      sync_kind = No_sync;
      save_includes_text = false;
      pending_save = false;
      opened = false;
      generation = 0;
      diagnostic_count = 0;
      last_error = None;
      trace_execution_id = 0;
      disposed = false;
      stderr_buffer = Buffer.create 256;
    }
  in
  match spawn value with
  | Ok () -> value
  | Error reason ->
      record_failure value reason;
      value

let status t =
  Mutex.lock t.lock;
  let sync_kind =
    match t.sync_kind with
    | No_sync -> `None
    | Full -> `Full
    | Incremental -> `Incremental
  in
  let value =
    {
      Language.language_id =
        (match Language.Server_config.language_ids t.config with
        | language :: _ -> Some language
        | [] -> None);
      server_id = Some (Language.Server_config.id t.config);
      executable = Some (Language.Server_config.executable t.config);
      workspace_root = Some t.workspace_root;
      state = t.state;
      position_encoding =
        (match t.state with
        | Language.Ready | Language.Shutting_down -> Some t.position_encoding
        | Stopped | Starting | Initializing | Failed -> None);
      sync_kind =
        (match t.state with
        | Language.Ready | Language.Shutting_down -> Some sync_kind
        | Stopped | Starting | Initializing | Failed -> None);
      pending_requests =
        List.length
          (List.filter
             (fun pending ->
               match pending.kind with Feature _ -> true | _ -> false)
             t.pending);
      diagnostic_count = t.diagnostic_count;
      last_error = t.last_error;
    }
  in
  Mutex.unlock t.lock;
  value

let wakeup_fd t = t.wake_read

let set_execution_id t ~execution_id =
  Mutex.lock t.lock;
  t.trace_execution_id <- execution_id;
  Mutex.unlock t.lock

let set_workspace_documents t documents =
  Mutex.lock t.lock;
  t.workspace_documents <- documents;
  Mutex.unlock t.lock

let drain t =
  drain_wakeup t.wake_read;
  Mutex.lock t.lock;
  let rec take values =
    if Queue.is_empty t.events then List.rev values
    else take (Queue.take t.events :: values)
  in
  let values = take [] in
  Mutex.unlock t.lock;
  values

let cancel_pending t pending =
  pending.cancelled <- true;
  ignore
    (notify t ~method_:"$/cancelRequest" ~params:[ ("id", `Int pending.id) ]);
  trace t ~request_id:pending.id ~document_version:pending.document_version
    ~stage:"cancel" ~outcome:"sent" ()

let cancel_stale t ~document_version =
  Mutex.lock t.lock;
  let values =
    t.pending
    |> List.filter (fun pending ->
        match pending.kind with
        | Feature _ ->
            pending.document_version <> document_version
            && not pending.cancelled
        | Initialize | Shutdown -> false)
  in
  Mutex.unlock t.lock;
  List.iter (cancel_pending t) values

let cancel t kind =
  Mutex.lock t.lock;
  let values =
    t.pending
    |> List.filter (fun pending ->
        match pending.kind with
        | Feature candidate -> candidate = kind && not pending.cancelled
        | Initialize | Shutdown -> false)
  in
  Mutex.unlock t.lock;
  List.iter (cancel_pending t) values

let notify_change t ~source_contents ~contents ~document_version ~edits =
  Mutex.lock t.lock;
  t.current_contents <- contents;
  t.workspace_documents <-
    (t.uri, contents)
    :: List.filter
         (fun (uri, _) -> not (String.equal uri t.uri))
         t.workspace_documents;
  t.document_version <- document_version;
  t.diagnostic_count <- 0;
  let ready = t.state = Language.Ready && t.opened in
  let sync_kind = t.sync_kind in
  let encoding = t.position_encoding in
  if ready then t.lsp_version <- t.lsp_version + 1;
  if ready then
    t.lsp_versions <-
      (t.lsp_version, document_version, contents)
      :: List.filter
           (fun (version, _, _) -> version <> t.lsp_version)
           t.lsp_versions;
  Mutex.unlock t.lock;
  cancel_stale t ~document_version;
  if ready then
    let sent =
      Profiler.measure t.profiler Profiler.Language_sync (fun () ->
          match sync_kind with
          | No_sync -> Ok ()
          | Full ->
              notify t ~method_:"textDocument/didChange"
                ~params:(did_change_full t)
          | Incremental ->
              Result.bind
                (Language.Sync.incremental_changes ~contents:source_contents
                   ~encoding ~edits ~expected:contents) (fun changes ->
                  notify t ~method_:"textDocument/didChange"
                    ~params:(did_change_incremental t changes)))
    in
    match sent with
    | Ok () ->
        trace t ~document_version ~stage:"did-change" ~outcome:"sent"
          ~detail:(sync_name sync_kind) ()
    | Error reason -> record_failure t reason

let observe_document_version t ~document_version =
  Mutex.lock t.lock;
  t.document_version <- document_version;
  t.lsp_versions <-
    List.map
      (fun (lsp_version, mapped_version, contents) ->
        if lsp_version = t.lsp_version then
          (lsp_version, document_version, contents)
        else (lsp_version, mapped_version, contents))
      t.lsp_versions;
  Mutex.unlock t.lock

let notify_save t ~contents ~document_version =
  Mutex.lock t.lock;
  t.current_contents <- contents;
  t.workspace_documents <-
    (t.uri, contents)
    :: List.filter
         (fun (uri, _) -> not (String.equal uri t.uri))
         t.workspace_documents;
  t.document_version <- document_version;
  t.pending_save <- true;
  Mutex.unlock t.lock;
  match flush_pending_save t with
  | Ok true -> trace t ~document_version ~stage:"did-save" ~outcome:"sent" ()
  | Ok false -> ()
  | Error reason -> record_failure t reason

let request_position t kind byte_offset extra =
  Mutex.lock t.lock;
  let state = t.state in
  let contents = t.current_contents in
  let document_version = t.document_version in
  let encoding = t.position_encoding in
  let lsp_version = t.lsp_version in
  Mutex.unlock t.lock;
  if state <> Language.Ready then Error "language server is unavailable"
  else
    Result.bind
      (Language.Position.offset_to_position ~contents ~encoding ~byte_offset)
      (fun position ->
        let params =
          [
            ("textDocument", assoc [ ("uri", `String t.uri) ]);
            ("position", position_json position);
          ]
          @ extra
        in
        ignore lsp_version;
        send_request ~byte_offset t (Feature kind) ~document_version ~contents
          ~params)

let request_hover t ~byte_offset = request_position t Hover byte_offset []

let request_definition t ~byte_offset =
  request_position t Definition byte_offset []

let request_completion t ~byte_offset =
  request_position t Completion byte_offset
    [ ("context", assoc [ ("triggerKind", `Int 1) ]) ]

let request_code_actions t ~start_offset ~stop_offset =
  Mutex.lock t.lock;
  let state = t.state in
  let contents = t.current_contents in
  let document_version = t.document_version in
  let encoding = t.position_encoding in
  Mutex.unlock t.lock;
  if state <> Language.Ready then Error "language server is unavailable"
  else if start_offset < 0 || stop_offset < start_offset then
    Error "code action range is invalid"
  else
    Result.bind
      (Language.Position.offsets_to_range ~contents ~encoding ~start_offset
         ~stop_offset) (fun range ->
        let params =
          [
            ("textDocument", assoc [ ("uri", `String t.uri) ]);
            ("range", range_json range);
            ("context", assoc [ ("diagnostics", `List []) ]);
          ]
        in
        send_request ~byte_offset:start_offset ~stop_offset t
          (Feature Code_action) ~document_version ~contents ~params)

let request_rename t ~byte_offset ~new_name =
  if String.length new_name = 0 then Error "rename target must not be empty"
  else request_position t Rename byte_offset [ ("newName", `String new_name) ]

let respond_apply_edit t ~request_id ~applied ~reason =
  Mutex.lock t.lock;
  let id = List.assoc_opt request_id t.inbound in
  t.inbound <- List.remove_assoc request_id t.inbound;
  Mutex.unlock t.lock;
  Option.iter
    (fun id ->
      immediate_response t id
        (Ok
           (assoc
              ([ ("applied", `Bool applied) ]
              @ Option.to_list
                  (Option.map
                     (fun reason -> ("failureReason", `String reason))
                     reason)))))
    id

let close_process t process =
  (match
     send_request t Shutdown ~document_version:t.document_version
       ~contents:t.current_contents ~params:[]
   with
  | Ok _ | Error _ -> ());
  ignore (notify t ~method_:"exit" ~params:[]);
  terminate_process t process

let stop t =
  Mutex.lock t.lock;
  t.state <- Language.Shutting_down;
  let process = t.process in
  Mutex.unlock t.lock;
  Option.iter (close_process t) process;
  Mutex.lock t.lock;
  t.process <- None;
  t.state <- Language.Stopped;
  Mutex.unlock t.lock;
  trace t ~stage:"shutdown" ~outcome:"stopped" ()

let close t =
  Mutex.lock t.lock;
  let dispose = not t.disposed in
  if dispose then t.disposed <- true;
  Mutex.unlock t.lock;
  if dispose then (
    stop t;
    (try Unix.close t.wake_read with Unix.Unix_error _ -> ());
    try Unix.close t.wake_write with Unix.Unix_error _ -> ())

let restart t =
  Mutex.lock t.lock;
  let disposed = t.disposed in
  Mutex.unlock t.lock;
  if not disposed then (
    stop t;
    Mutex.lock t.lock;
    t.state <- Language.Starting;
    Mutex.unlock t.lock;
    match spawn t with Ok () -> () | Error reason -> record_failure t reason)
