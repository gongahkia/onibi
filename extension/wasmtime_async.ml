module Backend = Wasmtime_backend

type limits = { fuel : int; memory_bytes : int; deadline_ms : int }

type metrics = {
  compile_seconds : float;
  instantiate_seconds : float;
  call_seconds : float;
  fuel_consumed : int;
}

type initialization = {
  registrations : Zenbu_model_api.Extension_value.t;
  metrics : metrics;
}

type stop_reason = Cancelled | Deadline

type call_state =
  | Queued
  | Running
  | Completed of (Zenbu_model_api.Extension_value.t, string) result * metrics
  | Taken

type call = {
  id : int;
  token : string;
  request : Zenbu_model_api.Extension_value.t;
  deadline_at : float;
  mutable stop_reason : stop_reason option;
  mutable state : call_state;
}

type initialization_state =
  | Initializing
  | Initialized of (initialization, string) result

type t = {
  lock : Mutex.t;
  condition : Condition.t;
  wake_read : Unix.file_descr;
  wake_write : Unix.file_descr;
  queue : call Queue.t;
  mutable backend : Backend.t option;
  mutable active : call option;
  mutable initialization : initialization_state;
  mutable next_id : int;
  mutable closed : bool;
  mutable worker : Thread.t option;
  mutable watchdog : Thread.t option;
  limits : limits;
  entrypoint : string;
  capabilities : string list;
}

let maximum_pending_calls = 64
let watchdog_interval_seconds = 0.002
let now = Backend.monotonic_seconds

let metrics value =
  {
    compile_seconds = value.Backend.compile_seconds;
    instantiate_seconds = value.instantiate_seconds;
    call_seconds = value.call_seconds;
    fuel_consumed = value.fuel_consumed;
  }

let signal t =
  try ignore (Unix.write_substring t.wake_write "x" 0 1)
  with Unix.Unix_error _ -> ()

let close_fd_noerr fd = try Unix.close fd with Unix.Unix_error _ -> ()
let first = function [] -> None | value :: _ -> Some value

let drain_wakeup fd =
  let bytes = Bytes.create 256 in
  let rec loop () =
    try
      match Unix.read fd bytes 0 (Bytes.length bytes) with
      | 0 -> ()
      | _ -> loop ()
    with Unix.Unix_error _ -> ()
  in
  loop ()

let error_message = function
  | Cancelled -> "component call was cancelled"
  | Deadline -> "component deadline exhausted"

let complete_locked t call result metrics =
  match call.state with
  | Taken | Completed _ -> ()
  | Queued | Running ->
      call.state <- Completed (result, metrics);
      (match t.active with
      | Some active when active.id = call.id -> t.active <- None
      | None | Some _ -> ());
      Condition.broadcast t.condition;
      signal t

let cancel_locked t call reason =
  match call.state with
  | Taken | Completed _ -> None
  | Queued ->
      call.stop_reason <- Some reason;
      complete_locked t call
        (Error (error_message reason))
        {
          compile_seconds = 0.;
          instantiate_seconds = 0.;
          call_seconds = 0.;
          fuel_consumed = 0;
        };
      None
  | Running ->
      if Option.is_none call.stop_reason then call.stop_reason <- Some reason;
      t.backend

let rec worker_loop t =
  Mutex.lock t.lock;
  let rec next () =
    if Queue.is_empty t.queue then
      if t.closed then None
      else (
        Condition.wait t.condition t.lock;
        next ())
    else Some (Queue.take t.queue)
  in
  let next_call = next () in
  Mutex.unlock t.lock;
  match next_call with
  | None ->
      Option.iter Backend.dispose t.backend;
      Mutex.lock t.lock;
      t.backend <- None;
      Condition.broadcast t.condition;
      Mutex.unlock t.lock
  | Some call ->
      Mutex.lock t.lock;
      let execute =
        match call.state with
        | Queued ->
            call.state <- Running;
            t.active <- Some call;
            true
        | Running | Completed _ | Taken -> false
      in
      let backend = t.backend in
      Mutex.unlock t.lock;
      if execute then (
        let result, call_metrics =
          match backend with
          | None ->
              ( Error "component runtime is closed",
                {
                  compile_seconds = 0.;
                  instantiate_seconds = 0.;
                  call_seconds = 0.;
                  fuel_consumed = 0;
                } )
          | Some backend ->
              let result =
                Backend.invoke backend ~token:call.token ~request:call.request
              in
              (result, Backend.metrics backend |> metrics)
        in
        Mutex.lock t.lock;
        let result =
          match call.stop_reason with
          | None -> result
          | Some reason -> Error (error_message reason)
        in
        complete_locked t call result call_metrics;
        Mutex.unlock t.lock);
      worker_loop t

let worker t =
  let backend =
    Backend.load ~entrypoint:t.entrypoint ~capabilities:t.capabilities
      ~limits:
        Backend.{ fuel = t.limits.fuel; memory_bytes = t.limits.memory_bytes }
  in
  match backend with
  | Error message ->
      Mutex.lock t.lock;
      t.initialization <- Initialized (Error message);
      t.closed <- true;
      Condition.broadcast t.condition;
      Mutex.unlock t.lock
  | Ok backend -> (
      Mutex.lock t.lock;
      t.backend <- Some backend;
      Mutex.unlock t.lock;
      let started_at = now () in
      let registration_call =
        {
          id = 0;
          token = "register";
          request = Zenbu_model_api.Extension_value.Nil;
          deadline_at =
            started_at +. (float_of_int t.limits.deadline_ms /. 1000.);
          stop_reason = None;
          state = Running;
        }
      in
      Mutex.lock t.lock;
      t.active <- Some registration_call;
      Mutex.unlock t.lock;
      let registration = Backend.register backend in
      let registration_metrics = Backend.metrics backend |> metrics in
      Mutex.lock t.lock;
      t.active <- None;
      let initialization =
        registration
        |> Result.map (fun registrations ->
            { registrations; metrics = registration_metrics })
      in
      t.initialization <- Initialized initialization;
      (match initialization with Error _ -> t.closed <- true | Ok _ -> ());
      Condition.broadcast t.condition;
      Mutex.unlock t.lock;
      match initialization with
      | Error _ ->
          Backend.dispose backend;
          Mutex.lock t.lock;
          t.backend <- None;
          Condition.broadcast t.condition;
          Mutex.unlock t.lock
      | Ok _ -> worker_loop t)

