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

type utf8_state =
  | Valid
  | Continuation of { remaining : int; minimum : int; maximum : int }
  | Invalid

type stream = {
  buffer : Buffer.t;
  mutable retained_bytes : int;
  mutable dropped_bytes : int;
  mutable observed_bytes : int;
  mutable utf8 : utf8_state;
}

type stop_reason = Cancellation | Timeout | Output_limit

type job = {
  id : int;
  request : Model_effect.background_process_request;
  pid : int;
  started_at : float;
  stdout : stream;
  stderr : stream;
  mutable total_output_bytes : int;
  mutable status : status;
  mutable stop_reason : stop_reason option;
  mutable termination_started_at : float option;
  mutable hard_kill_sent : bool;
  mutable worker : Thread.t option;
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

external spawn_isolated :
  string ->
  string array ->
  string array ->
  Unix.file_descr ->
  Unix.file_descr ->
  Unix.file_descr ->
  string ->
  int = "zenbu_background_job_spawn_bytecode" "zenbu_background_job_spawn"

let maximum_jobs = 64
let maximum_completed_jobs = 64
let maximum_output_bytes = 16 * 1024 * 1024
let maximum_retained_stream_bytes = 64 * 1024
let maximum_runtime_seconds = 5.
let termination_grace_seconds = 0.1
let read_chunk_bytes = 8192
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
  | Failure message -> Error (error (operation ^ ": " ^ message))

let close_fd_noerr fd = try Unix.close fd with Unix.Unix_error _ -> ()

let controlled_environment =
  [| "PATH=/usr/bin:/bin"; "LANG=C"; "LC_ALL=C"; "TERM=dumb" |]

let new_stream () =
  {
    buffer = Buffer.create 1024;
    retained_bytes = 0;
    dropped_bytes = 0;
    observed_bytes = 0;
    utf8 = Valid;
  }

let utf8_after_byte state byte =
  match state with
  | Invalid -> Invalid
  | Valid -> (
      match byte with
      | value when value <= 0x7F -> Valid
      | value when value >= 0xC2 && value <= 0xDF ->
          Continuation { remaining = 1; minimum = 0x80; maximum = 0xBF }
      | 0xE0 -> Continuation { remaining = 2; minimum = 0xA0; maximum = 0xBF }
      | 0xED -> Continuation { remaining = 2; minimum = 0x80; maximum = 0x9F }
      | value
        when (value >= 0xE1 && value <= 0xEC) || (value >= 0xEE && value <= 0xEF)
        ->
          Continuation { remaining = 2; minimum = 0x80; maximum = 0xBF }
      | 0xF0 -> Continuation { remaining = 3; minimum = 0x90; maximum = 0xBF }
      | value when value >= 0xF1 && value <= 0xF3 ->
          Continuation { remaining = 3; minimum = 0x80; maximum = 0xBF }
      | 0xF4 -> Continuation { remaining = 3; minimum = 0x80; maximum = 0x8F }
      | _ -> Invalid)
  | Continuation { remaining; minimum; maximum } ->
      if byte < minimum || byte > maximum then Invalid
      else if remaining = 1 then Valid
      else
        Continuation
          { remaining = remaining - 1; minimum = 0x80; maximum = 0xBF }

let retain_stream_bytes stream bytes count =
  let accepted =
    min count (maximum_retained_stream_bytes - stream.retained_bytes)
  in
  if accepted > 0 then Buffer.add_subbytes stream.buffer bytes 0 accepted;
  stream.retained_bytes <- stream.retained_bytes + accepted;
  stream.dropped_bytes <- stream.dropped_bytes + count - accepted;
  stream.observed_bytes <- stream.observed_bytes + count;
  for offset = 0 to count - 1 do
    stream.utf8 <-
      utf8_after_byte stream.utf8 (Char.code (Bytes.get bytes offset))
  done

let valid_prefix value =
  let rec loop length remaining =
    if length = 0 then ""
    else
      let candidate = String.sub value 0 length in
      match Text_buffer.of_utf8 candidate with
      | Ok _ -> candidate
      | Error _ when remaining > 0 -> loop (length - 1) (remaining - 1)
      | Error _ -> ""
  in
  loop (String.length value) 4

let trim_for_display maximum value =
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
  let buffer = Buffer.create stop in
  for index = 0 to stop - 1 do
    let character = value.[index] in
    if Char.code character < 32 && character <> '\n' && character <> '\t' then
      Buffer.add_char buffer ' '
    else Buffer.add_char buffer character
  done;
  let trimmed = Buffer.contents buffer in
  if stop < String.length value then trimmed ^ "…" else trimmed

let stream_text ~complete ~name stream =
  let invalid =
    match stream.utf8 with
    | Invalid -> true
    | Valid -> false
    | Continuation _ -> complete
  in
  if invalid then Error ("program wrote invalid UTF-8 to " ^ name)
  else
    let retained = Buffer.contents stream.buffer |> valid_prefix in
    let retained =
      if stream.dropped_bytes = 0 then retained
      else
        retained
        ^ Printf.sprintf "\n[… %s truncated; %d bytes not retained]" name
            stream.dropped_bytes
    in
    Ok retained

let stderr_text ~complete stream =
  match stream_text ~complete ~name:"stderr" stream with
  | Ok text -> text
  | Error message -> message

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

let signal_process_group pid signal =
  try Unix.kill (-pid) signal
  with Unix.Unix_error _ -> (
    try Unix.kill pid signal with Unix.Unix_error _ -> ())

let request_termination t job reason =
  Mutex.lock t.lock;
  let should_signal =
    match job.termination_started_at with
    | Some _ -> false
    | None ->
        job.termination_started_at <- Some (Unix.gettimeofday ());
        (match reason with
        | Some reason -> job.stop_reason <- Some reason
        | None -> ());
        true
  in
  Mutex.unlock t.lock;
  if should_signal then (
    signal_process_group job.pid Sys.sigterm;
    signal t)

let request_hard_kill t job =
  Mutex.lock t.lock;
  let should_signal =
    match job.termination_started_at with
    | Some started
      when (not job.hard_kill_sent)
           && Unix.gettimeofday () -. started >= termination_grace_seconds ->
        job.hard_kill_sent <- true;
        true
    | None | Some _ -> false
  in
  Mutex.unlock t.lock;
  if should_signal then (
    signal_process_group job.pid Sys.sigkill;
    signal t)

let terminate_before_worker pid =
  signal_process_group pid Sys.sigterm;
  ignore (Unix.select [] [] [] termination_grace_seconds);
  signal_process_group pid Sys.sigkill;
  try ignore (Unix.waitpid [] pid) with Unix.Unix_error _ -> ()

let spawn request =
  with_unix_error "background job" (fun () ->
      let stdin = Unix.openfile "/dev/null" [ Unix.O_RDONLY ] 0 in
      let stdout_read, stdout_write = Unix.pipe () in
      let stderr_read, stderr_write = Unix.pipe () in
      let descriptors =
        [ stdin; stdout_read; stdout_write; stderr_read; stderr_write ]
      in
      try
        Unix.set_nonblock stdout_read;
        Unix.set_nonblock stderr_read;
        Unix.set_close_on_exec stdout_read;
        Unix.set_close_on_exec stderr_read;
        let pid =
          spawn_isolated request.Model_effect.program
            (Array.of_list (request.Model_effect.program :: request.arguments))
            controlled_environment stdin stdout_write stderr_write "/"
        in
        close_fd_noerr stdin;
        close_fd_noerr stdout_write;
        close_fd_noerr stderr_write;
        Ok (pid, stdout_read, stderr_read)
      with exception_ ->
        List.iter close_fd_noerr descriptors;
        raise exception_)

let record_stream_chunk t (job : job) stream bytes count =
  Mutex.lock t.lock;
  let changed, exceeds_limit =
    match job.status with
    | Running when not t.closed ->
        retain_stream_bytes stream bytes count;
        job.total_output_bytes <- job.total_output_bytes + count;
        (true, job.total_output_bytes > maximum_output_bytes)
    | Running | Succeeded _ | Failed _ | Timed_out _ | Cancelled ->
        (false, false)
  in
  Mutex.unlock t.lock;
  if changed then signal t;
  if exceeds_limit then request_termination t job (Some Output_limit)

let drain_stream t job fd stream =
  let chunk = Bytes.create read_chunk_bytes in
  let rec loop () =
    try
      match Unix.read fd chunk 0 (Bytes.length chunk) with
      | 0 -> false
      | count ->
          record_stream_chunk t job stream chunk count;
          loop ()
    with Unix.Unix_error ((Unix.EAGAIN | Unix.EWOULDBLOCK), _, _) -> true
  in
  loop ()

let final_status job exit_status =
  let duration = Unix.gettimeofday () -. job.started_at in
  match (job.status, job.stop_reason) with
  | Cancelled, _ | _, Some Cancellation -> Cancelled
  | _, Some Timeout -> Timed_out { duration }
  | _, Some Output_limit ->
      Failed
        {
          reason =
            Printf.sprintf "program output exceeded the %d-byte limit"
              maximum_output_bytes;
          stdout =
            Option.value ~default:""
              (Result.to_option
                 (stream_text ~complete:true ~name:"stdout" job.stdout));
          stderr = stderr_text ~complete:true job.stderr;
          duration;
        }
  | Running, None -> (
      let stdout = stream_text ~complete:true ~name:"stdout" job.stdout in
      let stderr = stderr_text ~complete:true job.stderr in
      match exit_status with
      | Unix.WEXITED 0 -> (
          match stdout with
          | Ok stdout -> Succeeded { stdout; stderr; duration }
          | Error reason -> Failed { reason; stdout = ""; stderr; duration })
      | Unix.WEXITED code ->
          Failed
            {
              reason = Printf.sprintf "exited with status %d" code;
              stdout = Option.value ~default:"" (Result.to_option stdout);
              stderr;
              duration;
            }
      | Unix.WSIGNALED signal ->
          Failed
            {
              reason = Printf.sprintf "was terminated by signal %d" signal;
              stdout = Option.value ~default:"" (Result.to_option stdout);
              stderr;
              duration;
            }
      | Unix.WSTOPPED signal ->
          Failed
            {
              reason = Printf.sprintf "stopped with signal %d" signal;
              stdout = Option.value ~default:"" (Result.to_option stdout);
              stderr;
              duration;
            })
  | (Succeeded _ | Failed _ | Timed_out _), None -> job.status

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

let job_state t (job : job) =
  Mutex.lock t.lock;
  let closed = t.closed in
  let status = job.status in
  let stop_reason = job.stop_reason in
  Mutex.unlock t.lock;
  (closed, status, stop_reason)

let worker t job stdout stderr =
  let stdout = ref (Some stdout) in
  let stderr = ref (Some stderr) in
  let exit_status = ref None in
  let deadline = job.started_at +. maximum_runtime_seconds in
  let close_stream stream =
    Option.iter close_fd_noerr !stream;
    stream := None
  in
  let rec loop () =
    let closed, status, stop_reason = job_state t job in
    if closed || status = Cancelled || stop_reason = Some Cancellation then
      request_termination t job (Some Cancellation)
    else if stop_reason = Some Output_limit then
      request_termination t job (Some Output_limit)
    else if !exit_status = None && Unix.gettimeofday () >= deadline then
      request_termination t job (Some Timeout);
    request_hard_kill t job;
    let ready, _, _ =
      Unix.select (Option.to_list !stdout @ Option.to_list !stderr) [] [] 0.01
    in
    Option.iter
      (fun fd ->
        if List.mem fd ready && not (drain_stream t job fd job.stdout) then
          close_stream stdout)
      !stdout;
    Option.iter
      (fun fd ->
        if List.mem fd ready && not (drain_stream t job fd job.stderr) then
          close_stream stderr)
      !stderr;
    (if !exit_status = None then
       match Unix.waitpid [ Unix.WNOHANG ] job.pid with
       | 0, _ -> ()
       | _, status ->
           exit_status := Some status;
           request_termination t job None);
    request_hard_kill t job;
    match (!exit_status, !stdout, !stderr) with
    | Some status, None, None -> complete t job (final_status job status)
    | None, _, _ | Some _, Some _, _ | Some _, _, Some _ -> loop ()
  in
  Fun.protect
    ~finally:(fun () ->
      close_stream stdout;
      close_stream stderr)
    loop

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
      Mutex.lock t.lock;
      let running =
        List.fold_left
          (fun count (job : job) ->
            match job.status with Running -> count + 1 | _ -> count)
          0 t.jobs
      in
      let admitted = (not t.closed) && running + t.starting < maximum_jobs in
      let id = t.next_id in
      if admitted then (
        t.next_id <- t.next_id + 1;
        t.starting <- t.starting + 1);
      Mutex.unlock t.lock;
      if not admitted then
        Error
          (error
             (if t.closed then "job registry is closed"
              else Printf.sprintf "job limit (%d) has been reached" maximum_jobs))
      else
        let release_start () =
          Mutex.lock t.lock;
          t.starting <- max 0 (t.starting - 1);
          Mutex.unlock t.lock
        in
        match spawn request with
        | Error error ->
            release_start ();
            Error error
        | Ok (pid, stdout, stderr) ->
            let job =
              {
                id;
                request;
                pid;
                started_at = Unix.gettimeofday ();
                stdout = new_stream ();
                stderr = new_stream ();
                total_output_bytes = 0;
                status = Running;
                stop_reason = None;
                termination_started_at = None;
                hard_kill_sent = false;
                worker = None;
              }
            in
            Mutex.lock t.lock;
            t.starting <- max 0 (t.starting - 1);
            let registry_closed = t.closed in
            if not registry_closed then t.jobs <- job :: t.jobs;
            Mutex.unlock t.lock;
            if registry_closed then (
              terminate_before_worker pid;
              close_fd_noerr stdout;
              close_fd_noerr stderr;
              Error (error "job registry is closed"))
            else (
              Mutex.lock t.lock;
              if t.closed then (
                t.jobs <-
                  List.filter
                    (fun (candidate : job) -> candidate.id <> id)
                    t.jobs;
                Mutex.unlock t.lock;
                terminate_before_worker pid;
                close_fd_noerr stdout;
                close_fd_noerr stderr;
                Error (error "job registry is closed"))
              else
                try
                  let worker =
                    Thread.create (fun () -> worker t job stdout stderr) ()
                  in
                  job.worker <- Some worker;
                  Mutex.unlock t.lock;
                  Ok id
                with exception_ ->
                  Mutex.unlock t.lock;
                  terminate_before_worker pid;
                  close_fd_noerr stdout;
                  close_fd_noerr stderr;
                  Mutex.lock t.lock;
                  t.jobs <-
                    List.filter
                      (fun (candidate : job) -> candidate.id <> id)
                      t.jobs;
                  Mutex.unlock t.lock;
                  raise exception_))

