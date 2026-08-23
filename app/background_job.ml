open Zenbu_kernel
open Zenbu_model_api

type status =
  | Running
  | Succeeded of { stdout : string; stderr : string; duration : float }
  | Failed of {
      reason : string;
      stdout : string;
      stderr : string;
      duration : float;
    }
  | Timed_out of { duration : float }
  | Cancelled

type job = {
  id : int;
  request : Model_effect.background_process_request;
  pid : int;
  started_at : float;
  mutable status : status;
}

type completion = { id : int; status : status }

type t = {
  lock : Mutex.t;
  wake_read : Unix.file_descr;
  wake_write : Unix.file_descr;
  completed : completion Queue.t;
  mutable jobs : job list;
  mutable starting : int;
  mutable next_id : int;
  mutable closed : bool;
}

let maximum_jobs = 64
let maximum_completed_jobs = 64
let maximum_output_bytes = 16 * 1024 * 1024
let maximum_stderr_bytes = 4096
let maximum_retained_stdout_bytes = 16 * 1024
let maximum_runtime_seconds = 5.
let sigpipe_ignored = ref false
let error message = Error.Background_job_error message
let valid_argument value = not (String.contains value '\000')

let ensure_sigpipe_ignored () =
  if not !sigpipe_ignored then (
    Sys.set_signal Sys.sigpipe Sys.Signal_ignore;
    sigpipe_ignored := true)

let validate_request (request : Model_effect.background_process_request) =
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

let close_fd_noerr fd = try Unix.close fd with Unix.Unix_error _ -> ()

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

let temporary_paths () =
  ( Filename.temp_file "zenbu-job-output-" ".tmp",
    Filename.temp_file "zenbu-job-error-" ".tmp" )

let remove_file path = try Sys.remove path with Sys_error _ -> ()

let spawn request ~output_path ~error_path =
  let stdin = Unix.openfile "/dev/null" [ Unix.O_RDONLY ] 0 in
  let stdout =
    Unix.openfile output_path [ Unix.O_WRONLY; Unix.O_TRUNC ] 0o600
  in
  let stderr = Unix.openfile error_path [ Unix.O_WRONLY; Unix.O_TRUNC ] 0o600 in
  Fun.protect
    ~finally:(fun () ->
      close_fd_noerr stdin;
      close_fd_noerr stdout;
      close_fd_noerr stderr)
    (fun () ->
      Unix.create_process_env request.Model_effect.program
        (Array.of_list
           (request.Model_effect.program :: request.Model_effect.arguments))
        (Unix.environment ()) stdin stdout stderr)

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
        `Timed_out
    | _, status -> `Exited status
  in
  loop ()

let trim_for_display maximum value =
  let buffer = Buffer.create (min maximum (String.length value)) in
  let stop =
    if String.length value <= maximum then String.length value
    else
      match Text_buffer.of_utf8 value with
      | Error _ -> maximum
      | Ok text ->
          let rec previous_boundary offset =
            if offset <= 0 || Text_buffer.is_code_point_boundary text offset
            then offset
            else previous_boundary (offset - 1)
          in
          previous_boundary maximum
  in
  let rec loop index =
    if index >= stop then ()
    else
      let character = value.[index] in
      if Char.code character < 32 && character <> '\n' && character <> '\t' then
        Buffer.add_char buffer ' '
      else Buffer.add_char buffer character;
      loop (index + 1)
  in
  loop 0;
  let trimmed = Buffer.contents buffer in
  if stop < String.length value then trimmed ^ "…" else trimmed

let stderr_from_path path =
  match read_file_bounded path ~limit:maximum_stderr_bytes with
  | Ok value -> (
      match Text_buffer.of_utf8 value with
      | Ok _ -> trim_for_display maximum_stderr_bytes value
      | Error _ -> "program wrote invalid UTF-8 to stderr")
  | Error _ -> "program stderr exceeded the diagnostic limit"