let rec watchdog t =
  Thread.delay watchdog_interval_seconds;
  Mutex.lock t.lock;
  let closed = t.closed in
  let backend =
    match (t.active, t.backend) with
    | Some call, Some backend
      when Option.is_none call.stop_reason && now () >= call.deadline_at ->
        call.stop_reason <- Some Deadline;
        Some backend
    | _ -> None
  in
  Mutex.unlock t.lock;
  Option.iter Backend.interrupt backend;
  if not closed then watchdog t

let create ~entrypoint ~capabilities ~limits =
  if limits.deadline_ms <= 0 then Error "component deadline must be positive"
  else
    let wake_read, wake_write = Unix.pipe () in
    Unix.set_nonblock wake_read;
    Unix.set_nonblock wake_write;
    let value =
      {
        lock = Mutex.create ();
        condition = Condition.create ();
        wake_read;
        wake_write;
        queue = Queue.create ();
        backend = None;
        active = None;
        initialization = Initializing;
        next_id = 1;
        closed = false;
        worker = None;
        watchdog = None;
        limits;
        entrypoint;
        capabilities;
      }
    in
    let worker = Thread.create worker value in
    let watchdog = Thread.create watchdog value in
    value.worker <- Some worker;
    value.watchdog <- Some watchdog;
    Mutex.lock value.lock;
    let rec wait () =
      match value.initialization with
      | Initializing ->
          Condition.wait value.condition value.lock;
          wait ()
      | Initialized result -> result
    in
    let initialized = wait () in
    Mutex.unlock value.lock;
    match initialized with
    | Ok initialized -> Ok (value, initialized)
    | Error message ->
        Option.iter Thread.join value.worker;
        Option.iter Thread.join value.watchdog;
        close_fd_noerr value.wake_read;
        close_fd_noerr value.wake_write;
        Error message

let start t ~token ~request =
  Mutex.lock t.lock;
  let result =
    match t.initialization with
    | Initializing -> Error "component runtime is still initializing"
    | Initialized (Error message) -> Error message
    | Initialized (Ok _) when t.closed -> Error "component runtime is closed"
    | Initialized (Ok _) ->
        let retained =
          Queue.length t.queue + if Option.is_some t.active then 1 else 0
        in
        if retained >= maximum_pending_calls then
          Error "component pending-call limit has been reached"
        else
          let started_at = now () in
          let call =
            {
              id = t.next_id;
              token;
              request;
              deadline_at =
                started_at +. (float_of_int t.limits.deadline_ms /. 1000.);
              stop_reason = None;
              state = Queued;
            }
          in
          t.next_id <- t.next_id + 1;
          Queue.add call t.queue;
          Condition.signal t.condition;
          Ok call
  in
  Mutex.unlock t.lock;
  result

let take t call =
  drain_wakeup t.wake_read;
  Mutex.lock t.lock;
  let result =
    match call.state with
    | Completed (result, metrics) ->
        call.state <- Taken;
        Some (result, metrics)
    | Queued | Running | Taken -> None
  in
  Mutex.unlock t.lock;
  result

let invoke_blocking t ~token ~request =
  Result.bind (start t ~token ~request) (fun call ->
      Mutex.lock t.lock;
      let rec wait () =
        match call.state with
        | Completed (result, metrics) ->
            call.state <- Taken;
            (result, metrics)
        | Queued | Running ->
            Condition.wait t.condition t.lock;
            wait ()
        | Taken ->
            ( Error "component call result was already consumed",
              {
                compile_seconds = 0.;
                instantiate_seconds = 0.;
                call_seconds = 0.;
                fuel_consumed = 0;
              } )
      in
      let result = wait () in
      Mutex.unlock t.lock;
      Ok result)

let wakeup_fd t = t.wake_read

let closed t =
  Mutex.lock t.lock;
  let result = t.closed in
  Mutex.unlock t.lock;
  result

let cancel t call =
  Mutex.lock t.lock;
  let backend = cancel_locked t call Cancelled in
  Condition.broadcast t.condition;
  Mutex.unlock t.lock;
  Option.iter Backend.interrupt backend

let close t =
  Mutex.lock t.lock;
  let should_close = not t.closed in
  t.closed <- true;
  let queued = Queue.to_seq t.queue |> List.of_seq in
  let backend =
    List.filter_map (fun call -> cancel_locked t call Cancelled) queued
    |> List.rev |> first
  in
  let active_backend =
    match t.active with
    | None -> None
    | Some call -> cancel_locked t call Cancelled
  in
  Condition.broadcast t.condition;
  Mutex.unlock t.lock;
  if should_close then (
    Option.iter Backend.interrupt active_backend;
    Option.iter Backend.interrupt backend;
    Option.iter Thread.join t.worker;
    Option.iter Thread.join t.watchdog;
    close_fd_noerr t.wake_read;
    close_fd_noerr t.wake_write)