let cancel t ~id =
  Mutex.lock t.lock;
  let result =
    match List.find_opt (fun (job : job) -> job.id = id) t.jobs with
    | None -> Error (error (Printf.sprintf "job %d is not retained" id))
    | Some job -> (
        match job.status with
        | Running ->
            job.status <- Cancelled;
            Ok job
        | Succeeded _ | Failed _ | Timed_out _ | Cancelled ->
            Error (error (Printf.sprintf "job %d is not running" id)))
  in
  Mutex.unlock t.lock;
  match result with
  | Error _ as error -> error
  | Ok job ->
      request_termination t job (Some Cancellation);
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

let current_stream_sections (job : job) =
  [
    (match stream_text ~complete:false ~name:"stdout" job.stdout with
    | Ok "" -> None
    | Ok stdout -> Some ("stdout\n" ^ stdout)
    | Error message -> Some message);
    (match stream_text ~complete:false ~name:"stderr" job.stderr with
    | Ok "" -> None
    | Ok stderr -> Some ("stderr\n" ^ stderr)
    | Error message -> Some message);
  ]
  |> List.filter_map Fun.id

let output_report (job : job) =
  match job.status with
  | Running ->
      Ok
        (String.concat "\n\n"
           (Printf.sprintf "background job %d running" job.id
           :: current_stream_sections job))
  | Succeeded { stdout; _ } -> Ok stdout
  | Failed { reason; stdout; stderr; _ } ->
      let sections =
        [
          Some (Printf.sprintf "background job %d failed: %s" job.id reason);
          (if String.length stdout = 0 then None else Some ("stdout\n" ^ stdout));
          (if String.length stderr = 0 then None else Some ("stderr\n" ^ stderr));
        ]
        |> List.filter_map Fun.id
      in
      Ok (String.concat "\n\n" sections)
  | Timed_out { duration } ->
      Ok
        (String.concat "\n\n"
           (Printf.sprintf "background job %d timed out after %.3fs" job.id
              duration
           :: current_stream_sections job))
  | Cancelled ->
      Ok
        (String.concat "\n\n"
           (Printf.sprintf "background job %d was cancelled" job.id
           :: current_stream_sections job))

