open Zenbu_kernel

type snapshot = {
  document_id : string;
  document_version : int;
  contents : string;
}

type completion = {
  id : int;
  provider : Provider.t;
  operation : string;
  snapshot : snapshot;
  input : Input_event.t;
  provenance : Provenance.t;
  result : (Model_effect.t list, Error.t) result;
}

type entry_state = Planned | Running of Extension_host.call

type entry = {
  id : int;
  response : Extension_host.response;
  request : Extension_host.request;
  snapshot : snapshot;
  decode :
    Extension_host.request ->
    Extension_value.t ->
    (Model_effect.t list, Error.t) result;
  mutable state : entry_state;
  mutable owner : int option;
  mutable input : Input_event.t option;
  mutable provenance : Provenance.t option;
}

let lock = Mutex.create ()
let next_id = ref 1
let next_owner = ref 1
let entries : entry list ref = ref []

let new_owner () =
  Mutex.lock lock;
  let owner = !next_owner in
  next_owner := owner + 1;
  Mutex.unlock lock;
  owner

let snapshot context =
  {
    document_id = Editor_context.document_id context;
    document_version = Editor_context.document_version context;
    contents = Editor_context.contents context;
  }

let schedule ~response ~request ~context ~decode =
  match response with
  | Extension_host.Immediate _ ->
      Error
        (Error.Invalid_provenance
           "attempted to defer an immediate extension response")
  | Extension_host.Deferred _ ->
      Mutex.lock lock;
      let id = !next_id in
      next_id := id + 1;
      entries :=
        {
          id;
          response;
          request;
          snapshot = snapshot context;
          decode;
          state = Planned;
          owner = None;
          input = None;
          provenance = None;
        }
        :: !entries;
      Mutex.unlock lock;
      Ok id

let activate ~id ~owner ~input ~provenance =
  Mutex.lock lock;
  let entry = List.find_opt (fun (entry : entry) -> entry.id = id) !entries in
  match entry with
  | None ->
      Mutex.unlock lock;
      Error
        (Error.Invalid_provenance
           ("unknown deferred extension call " ^ string_of_int id))
  | Some { state = Running _; _ } ->
      Mutex.unlock lock;
      Error
        (Error.Invalid_provenance
           ("deferred extension call was activated twice: " ^ string_of_int id))
  | Some entry ->
      let started = Extension_host.start entry.response in
      (match started with
      | Error _ -> entries := List.filter (fun value -> value.id <> id) !entries
      | Ok call ->
          entry.state <- Running call;
          entry.owner <- Some owner;
          entry.input <- Some input;
          entry.provenance <- Some provenance);
      Mutex.unlock lock;
      started |> Result.map (fun _ -> ())

let owned owners entry =
  match entry.owner with Some owner -> List.mem owner owners | None -> false

let wakeup_fds ~owners =
  Mutex.lock lock;
  let fds =
    !entries
    |> List.filter_map (fun (entry : entry) ->
        if owned owners entry then
          match entry.state with
          | Planned -> None
          | Running call when Extension_host.closed call -> None
          | Running call -> Some (Extension_host.wakeup_fd call)
        else None)
    |> List.sort_uniq compare
  in
  Mutex.unlock lock;
  fds

let detach id =
  Mutex.lock lock;
  let found = List.find_opt (fun (entry : entry) -> entry.id = id) !entries in
  entries := List.filter (fun (entry : entry) -> entry.id <> id) !entries;
  Mutex.unlock lock;
  found

let drain ~owners =
  Mutex.lock lock;
  let candidates =
    !entries
    |> List.filter_map (fun (entry : entry) ->
        if not (owned owners entry) then None
        else
          match (entry.state, entry.input, entry.provenance) with
          | Running call, Some input, Some provenance ->
              Some (entry, call, input, provenance)
          | Planned, _, _ | Running _, _, _ -> None)
  in
  Mutex.unlock lock;
  candidates
  |> List.filter_map (fun (entry, call, input, provenance) ->
      if Extension_host.closed call then (
        ignore (detach entry.id);
        None)
      else
        match Extension_host.take call with
        | None -> None
        | Some result -> (
            match detach entry.id with
            | None -> None
            | Some entry ->
                let result = Result.bind result (entry.decode entry.request) in
                Some
                  {
                    id = entry.id;
                    provider = entry.request.provider;
                    operation = entry.request.operation;
                    snapshot = entry.snapshot;
                    input;
                    provenance;
                    result;
                  }))

let cancel ~owners =
  Mutex.lock lock;
  let removed, retained =
    List.partition (fun (entry : entry) -> owned owners entry) !entries
  in
  entries := retained;
  Mutex.unlock lock;
  List.iter
    (fun (entry : entry) ->
      match entry.state with
      | Planned -> ()
      | Running call -> Extension_host.cancel call)
    removed
