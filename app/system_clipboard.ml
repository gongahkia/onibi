open Zenbu_kernel

type t = {
  name : string;
  read : unit -> (string, Error.t) result;
  write : string -> (unit, Error.t) result;
}

let maximum_bytes = 16 * 1024 * 1024
let create ~name ~read ~write = { name; read; write }

let unavailable reason =
  create ~name:"unavailable"
    ~read:(fun () -> Error (Error.System_clipboard_error reason))
    ~write:(fun _ -> Error (Error.System_clipboard_error reason))

let name value = value.name

let validate_contents contents =
  if String.length contents > maximum_bytes then
    Error
      (Error.System_clipboard_error
         (Printf.sprintf "contents exceed the %d-byte limit" maximum_bytes))
  else
    Text_buffer.of_utf8 contents
    |> Result.map_error (fun _ ->
        Error.System_clipboard_error "provider returned invalid UTF-8")
    |> Result.map (fun _ -> contents)

let read value = Result.bind (value.read ()) validate_contents

let write value contents =
  Result.bind (validate_contents contents) (fun _ -> value.write contents)

let process_error operation program = function
  | Unix.WEXITED 0 -> Ok ()
  | Unix.WEXITED status ->
      Error
        (Error.System_clipboard_error
           (Printf.sprintf "%s exited with status %d" program status))
  | Unix.WSIGNALED signal ->
      Error
        (Error.System_clipboard_error
           (Printf.sprintf "%s was terminated by signal %d" program signal))
  | Unix.WSTOPPED signal ->
      Error
        (Error.System_clipboard_error
           (Printf.sprintf "%s stopped with signal %d during %s" program signal
              operation))

let with_unix_error operation callback =
  try callback () with
  | Unix.Unix_error (error, function_name, argument) ->
      let location =
        if String.length function_name = 0 then ""
        else
          let argument_suffix =
            if String.length argument = 0 then "" else "(" ^ argument ^ ")"
          in
          " in " ^ function_name ^ argument_suffix
      in
      Error
        (Error.System_clipboard_error
           (Printf.sprintf "%s failed: %s%s" operation
              (Unix.error_message error) location))
  | Sys_error message ->
      Error (Error.System_clipboard_error (operation ^ ": " ^ message))

let read_all channel =
  let buffer = Buffer.create 1024 in
  let chunk = Bytes.create 4096 in
  let rec loop count =
    let read =
      try input channel chunk 0 (Bytes.length chunk) with End_of_file -> 0
    in
    if read = 0 then Ok (Buffer.contents buffer)
    else if count + read > maximum_bytes then
      Error
        (Error.System_clipboard_error
           (Printf.sprintf "provider output exceeds the %d-byte limit"
              maximum_bytes))
    else (
      Buffer.add_subbytes buffer chunk 0 read;
      loop (count + read))
  in
  loop 0

let command_reader ~program ~arguments () =
  with_unix_error "system clipboard read" (fun () ->
      let channel =
        Unix.open_process_args_in program (Array.of_list (program :: arguments))
      in
      match read_all channel with
      | Error _ as error ->
          ignore (Unix.close_process_in channel);
          error
      | Ok contents ->
          Result.bind
            (process_error "read" program (Unix.close_process_in channel))
            (fun () -> Ok contents))

let command_writer ~program ~arguments contents =
  with_unix_error "system clipboard write" (fun () ->
      let channel =
        Unix.open_process_args_out program
          (Array.of_list (program :: arguments))
      in
      try
        output_string channel contents;
        flush channel;
        process_error "write" program (Unix.close_process_out channel)
      with exception_ ->
        ignore (Unix.close_process_out channel);
        raise exception_)

let executable name =
  let paths =
    Sys.getenv_opt "PATH" |> Option.value ~default:""
    |> String.split_on_char ':'
  in
  let candidates =
    [ "/usr/bin"; "/usr/local/bin"; "/opt/homebrew/bin" ] @ paths
  in
  match
    candidates
    |> List.filter_map (fun directory ->
        if String.length directory = 0 then None
        else
          let path = Filename.concat directory name in
          try
            Unix.access path [ Unix.X_OK ];
            Some path
          with Unix.Unix_error _ -> None)
    |> List.sort_uniq String.compare
  with
  | path :: _ -> Some path
  | [] -> None

let external_provider ~name ~copy_program ~copy_arguments ~paste_program
    ~paste_arguments =
  create ~name
    ~read:(command_reader ~program:paste_program ~arguments:paste_arguments)
    ~write:(command_writer ~program:copy_program ~arguments:copy_arguments)

let default () =
  match (executable "pbcopy", executable "pbpaste") with
  | Some copy_program, Some paste_program ->
      external_provider ~name:"pbcopy/pbpaste" ~copy_program ~copy_arguments:[]
        ~paste_program ~paste_arguments:[]
  | None, _ | _, None -> (
      match (executable "wl-copy", executable "wl-paste") with
      | Some copy_program, Some paste_program ->
          external_provider ~name:"wl-clipboard" ~copy_program
            ~copy_arguments:[] ~paste_program
            ~paste_arguments:[ "--no-newline" ]
      | None, _ | _, None -> (
          match executable "xclip" with
          | Some program ->
              external_provider ~name:"xclip" ~copy_program:program
                ~copy_arguments:[ "-selection"; "clipboard"; "-in" ]
                ~paste_program:program
                ~paste_arguments:[ "-selection"; "clipboard"; "-out" ]
          | None -> (
              match executable "xsel" with
              | Some program ->
                  external_provider ~name:"xsel" ~copy_program:program
                    ~copy_arguments:[ "--clipboard"; "--input" ]
                    ~paste_program:program
                    ~paste_arguments:[ "--clipboard"; "--output" ]
              | None ->
                  unavailable
                    "no supported provider found (pbcopy/pbpaste, \
                     wl-clipboard, xclip, or xsel)")))
