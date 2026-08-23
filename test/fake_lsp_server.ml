(* A deterministic LSP peer used only by M11 tests and headless demos. It is
   deliberately small: the client remains responsible for malformed input,
   request ownership, and result validation. *)

let sync_mode = ref "incremental"
let delay_hover = ref false
let malformed = ref false
let crash_marker = ref None
let apply_edit = ref false
let definition_path = ref None

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
    | Some "textDocument/didChange", None ->
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
        diagnostic ~uri:!uri ~version:!version !contents
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
    | Some "textDocument/rename", Some id ->
        let new_name =
          Option.value ~default:"renamed" (string_field "newName" params)
        in
        response id
          (`Assoc
             [
               ( "changes",
                 `Assoc
                   [
                     ( !uri,
                       `List
                         [
                           `Assoc
                             [
                               ("range", range 0 0 0 1);
                               ("newText", `String new_name);
                             ];
                           `Assoc
                             [
                               ("range", range 0 2 0 3);
                               ("newText", `String new_name);
                             ];
                         ] );
                   ] );
             ])
    | Some "shutdown", Some id -> response id `Null
    | Some "exit", None -> exit 0
    | Some "$/cancelRequest", None -> ()
    | _, Some id ->
        response id (`Assoc [ ("error", `String "unexpected request") ])
    | _ -> ());
    if !initialized then loop () else loop ()
  in
  try loop () with End_of_file -> ()
