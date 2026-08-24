type event_kind =
  | Modified
  | Deleted
  | Replaced
  | Renamed
  | Overflow
  | Failure of string

type event = { path : string option; kind : event_kind }

type watched = {
  path : string;
  snapshot : File_io.snapshot;
  mutable last : File_io.watch_state;
}

type t = {
  interval_seconds : float;
  read_fd : Unix.file_descr;
  write_fd : Unix.file_descr;
  lock : Mutex.t;
  mutable watched : watched list;
  mutable events_rev : event list;
  mutable closed : bool;
  mutable worker : Thread.t option;
}

type fake = { watcher : t }

let maximum_queued_events = 256

let rec take maximum values =
  match (maximum, values) with
  | 0, _ | _, [] -> []
  | maximum, value :: rest -> value :: take (maximum - 1) rest

let queue_event watcher event =
  if List.length watcher.events_rev < maximum_queued_events then (
    watcher.events_rev <- event :: watcher.events_rev;
    true)
  else if
    List.exists
      (fun (event : event) ->
        match event.kind with Overflow -> true | _ -> false)
      watcher.events_rev
  then false
  else (
    watcher.events_rev <-
      { path = None; kind = Overflow }
      :: take (maximum_queued_events - 1) watcher.events_rev;
    true)

let event_kind_name = function
  | Modified -> "modified"
  | Deleted -> "deleted"
  | Replaced -> "replaced"
  | Renamed -> "renamed"
  | Overflow -> "overflow"
  | Failure message -> "failure: " ^ message

let notify watcher =
  try ignore (Unix.write_substring watcher.write_fd "x" 0 1)
  with Unix.Unix_error _ -> ()

let push_event watcher event =
  Mutex.lock watcher.lock;
  let queued = (not watcher.closed) && queue_event watcher event in
  Mutex.unlock watcher.lock;
  if queued then notify watcher

let state_event = function
  | File_io.Current -> None
  | Changed -> Some Modified
  | Replaced -> Some Replaced
  | Renamed -> Some Renamed
  | Deleted -> Some Deleted
  | Failure message -> Some (Failure message)

let scan watcher =
  Mutex.lock watcher.lock;
  let events =
    if watcher.closed then []
    else
      List.filter_map
        (fun watch ->
          let state = File_io.watch_state watch.snapshot ~path:watch.path in
          if state = watch.last then None
          else (
            watch.last <- state;
            Option.map
              (fun kind -> { path = Some watch.path; kind })
              (state_event state)))
        watcher.watched
  in
  let queued =
    List.fold_left
      (fun queued event -> if queue_event watcher event then true else queued)
      false events
  in
  Mutex.unlock watcher.lock;
  if queued then notify watcher

let create ?(interval_seconds = 0.1) () =
  let read_fd, write_fd = Unix.pipe () in
  Unix.set_nonblock read_fd;
  Unix.set_nonblock write_fd;
  let watcher =
    {
      interval_seconds = max 0.01 interval_seconds;
      read_fd;
      write_fd;
      lock = Mutex.create ();
      watched = [];
      events_rev = [];
      closed = false;
      worker = None;
    }
  in
  let rec work () =
    Thread.delay watcher.interval_seconds;
    Mutex.lock watcher.lock;
    let closed = watcher.closed in
    Mutex.unlock watcher.lock;
    if not closed then (
      scan watcher;
      work ())
  in
  watcher.worker <- Some (Thread.create work ());
  watcher

let fake () =
  let read_fd, write_fd = Unix.pipe () in
  Unix.set_nonblock read_fd;
  Unix.set_nonblock write_fd;
  let watcher =
    {
      interval_seconds = 0.;
      read_fd;
      write_fd;
      lock = Mutex.create ();
      watched = [];
      events_rev = [];
      closed = false;
      worker = None;
    }
  in
  (watcher, { watcher })

let watch watcher ~path ~snapshot =
  Mutex.lock watcher.lock;
  (if not watcher.closed then
     let watched = { path; snapshot; last = File_io.Current } in
     watcher.watched <-
       watched
       :: List.filter
            (fun existing -> not (String.equal existing.path path))
            watcher.watched);
  Mutex.unlock watcher.lock

let unwatch watcher ~path =
  Mutex.lock watcher.lock;
  watcher.watched <-
    List.filter
      (fun existing -> not (String.equal existing.path path))
      watcher.watched;
  Mutex.unlock watcher.lock

let push fake ~path kind = push_event fake.watcher { path; kind }

let drain_fd descriptor =
  let bytes = Bytes.create 128 in
  let rec loop () =
    try if Unix.read descriptor bytes 0 (Bytes.length bytes) > 0 then loop ()
    with Unix.Unix_error ((Unix.EAGAIN | Unix.EWOULDBLOCK), _, _) -> ()
  in
  loop ()

let drain watcher =
  drain_fd watcher.read_fd;
  Mutex.lock watcher.lock;
  let events = List.rev watcher.events_rev in
  watcher.events_rev <- [];
  Mutex.unlock watcher.lock;
  events

let wakeup_fd watcher = watcher.read_fd

let close watcher =
  Mutex.lock watcher.lock;
  let already_closed = watcher.closed in
  watcher.closed <- true;
  Mutex.unlock watcher.lock;
  if not already_closed then (
    Option.iter Thread.join watcher.worker;
    Unix.close watcher.read_fd;
    Unix.close watcher.write_fd)
