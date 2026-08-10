module Language = Zenbu_language.Language
module Lsp = Zenbu_lsp.Client

let fail message =
  prerr_endline ("M11 ocamllsp acceptance failure: " ^ message);
  exit 1

let must = function Ok value -> value | Error error -> fail error

let wait_for client predicate =
  let deadline = Unix.gettimeofday () +. 10. in
  let rec loop events =
    let events = events @ Lsp.drain client in
    if predicate events then events
    else if Unix.gettimeofday () >= deadline then
      let status = Lsp.status client in
      fail
        ("timed out; state="
        ^ Language.server_state_name status.state
        ^ " error="
        ^ Option.value ~default:"none" status.last_error)
    else (
      ignore (Unix.select [ Lsp.wakeup_fd client ] [] [] 0.05);
      loop events)
  in
  loop []

let config =
  Language.Server_config.create ~id:"ocamllsp" ~language_ids:[ "ocaml" ]
    ~extensions:[ ".ml"; ".mli" ] ~executable:"ocamllsp"
    ~root_markers:[ "dune-project"; ".git" ] ()
  |> must

let () =
  let fixture = "fixtures/lsp_ocaml/sample.ml" in
  let path =
    let local = Filename.concat (Sys.getcwd ()) fixture in
    if Sys.file_exists local then local
    else Filename.concat (Sys.getcwd ()) ("test/" ^ fixture)
  in
  let contents =
    let channel = open_in_bin path in
    Fun.protect
      ~finally:(fun () -> close_in_noerr channel)
      (fun () -> really_input_string channel (in_channel_length channel))
  in
  let client =
    Lsp.start ~config ~document_id:"ocamllsp-acceptance" ~document_version:0
      ~file_path:path ~contents
      ~trace:(Zenbu_model_api.Trace.disabled ())
      ~profiler:(Zenbu_model_api.Profiler.disabled ())
  in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      ignore
        (wait_for client
           (List.exists (function Lsp.Initialized -> true | _ -> false)));
      if (Lsp.status client).state <> Language.Ready then
        fail "ocamllsp did not negotiate a ready session";
      ignore (Lsp.request_hover client ~byte_offset:4 |> must);
      ignore
        (wait_for client
           (List.exists (function Lsp.Hover_result _ -> true | _ -> false)));
      print_endline "M11 ocamllsp acceptance passed")
