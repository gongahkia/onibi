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
    cwd : string option;
    environment : (string * string) list;
    root_markers : string list;
    workspace_folders : string list;
    initialization_options : Data.t option;
    settings : Data.t option;
  }

  let nonempty field value =
    if String.length value = 0 then Error (field ^ " must not be empty")
    else Ok value

  let unique field values =
    if List.length values = List.length (List.sort_uniq String.compare values)
    then Ok values
    else Error (field ^ " contains duplicates")

  let create ~id ~language_ids ~extensions ~executable ?(argv = []) ?cwd
      ?(environment = []) ?(root_markers = []) ?(workspace_folders = [])
      ?initialization_options ?settings () =
    Result.bind (nonempty "server id" id) (fun id ->
        Result.bind (nonempty "server executable" executable) (fun executable ->
            Result.bind (unique "language ids" language_ids)
              (fun language_ids ->
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
                          cwd;
                          environment;
                          root_markers;
                          workspace_folders;
                          initialization_options;
                          settings;
                        }))))

  let id value = value.id
  let language_ids value = value.language_ids
  let extensions value = value.extensions
  let executable value = value.executable
  let argv value = value.argv
  let cwd value = value.cwd
  let environment value = value.environment
  let root_markers value = value.root_markers
  let workspace_folders value = value.workspace_folders
  let initialization_options value = value.initialization_options
  let settings value = value.settings
end

module Registry = struct
  type t = Server_config.t list

  let empty = []

  let register registry server =
    if
      List.exists
        (fun known ->
          String.equal (Server_config.id known) (Server_config.id server))
        registry
    then Error ("duplicate language server " ^ Server_config.id server)
    else
      Ok
        (List.sort
           (fun left right ->
             String.compare (Server_config.id left) (Server_config.id right))
           (server :: registry))

  let all registry = registry

  let has_extension server path =
    let extension = Filename.extension path in
    List.exists (String.equal extension) (Server_config.extensions server)

  let has_language server language =
    List.exists (String.equal language) (Server_config.language_ids server)

  let find_for_path registry ~language_id path =
    match language_id with
    | Some language ->
        List.find_opt (fun server -> has_language server language) registry
    | None -> List.find_opt (fun server -> has_extension server path) registry

  let default () =
    let ocaml =
      Server_config.create ~id:"ocaml.ocamllsp" ~language_ids:[ "ocaml" ]
        ~extensions:[ ".ml"; ".mli" ] ~executable:"ocamllsp"
        ~root_markers:[ "dune-project"; "dune-workspace"; ".git" ]
        ()
      |> Result.get_ok
    in
    register empty ocaml |> Result.get_ok
end

