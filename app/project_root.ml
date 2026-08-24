type t = { path : string }
type entry = { relative_path : string }

let maximum_depth = 32
let maximum_entries = 4096
let binary_probe_bytes = 8192

let error action path message =
  Error (Printf.sprintf "project root %s %S: %s" action path message)

let path root = root.path

let select requested =
  try
    let path = Unix.realpath requested in
    let stats = Unix.stat path in
    if stats.Unix.st_kind <> Unix.S_DIR then
      error "selection rejected" requested "not a directory"
    else (
      Unix.access path [ Unix.R_OK; Unix.X_OK ];
      Ok { path })
  with
  | Unix.Unix_error (reason, _, _) ->
      error "selection rejected" requested (Unix.error_message reason)
  | Sys_error message -> error "selection rejected" requested message
  | exception_ ->
      error "selection rejected" requested (Printexc.to_string exception_)

let hidden name = String.length name > 0 && name.[0] = '.'

let readable_regular path =
  try
    let stats = Unix.lstat path in
    if stats.Unix.st_kind <> Unix.S_REG then false
    else if stats.Unix.st_perm land 0o444 = 0 then false
    else (
      Unix.access path [ Unix.R_OK ];
      true)
  with Unix.Unix_error _ | Sys_error _ -> false

let binary path =
  try
    let channel = open_in_bin path in
    Fun.protect
      ~finally:(fun () -> close_in_noerr channel)
      (fun () ->
        let bytes = Bytes.create binary_probe_bytes in
        let count = input channel bytes 0 binary_probe_bytes in
        let rec contains_nul offset =
          offset < count
          && (Bytes.get bytes offset = '\000' || contains_nul (offset + 1))
        in
        contains_nul 0)
  with Sys_error _ | Unix.Unix_error _ -> true

let relative_path parent name =
  if String.length parent = 0 then name else parent ^ "/" ^ name

exception Candidate_limit

let discover root =
  let count = ref 0 in
  let rec scan ~depth relative =
    if depth > maximum_depth then []
    else
      let directory = Filename.concat root.path relative in
      let names =
        try Sys.readdir directory |> Array.to_list |> List.sort String.compare
        with Sys_error _ | Unix.Unix_error _ -> []
      in
      List.concat_map
        (fun name ->
          if hidden name then []
          else
            let relative_path = relative_path relative name in
            let path = Filename.concat root.path relative_path in
            try
              match (Unix.lstat path).Unix.st_kind with
              | Unix.S_DIR -> scan ~depth:(depth + 1) relative_path
              | Unix.S_REG when readable_regular path && not (binary path) ->
                  if !count = maximum_entries then raise Candidate_limit
                  else (
                    incr count;
                    [ { relative_path } ])
              | Unix.S_REG | Unix.S_LNK | Unix.S_CHR | Unix.S_BLK | Unix.S_FIFO
              | Unix.S_SOCK ->
                  []
            with Unix.Unix_error _ | Sys_error _ -> [])
        names
  in
  try Ok (scan ~depth:0 "")
  with Candidate_limit ->
    error "discovery rejected" root.path
      (Printf.sprintf "more than %d readable text files" maximum_entries)

let contains_casefold ~needle text =
  let needle = String.lowercase_ascii needle in
  let text = String.lowercase_ascii text in
  let length = String.length needle in
  let rec loop index =
    if length = 0 then true
    else if index + length > String.length text then false
    else if String.sub text index length = needle then true
    else loop (index + 1)
  in
  loop 0

let filter root ~query =
  discover root
  |> Result.map
       (List.filter (fun entry ->
            contains_casefold ~needle:query entry.relative_path))

let relative_path_is_safe relative_path =
  Filename.is_relative relative_path
  && String.length relative_path > 0
  && String.split_on_char '/' relative_path
     |> List.for_all (fun component ->
         String.length component > 0
         && (not (String.equal component "."))
         && (not (String.equal component ".."))
         && not (hidden component))

let inside root path =
  if String.equal root.path Filename.dir_sep then
    String.starts_with ~prefix:Filename.dir_sep path
  else
    String.equal root.path path
    ||
    let prefix = root.path ^ Filename.dir_sep in
    String.length path > String.length prefix && String.starts_with ~prefix path

let contains root ~path = inside root path

let resolve root ~relative_path =
  if not (relative_path_is_safe relative_path) then
    error "path rejected" relative_path
      "expected a non-empty relative path without traversal components"
  else
    let candidate = Filename.concat root.path relative_path in
    try
      if not (readable_regular candidate) then
        error "path rejected" relative_path "not a readable regular file"
      else if binary candidate then
        error "path rejected" relative_path
          "contains a NUL byte in its first probe"
      else
        let canonical = Unix.realpath candidate in
        if not (inside root canonical) then
          error "path rejected" relative_path
            "resolves outside the selected root"
        else Ok canonical
    with
    | Unix.Unix_error (reason, _, _) ->
        error "path rejected" relative_path (Unix.error_message reason)
    | Sys_error message -> error "path rejected" relative_path message
    | exception_ ->
        error "path rejected" relative_path (Printexc.to_string exception_)
