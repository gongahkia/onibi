module Data = struct
  type t =
    | Null
    | Bool of bool
    | Number of float
    | Text of string
    | List of t list
    | Object of (string * t) list
end

module Server_config = struct
  type t = {
    id : string;
    language_ids : string list;
    extensions : string list;
    executable : string;
    argv : string list;
    environment : (string * string) list;
    root_markers : string list;
    initialization_options : Data.t option;
    settings : Data.t option;
  }

  let nonempty field value =
    if String.length value = 0 then Error (field ^ " must not be empty")
    else Ok value

  let unique field values =
    if List.length values = List.length (List.sort_uniq String.compare values) then
      Ok values
    else Error (field ^ " contains duplicates")

  let create ~id ~language_ids ~extensions ~executable ?(argv = [])
      ?(environment = []) ?(root_markers = []) ?initialization_options
      ?settings () =
    Result.bind (nonempty "server id" id) (fun id ->
        Result.bind (nonempty "server executable" executable) (fun executable ->
            Result.bind (unique "language ids" language_ids) (fun language_ids ->
                Result.bind (unique "extensions" extensions) (fun extensions ->
                    if language_ids = [] then
                      Error "a server needs at least one language id"
                    else
                      Ok
                        {
                          id;
                          language_ids;
                          extensions;
                          executable;
                          argv;
                          environment;
                          root_markers;
                          initialization_options;
                          settings;
                        }))))

  let id value = value.id
  let language_ids value = value.language_ids
  let extensions value = value.extensions
  let executable value = value.executable
  let argv value = value.argv
  let environment value = value.environment
  let root_markers value = value.root_markers
  let initialization_options value = value.initialization_options
  let settings value = value.settings
end

module Registry = struct
  type t = Server_config.t list

  let empty = []

  let register registry server =
    if List.exists (fun known -> String.equal (Server_config.id known) (Server_config.id server)) registry then
      Error ("duplicate language server " ^ Server_config.id server)
    else Ok (List.sort (fun left right -> String.compare (Server_config.id left) (Server_config.id right)) (server :: registry))

  let all registry = registry

  let has_extension server path =
    let extension = Filename.extension path in
    List.exists (String.equal extension) (Server_config.extensions server)

  let has_language server language =
    List.exists (String.equal language) (Server_config.language_ids server)

  let find_for_path registry ~language_id path =
    match language_id with
    | Some language -> List.find_opt (fun server -> has_language server language) registry
    | None -> List.find_opt (fun server -> has_extension server path) registry

  let default () =
    let ocaml =
      Server_config.create ~id:"ocaml.ocamllsp" ~language_ids:[ "ocaml" ]
        ~extensions:[ ".ml"; ".mli" ] ~executable:"ocamllsp"
        ~root_markers:[ "dune-project"; "dune-workspace"; ".git" ] ()
      |> Result.get_ok
    in
    register empty ocaml |> Result.get_ok
end

