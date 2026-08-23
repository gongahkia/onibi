open Zenbu_kernel
open Zenbu_model_api

let maximum_bytes = 16 * 1024 * 1024
let maximum_stderr_bytes = 4096
let maximum_runtime_seconds = 5.
let error message = Error.External_filter_error message
let valid_argument value = not (String.contains value '\000')

let validate_request (request : Model_effect.external_filter_request) =
  if
    String.length request.Model_effect.program = 0
    || Filename.is_relative request.Model_effect.program
  then Error (error "program must be a non-empty absolute executable path")
  else if not (valid_argument request.Model_effect.program) then
    Error (error "program path must not contain NUL")
  else if
    List.exists
      (fun argument -> not (valid_argument argument))
      request.Model_effect.arguments
  then Error (error "arguments must not contain NUL")
  else Ok ()

let validate_input input =
  if String.length input > maximum_bytes then
    Error
      (error
         (Printf.sprintf "selection input exceeds the %d-byte limit"
            maximum_bytes))
  else Ok ()

let with_unix_error operation callback =
  try callback () with
  | Unix.Unix_error (reason, function_name, argument) ->
      let location =
        match (function_name, argument) with
        | "", _ -> ""
        | function_name, "" -> " in " ^ function_name
        | function_name, argument ->
            " in " ^ function_name ^ "(" ^ argument ^ ")"
      in
      Error (error (operation ^ ": " ^ Unix.error_message reason ^ location))
  | Sys_error message -> Error (error (operation ^ ": " ^ message))

let read_file_bounded path ~limit =
  let channel = open_in_bin path in
  Fun.protect
    ~finally:(fun () -> close_in_noerr channel)
    (fun () ->
      let buffer = Buffer.create 1024 in
      let chunk = Bytes.create 4096 in
      let rec loop count =
        let received =
          try input channel chunk 0 (Bytes.length chunk) with End_of_file -> 0
        in
        if received = 0 then Ok (Buffer.contents buffer)
        else if count + received > limit then
          Error
            (error
               (Printf.sprintf "program output exceeds the %d-byte limit" limit))
        else (
          Buffer.add_subbytes buffer chunk 0 received;
          loop (count + received))
      in
      loop 0)

let write_file path contents =
  let channel = open_out_bin path in
  Fun.protect
    ~finally:(fun () -> close_out_noerr channel)
    (fun () -> output_string channel contents)

let close_fd_noerr fd = try Unix.close fd with Unix.Unix_error _ -> ()

let spawn request ~input_path ~output_path ~error_path =
  let input = Unix.openfile input_path [ Unix.O_RDONLY ] 0 in
  let output =
    Unix.openfile output_path [ Unix.O_WRONLY; Unix.O_TRUNC ] 0o600
  in
  let error = Unix.openfile error_path [ Unix.O_WRONLY; Unix.O_TRUNC ] 0o600 in
  Fun.protect
    ~finally:(fun () ->
      close_fd_noerr input;
      close_fd_noerr output;
      close_fd_noerr error)
    (fun () ->
      Unix.create_process_env request.Model_effect.program
        (Array.of_list
           (request.Model_effect.program :: request.Model_effect.arguments))
        (Unix.environment ()) input output error)

let terminate pid =
  (try Unix.kill pid Sys.sigterm with Unix.Unix_error _ -> ());
  ignore (Unix.select [] [] [] 0.1);
  match Unix.waitpid [ Unix.WNOHANG ] pid with
  | 0, _ ->
      (try Unix.kill pid Sys.sigkill with Unix.Unix_error _ -> ());
      ignore (Unix.waitpid [] pid)
  | _ -> ()

let wait_for_process pid =
  let deadline = Unix.gettimeofday () +. maximum_runtime_seconds in
  let rec loop () =
    match Unix.waitpid [ Unix.WNOHANG ] pid with
    | 0, _ when Unix.gettimeofday () < deadline ->
        ignore (Unix.select [] [] [] 0.01);
        loop ()
    | 0, _ ->
        terminate pid;
        Error
          (error
             (Printf.sprintf "program exceeded the %.0f-second limit"
                maximum_runtime_seconds))
    | _, status -> Ok status
  in
  loop ()

let status_error program stderr = function
  | Unix.WEXITED 0 -> assert false
  | Unix.WEXITED status ->
      error (Printf.sprintf "%s exited with status %d%s" program status stderr)
  | Unix.WSIGNALED signal ->
      error
        (Printf.sprintf "%s was terminated by signal %d%s" program signal stderr)
  | Unix.WSTOPPED signal ->
      error (Printf.sprintf "%s stopped with signal %d%s" program signal stderr)

let stderr_detail path =
  match read_file_bounded path ~limit:maximum_stderr_bytes with
  | Ok contents when String.length contents = 0 -> ""
  | Ok contents -> (
      match Text_buffer.of_utf8 contents with
      | Ok _ -> ": " ^ String.trim contents
      | Error _ -> ": program wrote invalid UTF-8 to stderr")
  | Error _ -> ": program stderr exceeded the diagnostic limit"

let with_temporary_files callback =
  let input_path = Filename.temp_file "zenbu-filter-input-" ".tmp" in
  let output_path = Filename.temp_file "zenbu-filter-output-" ".tmp" in
  let error_path = Filename.temp_file "zenbu-filter-error-" ".tmp" in
  Fun.protect
    ~finally:(fun () ->
      List.iter
        (fun path -> try Sys.remove path with Sys_error _ -> ())
        [ input_path; output_path; error_path ])
    (fun () -> callback ~input_path ~output_path ~error_path)

let run request input =
  Result.bind (validate_request request) (fun () ->
      Result.bind (validate_input input) (fun () ->
          with_unix_error "external filter" (fun () ->
              with_temporary_files (fun ~input_path ~output_path ~error_path ->
                  write_file input_path input;
                  let pid =
                    spawn request ~input_path ~output_path ~error_path
                  in
                  Result.bind (wait_for_process pid) (fun status ->
                      match status with
                      | Unix.WEXITED 0 ->
                          Result.bind
                            (read_file_bounded output_path ~limit:maximum_bytes)
                            (fun output ->
                              Text_buffer.of_utf8 output
                              |> Result.map_error (fun _ ->
                                  error "program returned invalid UTF-8")
                              |> Result.map (fun _ -> output))
                      | _ ->
                          Error
                            (status_error request.Model_effect.program
                               (stderr_detail error_path) status))))))