module Config = struct
  type t = Default | Explicit of string | Disabled

  type loaded = {
    registry : Registry.t;
    source : string;
    user_servers : Server_config.t list;
  }

  let maximum_file_bytes = 128 * 1024
  let maximum_servers = 32
  let maximum_arguments = 64
  let maximum_environment = 32
  let maximum_root_markers = 16
  let maximum_workspace_folders = 16

  let default_path () =
    let root =
      match Sys.getenv_opt "XDG_CONFIG_HOME" with
      | Some path when String.length path > 0 -> path
      | None | Some _ -> (
          match Sys.getenv_opt "HOME" with
          | Some path when String.length path > 0 ->
              Filename.concat path ".config"
          | None | Some _ -> ".config")
    in
    Filename.concat root "zenbu/language-servers.toml"

  let table = function
    | Otoml.TomlTable values | Otoml.TomlInlineTable values -> Some values
    | _ -> None

  let field fields name = List.assoc_opt name fields
  let error path message = Error ("language config " ^ path ^ ": " ^ message)
  let ( let* ) = Result.bind

  let has_control_or_nul value =
    String.exists
      (fun character ->
        let code = Char.code character in
        code = 0 || code < 32 || code = 127)
      value

  let nonempty_text path value =
    if String.length value = 0 then error path "must not be empty"
    else if has_control_or_nul value then
      error path "must not contain control bytes"
    else Ok value

  let unique path values =
    if List.length values = List.length (List.sort_uniq String.compare values)
    then Ok values
    else error path "contains duplicates"

  let required_text fields path name =
    match field fields name with
    | Some (Otoml.TomlString value) -> nonempty_text path value
    | Some _ -> error path "must be a nonempty string"
    | None -> error path "is required"

  let optional_text fields path name =
    match field fields name with
    | None -> Ok None
    | Some (Otoml.TomlString value) ->
        Result.map Option.some (nonempty_text path value)
    | Some _ -> error path "must be a nonempty string"

  let strings fields path name ~required ~limit =
    match field fields name with
    | None when required -> error path "is required"
    | None -> Ok []
    | Some (Otoml.TomlArray values) ->
        if List.length values > limit then
          error path ("exceeds the " ^ string_of_int limit ^ " item limit")
        else
          let rec collect result = function
            | [] -> Ok (List.rev result)
            | Otoml.TomlString value :: rest ->
                let* value = nonempty_text path value in
                collect (value :: result) rest
            | _ -> error path "must be an array of nonempty strings"
          in
          let* values = collect [] values in
          unique path values
    | Some _ -> error path "must be an array of nonempty strings"

  let absolute_directory path value =
    if Filename.is_relative value then error path "must be an absolute path"
    else
      try
        let value = Unix.realpath value in
        if Sys.is_directory value then Ok value
        else error path "must name an existing directory"
      with Unix.Unix_error _ | Sys_error _ ->
        error path "must name an accessible existing directory"

  let trusted_ancestor_directories path directory =
    let rec validate directory =
      try
        let stat = Unix.stat directory in
        if stat.Unix.st_kind <> Unix.S_DIR then
          error path "has a non-directory ancestor"
        else if stat.Unix.st_perm land 0o022 <> 0 then
          error path "has a group- or world-writable ancestor directory"
        else
          let parent = Filename.dirname directory in
          if String.equal parent directory then Ok () else validate parent
      with Unix.Unix_error _ | Sys_error _ ->
        error path "has an inaccessible ancestor directory"
    in
    validate directory

  let trusted_executable path value =
    if Filename.is_relative value then error path "must be an absolute path"
    else
      try
        let value = Unix.realpath value in
        let stat = Unix.stat value in
        if stat.Unix.st_kind <> Unix.S_REG then
          error path "must name a regular file"
        else if stat.Unix.st_perm land 0o022 <> 0 then
          error path "must not be group- or world-writable"
        else
          let* () =
            trusted_ancestor_directories path (Filename.dirname value)
          in
          Unix.access value [ Unix.X_OK ];
          Ok value
      with Unix.Unix_error _ | Sys_error _ ->
        error path "must name an executable regular file"

  let valid_environment_name value =
    String.length value > 0
    &&
    let first = value.[0] in
    (match first with 'A' .. 'Z' | '_' -> true | _ -> false)
    &&
    let rec rest index =
      if index = String.length value then true
      else
        match value.[index] with
        | 'A' .. 'Z' | '0' .. '9' | '_' -> rest (index + 1)
        | _ -> false
    in
    rest 1

  let denied_environment_name value =
    String.equal value "PATH"
    || String.equal value "LD_PRELOAD"
    || String.equal value "LD_LIBRARY_PATH"
    || String.starts_with ~prefix:"DYLD_" value

  let environment fields path =
    match field fields "environment" with
    | None -> Ok []
    | Some value -> (
        match table value with
        | None -> error path "must be an inline table of string values"
        | Some values ->
            if List.length values > maximum_environment then
              error path
                ("exceeds the "
                ^ string_of_int maximum_environment
                ^ " item limit")
            else
              let rec collect result = function
                | [] -> Ok (List.rev result)
                | (name, Otoml.TomlString value) :: rest ->
                    if not (valid_environment_name name) then
                      error path "contains an invalid environment variable name"
                    else if denied_environment_name name then
                      error path ("may not override protected variable " ^ name)
                    else if
                      String.length value > 4_096 || has_control_or_nul value
                    then error path "contains an invalid environment value"
                    else collect ((name, value) :: result) rest
                | _ -> error path "must be an inline table of string values"
              in
              collect [] values)

  let root_markers fields path =
    let* values =
      strings fields path "root_markers" ~required:false
        ~limit:maximum_root_markers
    in
    match
      List.find_opt
        (fun value ->
          String.equal value "." || String.equal value ".."
          || not (String.equal (Filename.basename value) value))
        values
    with
    | None -> Ok values
    | Some _ -> error path "must contain basename markers only"

  let workspace_folders fields path =
    let* values =
      strings fields path "workspace_folders" ~required:false
        ~limit:maximum_workspace_folders
    in
    let rec resolve result = function
      | [] -> unique path (List.rev result)
      | value :: rest ->
          let* value = absolute_directory path value in
          resolve (value :: result) rest
    in
    resolve [] values

  let validate_server_fields path fields =
    let allowed =
      [
        "id";
        "language_ids";
        "extensions";
        "executable";
        "args";
        "cwd";
        "environment";
        "root_markers";
        "workspace_folders";
      ]
    in
    match
      List.find_opt (fun (name, _) -> not (List.mem name allowed)) fields
    with
    | None -> Ok ()
    | Some (name, _) -> error path ("contains unknown field " ^ name)

  let parse_server index value =
    let path = "server[" ^ string_of_int index ^ "]" in
    let* fields =
      match table value with
      | Some fields -> Ok fields
      | None -> error path "must be a TOML table"
    in
    let* () = validate_server_fields path fields in
    let* id = required_text fields (path ^ ".id") "id" in
    let* language_ids =
      strings fields (path ^ ".language_ids") "language_ids" ~required:true
        ~limit:16
    in
    let* extensions =
      strings fields (path ^ ".extensions") "extensions" ~required:true
        ~limit:32
    in
    let* () =
      match
        List.find_opt
          (fun value -> not (String.starts_with ~prefix:"." value))
          extensions
      with
      | None -> Ok ()
      | Some _ -> error (path ^ ".extensions") "must start with a dot"
    in
    let* executable =
      required_text fields (path ^ ".executable") "executable"
    in
    let* executable = trusted_executable (path ^ ".executable") executable in
    let* argv =
      strings fields (path ^ ".args") "args" ~required:false
        ~limit:maximum_arguments
    in
    let* cwd = optional_text fields (path ^ ".cwd") "cwd" in
    let* cwd =
      match cwd with
      | None -> Ok None
      | Some cwd ->
          Result.map Option.some (absolute_directory (path ^ ".cwd") cwd)
    in
    let* environment = environment fields (path ^ ".environment") in
    let* root_markers = root_markers fields (path ^ ".root_markers") in
    let* workspace_folders =
      workspace_folders fields (path ^ ".workspace_folders")
    in
    Server_config.create ~id ~language_ids ~extensions ~executable ~argv ?cwd
      ~environment ~root_markers ~workspace_folders ()
    |> Result.map_error (fun message ->
        "language config " ^ path ^ ": " ^ message)

  let parse path =
    let* () =
      try
        if (Unix.stat path).Unix.st_size > maximum_file_bytes then
          error path
            ("exceeds the " ^ string_of_int maximum_file_bytes ^ " byte limit")
        else Ok ()
      with Unix.Unix_error _ | Sys_error _ -> error path "cannot be inspected"
    in
    let* document =
      Otoml.Parser.from_file_result path
      |> Result.map_error (fun message ->
          "language config " ^ path ^ ": " ^ message)
    in
    let* root =
      match table document with
      | Some root -> Ok root
      | None -> error path "root must be a TOML table"
    in
    let* () =
      match
        List.find_opt
          (fun (name, _) ->
            not (String.equal name "version" || String.equal name "server"))
          root
      with
      | None -> Ok ()
      | Some (name, _) -> error path ("contains unknown field " ^ name)
    in
    let* () =
      match field root "version" with
      | Some (Otoml.TomlInteger 1) -> Ok ()
      | Some (Otoml.TomlInteger _) -> error path "version must be 1"
      | Some _ -> error path "version must be the integer 1"
      | None -> error path "version is required"
    in
    let* servers =
      match field root "server" with
      | Some (Otoml.TomlTableArray values) ->
          if values = [] then error path "must declare at least one [[server]]"
          else if List.length values > maximum_servers then
            error path
              ("exceeds the " ^ string_of_int maximum_servers ^ " server limit")
          else
            let rec collect index result = function
              | [] -> Ok (List.rev result)
              | value :: rest ->
                  let* server = parse_server index value in
                  collect (index + 1) (server :: result) rest
            in
            collect 0 [] values
      | Some _ -> error path "server must use [[server]] tables"
      | None -> error path "must declare at least one [[server]]"
    in
    List.fold_left
      (fun registry server ->
        Result.bind registry (fun registry -> Registry.register registry server))
      (Ok Registry.empty) servers
    |> Result.map (fun _ -> servers)

  let overlaps left right =
    List.exists
      (fun language -> List.mem language (Server_config.language_ids right))
      (Server_config.language_ids left)
    || List.exists
         (fun extension -> List.mem extension (Server_config.extensions right))
         (Server_config.extensions left)

  let registry_with_user_servers user_servers =
    let inherited =
      Registry.default ()
      |> List.filter (fun builtin ->
          not (List.exists (fun user -> overlaps builtin user) user_servers))
    in
    List.fold_left
      (fun registry server ->
        Result.bind registry (fun registry -> Registry.register registry server))
      (Ok inherited) user_servers

  let loaded ~source user_servers =
    registry_with_user_servers user_servers
    |> Result.map (fun registry -> { registry; source; user_servers })

  let load = function
    | Disabled ->
        loaded ~source:"built-in defaults (user configuration disabled)" []
    | Default ->
        let path = default_path () in
        if Sys.file_exists path then
          Result.bind (parse path) (loaded ~source:path)
        else loaded ~source:"built-in defaults (no user configuration file)" []
    | Explicit path -> Result.bind (parse path) (loaded ~source:path)

  let registry value = value.registry

  let inspect value =
    [
      "Language configuration";
      "source: " ^ value.source;
      "user servers: " ^ string_of_int (List.length value.user_servers);
      "authority: host-loaded declarative configuration only";
      "environment values and arguments: redacted";
    ]
    @ List.map
        (fun server ->
          Printf.sprintf "server: %s (%d environment values; cwd: %s)"
            (Server_config.id server)
            (List.length (Server_config.environment server))
            (Option.value ~default:"inherited" (Server_config.cwd server)))
        value.user_servers