module Position = struct
  type encoding = Utf8 | Utf16 | Utf32
  type t = { line : int; character : int }
  type range = { start_ : t; end_ : t }

  let encoding_name = function Utf8 -> "utf-8" | Utf16 -> "utf-16" | Utf32 -> "utf-32"

  let of_name = function
    | "utf-8" | "utf8" -> Some Utf8
    | "utf-16" | "utf16" -> Some Utf16
    | "utf-32" | "utf32" -> Some Utf32
    | _ -> None

  let byte text offset = Char.code (String.get text offset)

  let continuation value = value land 0xc0 = 0x80

  let decode text offset =
    let length = String.length text in
    if offset >= length then Error "unexpected end of UTF-8 input"
    else
      let first = byte text offset in
      let require count =
        if offset + count > length then Error "truncated UTF-8 sequence" else Ok ()
      in
      if first < 0x80 then Ok (first, 1)
      else if first land 0xe0 = 0xc0 then
        Result.bind (require 2) (fun () ->
            let second = byte text (offset + 1) in
            if first < 0xc2 || not (continuation second) then Error "invalid UTF-8 sequence"
            else Ok ((((first land 0x1f) lsl 6) lor (second land 0x3f)), 2))
      else if first land 0xf0 = 0xe0 then
        Result.bind (require 3) (fun () ->
            let second = byte text (offset + 1) in
            let third = byte text (offset + 2) in
            if not (continuation second && continuation third)
               || (first = 0xe0 && second < 0xa0)
               || (first = 0xed && second >= 0xa0)
            then Error "invalid UTF-8 sequence"
            else
              Ok
                ((((first land 0x0f) lsl 12) lor ((second land 0x3f) lsl 6)
                 lor (third land 0x3f), 3)))
      else if first land 0xf8 = 0xf0 then
        Result.bind (require 4) (fun () ->
            let second = byte text (offset + 1) in
            let third = byte text (offset + 2) in
            let fourth = byte text (offset + 3) in
            if first > 0xf4
               || not (continuation second && continuation third && continuation fourth)
               || (first = 0xf0 && second < 0x90)
               || (first = 0xf4 && second >= 0x90)
            then Error "invalid UTF-8 sequence"
            else
              Ok
                ((((first land 0x07) lsl 18) lor ((second land 0x3f) lsl 12)
                 lor ((third land 0x3f) lsl 6) lor (fourth land 0x3f), 4)))
      else Error "invalid UTF-8 leading byte"

  let units encoding code_point width =
    match encoding with
    | Utf8 -> width
    | Utf16 -> if code_point > 0xffff then 2 else 1
    | Utf32 -> 1

  let offset_to_position ~contents ~encoding ~byte_offset =
    if byte_offset < 0 || byte_offset > String.length contents then
      Error "byte offset is outside the document"
    else
      let rec loop offset line character =
        if offset = byte_offset then Ok { line; character }
        else if offset >= String.length contents then Error "byte offset is not a UTF-8 boundary"
        else
          Result.bind (decode contents offset) (fun (code_point, width) ->
              if code_point = Char.code '\r' && offset + width < String.length contents
                 && String.get contents (offset + width) = '\n'
              then
                if byte_offset = offset + width then
                  Error "byte offset splits a CRLF line ending"
                else loop (offset + width + 1) (line + 1) 0
              else if code_point = Char.code '\n' then loop (offset + width) (line + 1) 0
              else loop (offset + width) line (character + units encoding code_point width))
      in
      loop 0 0 0

  let position_to_offset ~contents ~encoding { line; character } =
    if line < 0 || character < 0 then Error "line and character must be non-negative"
    else
      let rec loop offset current_line current_character =
        if current_line = line && current_character = character then Ok offset
        else if offset >= String.length contents then
          if current_line = line && current_character = character then Ok offset
          else Error "position is outside the document"
        else
          Result.bind (decode contents offset) (fun (code_point, width) ->
              if code_point = Char.code '\r' && offset + width < String.length contents
                 && String.get contents (offset + width) = '\n'
              then
                if current_line = line then Error "position is outside the line"
                else loop (offset + width + 1) (current_line + 1) 0
              else if code_point = Char.code '\n' then
                if current_line = line then Error "position is outside the line"
                else loop (offset + width) (current_line + 1) 0
              else if current_line = line && current_character + units encoding code_point width > character then
                Error "position splits an encoded character"
              else loop (offset + width) current_line (current_character + units encoding code_point width))
      in
      loop 0 0 0

  let offsets_to_range ~contents ~encoding ~start_offset ~stop_offset =
    if start_offset > stop_offset then Error "range start is after range end"
    else
      Result.bind (offset_to_position ~contents ~encoding ~byte_offset:start_offset) (fun start_ ->
          Result.map
            (fun end_ -> { start_; end_ })
            (offset_to_position ~contents ~encoding ~byte_offset:stop_offset))

  let range_to_offsets ~contents ~encoding { start_; end_ } =
    Result.bind (position_to_offset ~contents ~encoding start_) (fun start_offset ->
        Result.bind (position_to_offset ~contents ~encoding end_) (fun stop_offset ->
            if start_offset > stop_offset then Error "range start is after range end"
            else Ok (start_offset, stop_offset)))
end