let output t ~id =
  Mutex.lock t.lock;
  let result =
    match List.find_opt (fun (job : job) -> job.id = id) t.jobs with
    | None -> Error (error (Printf.sprintf "job %d is not retained" id))
    | Some job -> output_report job
  in
  Mutex.unlock t.lock;
  result

let live_lines stream name =
  match stream_text ~complete:false ~name stream with
  | Ok "" -> []
  | Ok text -> [ "  " ^ name ^ ": " ^ trim_for_display 2048 text ]
  | Error message -> [ "  " ^ name ^ ": " ^ message ]

let lines t =
  Mutex.lock t.lock;
  let jobs = List.rev t.jobs in
  let result =
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
            | Running ->
                [ prefix ^ " running" ]
                @ live_lines job.stdout "stdout"
                @ live_lines job.stderr "stderr"
            | Succeeded { stdout; stderr; duration } ->
                [ prefix ^ Printf.sprintf " succeeded %.3fs" duration ]
                @ (if String.length stdout = 0 then []
                   else [ "  stdout: " ^ trim_for_display 2048 stdout ])
                @
                if String.length stderr = 0 then []
                else [ "  stderr: " ^ trim_for_display 2048 stderr ]
            | Failed { reason; stdout; stderr; duration } ->
                [ prefix ^ Printf.sprintf " failed %.3fs: %s" duration reason ]
                @ (if String.length stdout = 0 then []
                   else [ "  stdout: " ^ trim_for_display 2048 stdout ])
                @
                if String.length stderr = 0 then []
                else [ "  stderr: " ^ trim_for_display 2048 stderr ]
            | Timed_out { duration } ->
                [ prefix ^ Printf.sprintf " timed out %.3fs" duration ]
                @ live_lines job.stdout "stdout"
                @ live_lines job.stderr "stderr"
            | Cancelled ->
                [ prefix ^ " cancelled" ]
                @ live_lines job.stdout "stdout"
                @ live_lines job.stderr "stderr")
          jobs)
  in
  Mutex.unlock t.lock;
  result

let close t =
  Mutex.lock t.lock;
  let should_close = not t.closed in
  t.closed <- true;
  let jobs = t.jobs in
  let running =
    List.filter
      (fun (job : job) -> match job.status with Running -> true | _ -> false)
      jobs
  in
  List.iter
    (fun (job : job) ->
      match job.status with
      | Running -> job.status <- Cancelled
      | Succeeded _ | Failed _ | Timed_out _ | Cancelled -> ())
    jobs;
  Mutex.unlock t.lock;
  if should_close then (
    List.iter
      (fun (job : job) -> request_termination t job (Some Cancellation))
      running;
    List.iter (fun (job : job) -> Option.iter Thread.join job.worker) jobs;
    close_fd_noerr t.wake_read;
    close_fd_noerr t.wake_write)