let stdout_from_path path =
  match read_file_bounded path ~limit:maximum_output_bytes with
  | Error error -> Error (Error.to_string error)
  | Ok value -> (
      match Text_buffer.of_utf8 value with
      | Error _ -> Error "program returned invalid UTF-8 output"
      | Ok _ -> Ok (trim_for_display maximum_retained_stdout_bytes value))

let status_of_result ~started_at ~output_path ~error_path = function
  | `Timed_out -> Timed_out { duration = Unix.gettimeofday () -. started_at }
  | `Exited (Unix.WEXITED 0) -> (
      match stdout_from_path output_path with
      | Error reason ->
          Failed
            {
              reason;
              stdout = "";
              stderr = stderr_from_path error_path;
              duration = Unix.gettimeofday () -. started_at;
            }
      | Ok stdout ->
          Succeeded
            {
              stdout;
              stderr = stderr_from_path error_path;
              duration = Unix.gettimeofday () -. started_at;
            })
  | `Exited (Unix.WEXITED code) ->
      Failed
        {
          reason = Printf.sprintf "exited with status %d" code;
          stdout =
            Option.value ~default:""
              (Result.to_option (stdout_from_path output_path));
          stderr = stderr_from_path error_path;
          duration = Unix.gettimeofday () -. started_at;
        }
  | `Exited (Unix.WSIGNALED signal) ->
      Failed
        {
          reason = Printf.sprintf "was terminated by signal %d" signal;
          stdout =
            Option.value ~default:""
              (Result.to_option (stdout_from_path output_path));
          stderr = stderr_from_path error_path;
          duration = Unix.gettimeofday () -. started_at;
        }
  | `Exited (Unix.WSTOPPED signal) ->
      Failed
        {
          reason = Printf.sprintf "stopped with signal %d" signal;
          stdout =
            Option.value ~default:""
              (Result.to_option (stdout_from_path output_path));
          stderr = stderr_from_path error_path;
          duration = Unix.gettimeofday () -. started_at;
        }

let signal t =
  try ignore (Unix.write_substring t.wake_write "x" 0 1)
  with Unix.Unix_error _ -> ()

let retain_latest count values =
  let rec take accepted remaining = function
    | [] -> List.rev accepted
    | _ when remaining = 0 -> List.rev accepted
    | value :: rest -> take (value :: accepted) (remaining - 1) rest
  in
  take [] count values

let retain_jobs jobs =
  let running, completed =
    List.partition
      (fun (job : job) -> match job.status with Running -> true | _ -> false)
      jobs
  in
  running @ retain_latest maximum_completed_jobs completed

let complete t (job : job) status =
  Mutex.lock t.lock;
  let signal_completion =
    match job.status with
    | Running when not t.closed ->
        job.status <- status;
        t.jobs <- retain_jobs t.jobs;
        Queue.add { id = job.id; status } t.completed;
        true
    | Running | Succeeded _ | Failed _ | Timed_out _ | Cancelled -> false
  in
  Mutex.unlock t.lock;
  if signal_completion then signal t

let worker t job ~output_path ~error_path =
  let status =
    try
      wait_for_process job.pid
      |> status_of_result ~started_at:job.started_at ~output_path ~error_path
    with
    | Unix.Unix_error (reason, _, _) ->
        Failed
          {
            reason = Unix.error_message reason;
            stdout =
              Option.value ~default:""
                (Result.to_option (stdout_from_path output_path));
            stderr = stderr_from_path error_path;
            duration = Unix.gettimeofday () -. job.started_at;
          }
    | Sys_error reason ->
        Failed
          {
            reason;
            stdout =
              Option.value ~default:""
                (Result.to_option (stdout_from_path output_path));
            stderr = stderr_from_path error_path;
            duration = Unix.gettimeofday () -. job.started_at;
          }
  in
  remove_file output_path;
  remove_file error_path;
  complete t job status

let create () =
  ensure_sigpipe_ignored ();
  let wake_read, wake_write = Unix.pipe () in
  Unix.set_nonblock wake_read;
  Unix.set_nonblock wake_write;
  {
    lock = Mutex.create ();
    wake_read;
    wake_write;
    completed = Queue.create ();
    jobs = [];
    starting = 0;
    next_id = 1;
    closed = false;
  }

let start t request =
  Result.bind (validate_request request) (fun () ->
      with_unix_error "background job" (fun () ->
          Mutex.lock t.lock;
          let running =
            List.fold_left
              (fun count (job : job) ->
                match job.status with Running -> count + 1 | _ -> count)
              0 t.jobs
          in
          let admitted =
            (not t.closed) && running + t.starting < maximum_jobs
          in
          let id = t.next_id in
          if admitted then (
            t.next_id <- t.next_id + 1;
            t.starting <- t.starting + 1);
          Mutex.unlock t.lock;
          if not admitted then
            Error
              (error
                 (if t.closed then "job registry is closed"
                  else
                    Printf.sprintf "job limit (%d) has been reached"
                      maximum_jobs))
          else
            let release_start () =
              Mutex.lock t.lock;
              t.starting <- max 0 (t.starting - 1);
              Mutex.unlock t.lock
            in
            let output_path, error_path =
              try temporary_paths ()
              with exception_ ->
                release_start ();
                raise exception_
            in
            match
              try Ok (spawn request ~output_path ~error_path)
              with Unix.Unix_error (reason, function_name, argument) ->
                Error (reason, function_name, argument)
            with
            | Error (reason, function_name, argument) ->
                release_start ();
                remove_file output_path;
                remove_file error_path;
                Error
                  (error
                     ("could not start " ^ request.Model_effect.program ^ ": "
                    ^ Unix.error_message reason
                     ^
                     match (function_name, argument) with
                     | "", _ -> ""
                     | function_name, "" -> " in " ^ function_name
                     | function_name, argument ->
                         " in " ^ function_name ^ "(" ^ argument ^ ")"))
            | Ok pid -> (
                let job =
                  {
                    id;
                    request;
                    pid;
                    started_at = Unix.gettimeofday ();
                    status = Running;
                  }
                in
                Mutex.lock t.lock;
                t.starting <- t.starting - 1;
                let registry_closed = t.closed in
                if not registry_closed then t.jobs <- job :: t.jobs;
                Mutex.unlock t.lock;
                if registry_closed then (
                  terminate pid;
                  remove_file output_path;
                  remove_file error_path;
                  Error (error "job registry is closed"))
                else
                  try
                    ignore
                      (Thread.create
                         (fun () -> worker t job ~output_path ~error_path)
                         ());
                    Ok id
                  with exception_ ->
                    terminate pid;
                    remove_file output_path;
                    remove_file error_path;
                    Mutex.lock t.lock;
                    t.jobs <-
                      List.filter
                        (fun (candidate : job) -> candidate.id <> id)
                        t.jobs;
                    Mutex.unlock t.lock;
                    raise exception_)))

let cancel t ~id =
  Mutex.lock t.lock;
  let result =
    match List.find_opt (fun (job : job) -> job.id = id) t.jobs with
    | None -> Error (error (Printf.sprintf "job %d is not retained" id))
    | Some job -> (
        match job.status with
        | Running ->
            job.status <- Cancelled;
            Ok job.pid
        | Succeeded _ | Failed _ | Timed_out _ | Cancelled ->
            Error (error (Printf.sprintf "job %d is not running" id)))
  in
  Mutex.unlock t.lock;
  match result with
  | Error _ as error -> error
  | Ok pid ->
      (try Unix.kill pid Sys.sigterm with Unix.Unix_error _ -> ());
      (try Unix.kill pid Sys.sigkill with Unix.Unix_error _ -> ());
      Ok ()

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

let wakeup_fd t = t.wake_read

let drain t =
  drain_wakeup t.wake_read;
  Mutex.lock t.lock;
  let rec take values =
    if Queue.is_empty t.completed then List.rev values
    else take (Queue.take t.completed :: values)
  in
  let values = take [] in
  Mutex.unlock t.lock;
  values

let request_summary request =
  request.Model_effect.program
  ^
  match request.arguments with
  | [] -> ""
  | arguments -> " " ^ String.concat " " arguments

let completion_message { id; status } =
  match status with
  | Running -> Printf.sprintf "background job %d is still running" id
  | Succeeded { duration; _ } ->
      Printf.sprintf "background job %d succeeded in %.3fs" id duration
  | Failed { reason; _ } ->
      Printf.sprintf "background job %d failed: %s" id reason
  | Timed_out { duration } ->
      Printf.sprintf "background job %d timed out after %.3fs" id duration
  | Cancelled -> Printf.sprintf "background job %d was cancelled" id

let output_report id = function
  | Running -> Error (error (Printf.sprintf "job %d is still running" id))
  | Succeeded { stdout; _ } -> Ok stdout
  | Failed { reason; stdout; stderr; _ } ->
      let sections =
        [
          Some (Printf.sprintf "background job %d failed: %s" id reason);
          (if String.length stdout = 0 then None else Some ("stdout\n" ^ stdout));
          (if String.length stderr = 0 then None else Some ("stderr\n" ^ stderr));
        ]
        |> List.filter_map Fun.id
      in
      Ok (String.concat "\n\n" sections)
  | Timed_out { duration } ->
      Ok (Printf.sprintf "background job %d timed out after %.3fs" id duration)
  | Cancelled -> Ok (Printf.sprintf "background job %d was cancelled" id)

let output t ~id =
  Mutex.lock t.lock;
  let result =
    match List.find_opt (fun (job : job) -> job.id = id) t.jobs with
    | None -> Error (error (Printf.sprintf "job %d is not retained" id))
    | Some job -> output_report job.id job.status
  in
  Mutex.unlock t.lock;
  result

let lines t =
  Mutex.lock t.lock;
  let jobs = List.rev t.jobs in
  Mutex.unlock t.lock;
  "Jobs"
  ::
  (match jobs with
  | [] -> [ "no jobs" ]
  | jobs ->
      List.concat_map
        (fun (job : job) ->
          let prefix =
            Printf.sprintf "%d: %s" job.id (request_summary job.request)
          in
          match job.status with
          | Running -> [ prefix ^ " running" ]
          | Succeeded { stdout; stderr; duration } ->
              [ prefix ^ Printf.sprintf " succeeded %.3fs" duration ]
              @
              if String.length stdout = 0 then []
              else
                [ "  stdout: " ^ trim_for_display 2048 stdout ]
                @
                if String.length stderr = 0 then []
                else [ "  stderr: " ^ stderr ]
          | Failed { reason; stdout; stderr; duration } ->
              [ prefix ^ Printf.sprintf " failed %.3fs: %s" duration reason ]
              @ (if String.length stdout = 0 then []
                 else [ "  stdout: " ^ trim_for_display 2048 stdout ])
              @
              if String.length stderr = 0 then [] else [ "  stderr: " ^ stderr ]
          | Timed_out { duration } ->
              [ prefix ^ Printf.sprintf " timed out %.3fs" duration ]
          | Cancelled -> [ prefix ^ " cancelled" ])
        jobs)

let close t =
  Mutex.lock t.lock;
  let should_close = not t.closed in
  t.closed <- true;
  let pids =
    t.jobs
    |> List.filter_map (fun (job : job) ->
        match job.status with
        | Running ->
            job.status <- Cancelled;
            Some job.pid
        | Succeeded _ | Failed _ | Timed_out _ | Cancelled -> None)
  in
  Mutex.unlock t.lock;
  if should_close then (
    List.iter
      (fun pid -> try Unix.kill pid Sys.sigterm with Unix.Unix_error _ -> ())
      pids;
    close_fd_noerr t.wake_read;
    close_fd_noerr t.wake_write)