module Uri = struct
  let is_unreserved = function
    | 'A' .. 'Z' | 'a' .. 'z' | '0' .. '9' | '-' | '.' | '_' | '~' | '/' -> true
    | _ -> false

  let hex = "0123456789ABCDEF"

  let percent_encode value =
    let buffer = Buffer.create (String.length value) in
    String.iter
      (fun character ->
        if is_unreserved character then Buffer.add_char buffer character
        else
          let code = Char.code character in
          Buffer.add_char buffer '%';
          Buffer.add_char buffer hex.[code lsr 4];
          Buffer.add_char buffer hex.[code land 0x0f])
      value;
    Buffer.contents buffer

  let absolute path =
    if Filename.is_relative path then Filename.concat (Sys.getcwd ()) path else path

  let file_of_path path = "file://" ^ percent_encode (absolute path)

  let digit = function
    | '0' .. '9' as value -> Char.code value - Char.code '0'
    | 'a' .. 'f' as value -> Char.code value - Char.code 'a' + 10
    | 'A' .. 'F' as value -> Char.code value - Char.code 'A' + 10
    | _ -> -1

  let percent_decode value =
    let buffer = Buffer.create (String.length value) in
    let rec loop index =
      if index = String.length value then Ok (Buffer.contents buffer)
      else if value.[index] <> '%' then (
        Buffer.add_char buffer value.[index];
        loop (index + 1))
      else if index + 2 >= String.length value then Error "truncated percent escape"
      else
        let high = digit value.[index + 1] in
        let low = digit value.[index + 2] in
        if high < 0 || low < 0 then Error "invalid percent escape"
        else (
          Buffer.add_char buffer (Char.chr ((high lsl 4) lor low));
          loop (index + 3))
    in
    loop 0

  let path_of_file uri =
    let prefix = "file://" in
    if not (String.starts_with ~prefix uri) then Error "URI is not a file URI"
    else
      let encoded = String.sub uri (String.length prefix) (String.length uri - String.length prefix) in
      let encoded =
        if String.starts_with ~prefix:"localhost/" encoded then
          String.sub encoded 9 (String.length encoded - 9)
        else encoded
      in
      Result.bind (percent_decode encoded) (fun path ->
          if String.length path = 0 || path.[0] <> '/' then Error "file URI path is not absolute"
          else
            match Position.offset_to_position ~contents:path ~encoding:Position.Utf8 ~byte_offset:(String.length path) with
            | Ok _ -> Ok path
            | Error error -> Error ("file URI path is not valid UTF-8: " ^ error))
end

module Workspace = struct
  let has_marker directory marker = Sys.file_exists (Filename.concat directory marker)

  let discover_root ~markers ~file_path =
    let rec search directory =
      if List.exists (has_marker directory) markers then directory
      else
        let parent = Filename.dirname directory in
        if String.equal parent directory then directory else search parent
    in
    search (Filename.dirname (Uri.absolute file_path))
end

type diagnostic_severity = Error | Warning | Information | Hint

type diagnostic = {
  start_offset : int;
  stop_offset : int;
  severity : diagnostic_severity;
  message : string;
  source : string option;
  code : string option;
  document_id : string;
  document_version : int;
}

type hover = { text : string; start_offset : int option; stop_offset : int option }

type definition_target = { uri : string; start_offset : int; stop_offset : int }

type text_edit = { start_offset : int; stop_offset : int; replacement : string }

type completion = {
  label : string;
  detail : string option;
  documentation : string option;
  sort_text : string option;
  filter_text : string option;
  insert_text : string option;
  text_edit : text_edit option;
  additional_text_edits : text_edit list;
  snippet : bool;
  deprecated : bool;
}

type server_state = Stopped | Starting | Initializing | Ready | Failed | Shutting_down

type status = {
  language_id : string option;
  server_id : string option;
  executable : string option;
  workspace_root : string option;
  state : server_state;
  position_encoding : Position.encoding option;
  sync_kind : [ `None | `Full | `Incremental ] option;
  pending_requests : int;
  diagnostic_count : int;
  last_error : string option;
}

let diagnostic_severity_name = function
  | Error -> "error"
  | Warning -> "warning"
  | Information -> "information"
  | Hint -> "hint"

let server_state_name = function
  | Stopped -> "stopped"
  | Starting -> "starting"
  | Initializing -> "initializing"
  | Ready -> "ready"
  | Failed -> "failed"
  | Shutting_down -> "shutting-down"
