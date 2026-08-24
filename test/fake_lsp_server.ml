(* A deterministic LSP peer used only by M11 tests and headless demos. It is
   deliberately small: the client remains responsible for malformed input,
   request ownership, and result validation. *)

let sync_mode = ref "incremental"
let delay_hover = ref false
let malformed = ref false
let crash_marker = ref None
let apply_edit = ref false
let definition_path = ref None
let rename_path = ref None
let delay_rename = ref false
let rename_conflict_path = ref None
let apply_edit_path = ref None
let code_action_path = ref None
let code_action_conflict_path = ref None
let delay_code_action = ref false
let code_action_resource = ref false
let code_action_command = ref false
let code_action_error = ref false
let delay_formatting = ref false
let formatting_noop = ref false
let formatting_malformed = ref false
let formatting_conflict = ref false
let formatting_error = ref false

let options =
  [
    ("--sync", Arg.Set_string sync_mode, "full or incremental");
    ("--delay-hover", Arg.Set delay_hover, "delay hover responses");
    ("--malformed", Arg.Set malformed, "emit malformed JSON after initialize");
    ( "--crash-once",
      Arg.String (fun path -> crash_marker := Some path),
      "exit after first initialize" );
    ("--apply-edit", Arg.Set apply_edit, "send one workspace/applyEdit request");
    ( "--definition-path",
      Arg.String (fun path -> definition_path := Some path),
      "return this file as the definition target" );
    ( "--rename-path",
      Arg.String (fun path -> rename_path := Some path),
      "include this file in rename edits" );
    ( "--delay-rename",
      Arg.Set delay_rename,
      "delay rename responses to exercise stale workspace snapshots" );
    ( "--rename-conflict-path",
      Arg.String (fun path -> rename_conflict_path := Some path),
      "include overlapping edits for this file in a rename response" );
    ( "--apply-edit-path",
      Arg.String (fun path -> apply_edit_path := Some path),
      "include this file in a workspace/applyEdit after didChange" );
    ( "--code-action-path",
      Arg.String (fun path -> code_action_path := Some path),
      "include this file in a code-action edit" );
    ( "--code-action-conflict-path",
      Arg.String (fun path -> code_action_conflict_path := Some path),
      "include overlapping edits for this file in a code action" );
    ( "--delay-code-action",
      Arg.Set delay_code_action,
      "delay code-action responses to exercise cancellation and staleness" );
    ( "--code-action-resource",
      Arg.Set code_action_resource,
      "return an unsupported resource operation in a code action" );
    ( "--code-action-command",
      Arg.Set code_action_command,
      "return a command-only code action" );
    ( "--code-action-error",
      Arg.Set code_action_error,
      "return a code-action server error" );
    ( "--delay-formatting",
      Arg.Set delay_formatting,
      "delay formatting responses to exercise cancellation and staleness" );
    ( "--formatting-noop",
      Arg.Set formatting_noop,
      "return a no-op formatting result" );
    ( "--formatting-malformed",
      Arg.Set formatting_malformed,
      "return a malformed formatting result" );
    ( "--formatting-conflict",
      Arg.Set formatting_conflict,
      "return overlapping formatting edits" );
    ( "--formatting-error",
      Arg.Set formatting_error,
      "return a formatting server error" );
  ]

let () = Arg.parse options (fun _ -> ()) "fake_lsp_server"

let write_all fd text =
  let rec loop offset =
    if offset < String.length text then
      let written =
        Unix.write_substring fd text offset (String.length text - offset)
      in
      if written = 0 then exit 2 else loop (offset + written)
  in
  loop 0

let send json =
  let body = Yojson.Safe.to_string json in
  write_all Unix.stdout
    (Printf.sprintf "Content-Length: %d\r\n\r\n" (String.length body));
  write_all Unix.stdout body

let field name = function
  | `Assoc fields -> List.assoc_opt name fields
  | _ -> None

let string_field name json =
  match field name json with Some (`String value) -> Some value | _ -> None

let int_field name json =
  match field name json with Some (`Int value) -> Some value | _ -> None

let has_formatting_options params =
  match field "options" params with
  | Some options ->
      int_field "tabSize" options = Some 2
      && field "insertSpaces" options = Some (`Bool true)
  | None -> false

let input = stdin

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