end

module Position = struct
  type encoding = Utf8 | Utf16 | Utf32
  type t = { line : int; character : int }
  type range = { start_ : t; end_ : t }

  let encoding_name = function
    | Utf8 -> "utf-8"
    | Utf16 -> "utf-16"
    | Utf32 -> "utf-32"

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
        if offset + count > length then Error "truncated UTF-8 sequence"
        else Ok ()
      in
      if first < 0x80 then Ok (first, 1)
      else if first land 0xe0 = 0xc0 then
        Result.bind (require 2) (fun () ->
            let second = byte text (offset + 1) in
            if first < 0xc2 || not (continuation second) then
              Error "invalid UTF-8 sequence"
            else Ok (((first land 0x1f) lsl 6) lor (second land 0x3f), 2))
      else if first land 0xf0 = 0xe0 then
        Result.bind (require 3) (fun () ->
            let second = byte text (offset + 1) in
            let third = byte text (offset + 2) in
            if
              (not (continuation second && continuation third))
              || (first = 0xe0 && second < 0xa0)
              || (first = 0xed && second >= 0xa0)
            then Error "invalid UTF-8 sequence"
            else
              Ok
                ( ((first land 0x0f) lsl 12)
                  lor ((second land 0x3f) lsl 6)
                  lor (third land 0x3f),
                  3 ))
      else if first land 0xf8 = 0xf0 then
        Result.bind (require 4) (fun () ->
            let second = byte text (offset + 1) in
            let third = byte text (offset + 2) in
            let fourth = byte text (offset + 3) in
            if
              first > 0xf4
              || (not
                    (continuation second && continuation third
                   && continuation fourth))
              || (first = 0xf0 && second < 0x90)
              || (first = 0xf4 && second >= 0x90)
            then Error "invalid UTF-8 sequence"
            else
              Ok
                ( ((first land 0x07) lsl 18)
                  lor ((second land 0x3f) lsl 12)
                  lor ((third land 0x3f) lsl 6)
                  lor (fourth land 0x3f),
                  4 ))
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
        else if offset >= String.length contents then
          Error "byte offset is not a UTF-8 boundary"
        else
          Result.bind (decode contents offset) (fun (code_point, width) ->
              if
                code_point = Char.code '\r'
                && offset + width < String.length contents
                && String.get contents (offset + width) = '\n'
              then
                if byte_offset = offset + width then
                  Error "byte offset splits a CRLF line ending"
                else loop (offset + width + 1) (line + 1) 0
              else if code_point = Char.code '\n' then
                loop (offset + width) (line + 1) 0
              else
                loop (offset + width) line
                  (character + units encoding code_point width))
      in
      loop 0 0 0

  let position_to_offset ~contents ~encoding { line; character } =
    if line < 0 || character < 0 then
      Error "line and character must be non-negative"
    else
      let rec loop offset current_line current_character =
        if current_line = line && current_character = character then Ok offset
        else if offset >= String.length contents then
          if current_line = line && current_character = character then Ok offset
          else Error "position is outside the document"
        else
          Result.bind (decode contents offset) (fun (code_point, width) ->
              if
                code_point = Char.code '\r'
                && offset + width < String.length contents
                && String.get contents (offset + width) = '\n'
              then
                if current_line = line then Error "position is outside the line"
                else loop (offset + width + 1) (current_line + 1) 0
              else if code_point = Char.code '\n' then
                if current_line = line then Error "position is outside the line"
                else loop (offset + width) (current_line + 1) 0
              else if
                current_line = line
                && current_character + units encoding code_point width
                   > character
              then Error "position splits an encoded character"
              else
                loop (offset + width) current_line
                  (current_character + units encoding code_point width))
      in
      loop 0 0 0

  let offsets_to_range ~contents ~encoding ~start_offset ~stop_offset =
    if start_offset > stop_offset then Error "range start is after range end"
    else
      Result.bind
        (offset_to_position ~contents ~encoding ~byte_offset:start_offset)
        (fun start_ ->
          Result.map
            (fun end_ -> { start_; end_ })
            (offset_to_position ~contents ~encoding ~byte_offset:stop_offset))

  let range_to_offsets ~contents ~encoding { start_; end_ } =
    Result.bind (position_to_offset ~contents ~encoding start_)
      (fun start_offset ->
        Result.bind (position_to_offset ~contents ~encoding end_)
          (fun stop_offset ->
            if start_offset > stop_offset then
              Error "range start is after range end"
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
    if Filename.is_relative path then Filename.concat (Sys.getcwd ()) path
    else path

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
      else if index + 2 >= String.length value then
        Error "truncated percent escape"
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
      let encoded =
        String.sub uri (String.length prefix)
          (String.length uri - String.length prefix)
      in
      let encoded =
        if String.starts_with ~prefix:"localhost/" encoded then
          String.sub encoded 9 (String.length encoded - 9)
        else encoded
      in
      Result.bind (percent_decode encoded) (fun path ->
          if String.length path = 0 || path.[0] <> '/' then
            Error "file URI path is not absolute"
          else
            match
              Position.offset_to_position ~contents:path ~encoding:Position.Utf8
                ~byte_offset:(String.length path)
            with
            | Ok _ -> Ok path
            | Error error -> Error ("file URI path is not valid UTF-8: " ^ error))
end

module Workspace = struct
  let has_marker directory marker =
    Sys.file_exists (Filename.concat directory marker)

  let cache_limit = 128
  let cache = Hashtbl.create cache_limit
  let cache_order = Queue.create ()
  let cache_lock = Mutex.create ()

  let cache_key ~markers ~directory =
    directory ^ "\000" ^ String.concat "\000" markers

  let cached key =
    Mutex.lock cache_lock;
    let value = Hashtbl.find_opt cache key in
    Mutex.unlock cache_lock;
    value

  let remember key root =
    Mutex.lock cache_lock;
    if not (Hashtbl.mem cache key) then (
      (if Hashtbl.length cache >= cache_limit then
         let oldest = Queue.take cache_order in
         Hashtbl.remove cache oldest);
      Queue.add key cache_order);
    Hashtbl.replace cache key root;
    Mutex.unlock cache_lock

  let discover_root ~markers ~file_path =
    let directory = Filename.dirname (Uri.absolute file_path) in
    let key = cache_key ~markers ~directory in
    let rec search directory =
      if List.exists (has_marker directory) markers then directory
      else
        let parent = Filename.dirname directory in
        if String.equal parent directory then directory else search parent
    in
    match cached key with
    | Some root -> root
    | None ->
        let root = search directory in
        remember key root;
        root
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

type hover = {
  text : string;
  start_offset : int option;
  stop_offset : int option;
}

type definition_target = { uri : string; start_offset : int; stop_offset : int }
type text_edit = { start_offset : int; stop_offset : int; replacement : string }

module Sync = struct
  type content_change = { range : Position.range; text : string }

  let apply_one contents ~encoding change =
    Result.bind (Position.range_to_offsets ~contents ~encoding change.range)
      (fun (start_offset, stop_offset) ->
        if
          start_offset > String.length contents
          || stop_offset > String.length contents
        then Error "synchronization range is outside the document"
        else
          Ok
            (String.sub contents 0 start_offset
            ^ change.text
            ^ String.sub contents stop_offset
                (String.length contents - stop_offset)))

  let apply_changes ~contents ~encoding changes =
    List.fold_left
      (fun result change ->
        Result.bind result (fun contents -> apply_one contents ~encoding change))
      (Ok contents) changes

  let incremental_changes ~contents ~encoding ~edits ~expected =
    (* Transactions use coordinates from one source snapshot. Applying their
       edits in descending source order leaves each remaining coordinate valid
       for the intermediate snapshot required by LSP. *)
    let source_order = List.rev edits in
    let rec build current changes = function
      | [] ->
          if String.equal current expected then Ok (List.rev changes)
          else
            Error
              "incremental changes do not reconstruct the committed document"
      | edit :: rest ->
          Result.bind
            (Position.offsets_to_range ~contents:current ~encoding
               ~start_offset:edit.start_offset ~stop_offset:edit.stop_offset)
            (fun range ->
              let change = { range; text = edit.replacement } in
              Result.bind (apply_one current ~encoding change) (fun next ->
                  build next (change :: changes) rest))
    in
    build contents [] source_order
end

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

type server_state =
  | Stopped
  | Starting
  | Initializing
  | Ready
  | Failed
  | Shutting_down

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