let read_packet () =
  let first = input_line input in
  let rec headers values =
    let line = input_line input in
    if String.trim line = "" then List.rev values else headers (line :: values)
  in
  let headers = headers [ first ] in
  match content_length headers with
  | None -> failwith "missing content length"
  | Some length -> really_input_string input length |> Yojson.Safe.from_string

let response id result =
  send (`Assoc [ ("jsonrpc", `String "2.0"); ("id", id); ("result", result) ])

let response_error id message =
  send
    (`Assoc
       [
         ("jsonrpc", `String "2.0");
         ("id", id);
         ( "error",
           `Assoc [ ("code", `Int (-32001)); ("message", `String message) ] );
       ])

let notification method_ params =
  send
    (`Assoc
       [
         ("jsonrpc", `String "2.0");
         ("method", `String method_);
         ("params", params);
       ])

let request id method_ params =
  send
    (`Assoc
       [
         ("jsonrpc", `String "2.0");
         ("id", `Int id);
         ("method", `String method_);
         ("params", params);
       ])

let position line character =
  `Assoc [ ("line", `Int line); ("character", `Int character) ]

let range start_line start_character end_line end_character =
  `Assoc
    [
      ("start", position start_line start_character);
      ("end", position end_line end_character);
    ]

let apply_change contents change =
  match string_field "text" change with
  | None -> contents
  | Some text -> (
      match field "range" change with
      | None -> text
      | Some range ->
          let line_offsets text =
            let offsets = ref [ 0 ] in
            String.iteri
              (fun index character ->
                if character = '\n' then offsets := (index + 1) :: !offsets)
              text;
            List.rev !offsets
          in
          let offset point =
            match point with
            | `Assoc _ ->
                let line = Option.value ~default:0 (int_field "line" point) in
                let character =
                  Option.value ~default:0 (int_field "character" point)
                in
                let line_start =
                  List.nth_opt (line_offsets contents) line
                  |> Option.value ~default:(String.length contents)
                in
                min (String.length contents) (line_start + character)
            | _ -> 0
          in
          let start =
            field "start" range |> Option.map offset |> Option.value ~default:0
          in
          let stop =
            field "end" range |> Option.map offset
            |> Option.value ~default:start
          in
          String.sub contents 0 start
          ^ text
          ^ String.sub contents stop (String.length contents - stop))

let diagnostic ~uri ~version contents =
  let stop = if String.length contents = 0 then 0 else 1 in
  notification "textDocument/publishDiagnostics"
    (`Assoc
       [
         ("uri", `String uri);
         ("version", `Int version);
         ( "diagnostics",
           `List
             [
               `Assoc
                 [
                   ("range", range 0 0 0 stop);
                   ("severity", `Int 1);
                   ("source", `String "zenbu-fake");
                   ("code", `String "fake");
                   ("message", `String ("sync:" ^ contents));
                 ];
             ] );
       ])

let initialized_result () =
  let change = if String.equal !sync_mode "full" then 1 else 2 in
  `Assoc
    [
      ( "capabilities",
        `Assoc
          [
            ("positionEncoding", `String "utf-16");
            ( "textDocumentSync",
              `Assoc
                [
                  ("openClose", `Bool true);
                  ("change", `Int change);
                  ("save", `Bool true);
                ] );
            ("hoverProvider", `Bool true);
            ("definitionProvider", `Bool true);
            ("completionProvider", `Assoc []);
            ("codeActionProvider", `Bool true);
            ("documentFormattingProvider", `Bool true);
            ("documentRangeFormattingProvider", `Bool true);
            ("renameProvider", `Bool true);
          ] );
    ]

let request_position params =
  field "position" params |> Option.value ~default:(position 0 0)

let () =
  let contents = ref "" in
  let uri = ref "file:///missing.ml" in
  let version = ref 1 in
  let initialized = ref false in
  let workspace_apply_edit_sent = ref false in
  let rec loop () =
    let packet = read_packet () in
    let method_ = string_field "method" packet in
    let params = field "params" packet |> Option.value ~default:(`Assoc []) in
    let id = field "id" packet in
    (match (method_, id) with
    | Some "initialize", Some id -> (
        response id (initialized_result ());
        match !crash_marker with
        | Some path when not (Sys.file_exists path) ->
            let output = open_out_bin path in
            close_out output;
            exit 0
        | Some _ | None -> ())
    | Some "initialized", None ->
        initialized := true;
        if !malformed then (
          write_all Unix.stdout "Content-Length: 3\r\n\r\n{{{";
          flush stdout)
    | Some "textDocument/didOpen", None ->
        let document =
          field "textDocument" params |> Option.value ~default:(`Assoc [])
        in
        uri := Option.value ~default:!uri (string_field "uri" document);
        contents := Option.value ~default:"" (string_field "text" document);
        version := Option.value ~default:1 (int_field "version" document);
        diagnostic ~uri:!uri ~version:!version !contents;
        if !apply_edit then
          request 99 "workspace/applyEdit"
            (`Assoc
               [
                 ( "edit",
                   `Assoc
                     [
                       ( "changes",
                         `Assoc
                           [
                             ( !uri,
                               `List
                                 [
                                   `Assoc
                                     [
                                       ("range", range 0 0 0 0);
                                       ("newText", `String "X");
                                     ];
                                 ] );
                           ] );
                     ] );
               ])
    | Some "textDocument/didChange", None -> (
        let document =
          field "textDocument" params |> Option.value ~default:(`Assoc [])
        in
        version :=
          Option.value ~default:(!version + 1) (int_field "version" document);
        let changes =
          field "contentChanges" params |> Option.value ~default:(`List [])
        in
        (match changes with
        | `List changes ->
            contents := List.fold_left apply_change !contents changes
        | _ -> ());
        diagnostic ~uri:!uri ~version:!version !contents;
        match !apply_edit_path with
        | Some path
          when (not !workspace_apply_edit_sent)
               && not (String.equal !uri ("file://" ^ path)) ->
            workspace_apply_edit_sent := true;
            request 100 "workspace/applyEdit"
              (`Assoc
                 [
                   ( "edit",
                     `Assoc
                       [
                         ( "changes",
                           `Assoc
                             [
                               ( !uri,
                                 `List
                                   [
                                     `Assoc
                                       [
                                         ("range", range 0 0 0 0);
                                         ("newText", `String "X");
                                       ];
                                   ] );
                               ( "file://" ^ path,
                                 `List
                                   [
                                     `Assoc
                                       [
                                         ("range", range 0 0 0 0);
                                         ("newText", `String "Y");
                                       ];
                                   ] );
                             ] );
                       ] );
                 ])
        | None | Some _ -> ())
    | Some "textDocument/didSave", None ->
        notification "window/logMessage"
          (`Assoc [ ("type", `Int 3); ("message", `String "fake saved") ])
    | Some "textDocument/hover", Some id ->
        if !delay_hover then ignore (Unix.select [] [] [] 0.15);
        let position = request_position params in
        response id
          (`Assoc
             [
               ( "contents",
                 `Assoc
                   [
                     ("kind", `String "markdown");
                     ("value", `String ("fake hover " ^ !contents));
                   ] );
               ("range", `Assoc [ ("start", position); ("end", position) ]);
             ])
    | Some "textDocument/definition", Some id ->
        let definition_uri =
          match !definition_path with
          | None -> !uri
          | Some path -> "file://" ^ path
        in
        response id
          (`List
             [
               `Assoc
                 [ ("uri", `String definition_uri); ("range", range 0 0 0 1) ];
             ])
    | Some "textDocument/completion", Some id ->
        let point = request_position params in
        response id
          (`Assoc
             [
               ("isIncomplete", `Bool false);
               ( "items",
                 `List
                   [
                     `Assoc
                       [
                         ("label", `String "fakeCompletion");
                         ("detail", `String "deterministic fake item");
                         ( "textEdit",
                           `Assoc
                             [
                               ( "range",
                                 `Assoc [ ("start", point); ("end", point) ] );
                               ("newText", `String "fakeCompletion");
                             ] );
                         ( "additionalTextEdits",
                           `List
                             [
                               `Assoc
                                 [
                                   ("range", range 0 3 0 3);
                                   ("newText", `String "(* fake *)\n");
                                 ];
                             ] );
                       ];
                   ] );
             ])
    | Some "textDocument/codeAction", Some id ->
        if !delay_code_action then ignore (Unix.select [] [] [] 0.15);
        if !code_action_error then response_error id "fake code action failure"
        else if !code_action_command then
          response id
            (`List
               [
                 `Assoc
                   [
                     ("title", `String "run denied fake command");
                     ( "command",
                       `Assoc
                         [
                           ("title", `String "fake command");
                           ("command", `String "fake.execute");
                         ] );
                   ];
               ])
        else
          let current_changes =
            [
              ( !uri,
                `List
                  [
                    `Assoc
                      [
                        ("range", range 0 0 0 1); ("newText", `String "action");
                      ];
                  ] );
            ]
          in
          let extra_changes =
            match !code_action_path with
            | None -> []
            | Some path ->
                [
                  ( "file://" ^ path,
                    `List
                      [
                        `Assoc
                          [
                            ("range", range 0 0 0 1);
                            ("newText", `String "action-target");
                          ];
                      ] );
                ]
          in
          let conflicting_changes =
            match !code_action_conflict_path with
            | None -> []
            | Some path ->
                [
                  ( "file://" ^ path,
                    `List
                      [
                        `Assoc
                          [
                            ("range", range 0 0 0 1);
                            ("newText", `String "action-target");
                          ];
                        `Assoc
                          [
                            ("range", range 0 0 0 1);
                            ("newText", `String "conflict");
                          ];
                      ] );
                ]
          in
          let edit =
            if !code_action_resource then
              `Assoc
                [
                  ( "documentChanges",
                    `List
                      [
                        `Assoc
                          [
                            ("kind", `String "rename");
                            ("oldUri", `String !uri);
                            ("newUri", `String (!uri ^ ".renamed"));
                          ];
                      ] );
                ]
            else
              `Assoc
                [
                  ( "changes",
                    `Assoc
                      (current_changes @ extra_changes @ conflicting_changes) );
                ]
          in
          response id
            (`List
               [
                 `Assoc
                   [
                     ("title", `String "apply fake code action"); ("edit", edit);
                   ];
               ])
    | ( (Some "textDocument/formatting" | Some "textDocument/rangeFormatting"),
        Some id ) ->
        if !delay_formatting then ignore (Unix.select [] [] [] 0.15);
        if !formatting_error then response_error id "fake formatting failure"
        else if not (has_formatting_options params) then
          response_error id "unexpected formatting options"
        else if !formatting_malformed then
          response id (`Assoc [ ("edits", `List []) ])
        else if !formatting_noop then response id `Null
        else
          let range_formatting =
            method_ = Some "textDocument/rangeFormatting"
          in
          let range =
            match (range_formatting, field "range" params) with
            | true, Some range -> range
            | _ -> range 0 0 0 1
          in
          let edits =
            [
              `Assoc
                [
                  ("range", range);
                  ( "newText",
                    `String
                      (if range_formatting then "range-formatted"
                       else "formatted") );
                ];
            ]
          in
          let edits =
            if !formatting_conflict then
              edits
              @ [ `Assoc [ ("range", range); ("newText", `String "conflict") ] ]
            else edits
          in
          response id (`List edits)
    | Some "textDocument/rename", Some id ->
        if !delay_rename then ignore (Unix.select [] [] [] 0.15);
        let new_name =
          Option.value ~default:"renamed" (string_field "newName" params)
        in
        let current_changes =
          [
            ( !uri,
              `List
                [
                  `Assoc
                    [ ("range", range 0 0 0 1); ("newText", `String new_name) ];
                  `Assoc
                    [ ("range", range 0 2 0 3); ("newText", `String new_name) ];
                ] );
          ]
        in
        let extra_changes =
          match !rename_path with
          | None -> []
          | Some path ->
              [
                ( "file://" ^ path,
                  `List
                    [
                      `Assoc
                        [
                          ("range", range 0 0 0 1); ("newText", `String new_name);
                        ];
                    ] );
              ]
        in
        let conflicting_changes =
          match !rename_conflict_path with
          | None -> []
          | Some path ->
              [
                ( "file://" ^ path,
                  `List
                    [
                      `Assoc
                        [
                          ("range", range 0 0 0 1); ("newText", `String new_name);
                        ];
                      `Assoc
                        [
                          ("range", range 0 0 0 1);
                          ("newText", `String "conflict");
                        ];
                    ] );
              ]
        in
        let changes = current_changes @ extra_changes @ conflicting_changes in
        response id (`Assoc [ ("changes", `Assoc changes) ])
    | Some "shutdown", Some id -> response id `Null
    | Some "exit", None -> exit 0
    | Some "$/cancelRequest", None -> ()
    | _, Some id ->
        response id (`Assoc [ ("error", `String "unexpected request") ])
    | _ -> ());
    if !initialized then loop () else loop ()
  in
  try loop () with End_of_file -> ()
