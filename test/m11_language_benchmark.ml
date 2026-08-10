module Language = Zenbu_language.Language
module Lsp = Zenbu_lsp.Client

let fail reason =
  prerr_endline ("M11 language benchmark failed: " ^ reason);
  exit 1

let must = function Ok value -> value | Error error -> fail error

let fake_server () =
  Filename.concat (Filename.dirname Sys.executable_name) "fake_lsp_server.exe"

let wait_for client predicate =
  let deadline = Unix.gettimeofday () +. 3. in
  let rec loop events =
    let events = events @ Lsp.drain client in
    if predicate events then ()
    else if Unix.gettimeofday () >= deadline then
      fail "timed out waiting for fake server"
    else (
      ignore (Unix.select [ Lsp.wakeup_fd client ] [] [] 0.05);
      loop events)
  in
  loop []

let report name seconds = Printf.printf "%-30s %.3f ms\n" name (seconds *. 1000.)

let () =
  let config =
    Language.Server_config.create ~id:"benchmark.fake" ~language_ids:[ "fake" ]
      ~extensions:[ ".fake" ] ~executable:(fake_server ()) ()
    |> must
  in
  let started = Unix.gettimeofday () in
  let client =
    Lsp.start ~config ~document_id:"m11-benchmark" ~document_version:0
      ~file_path:(Filename.concat (Sys.getcwd ()) "benchmark.fake")
      ~contents:"alpha"
      ~trace:(Zenbu_model_api.Trace.disabled ())
      ~profiler:(Zenbu_model_api.Profiler.disabled ())
  in
  Fun.protect
    ~finally:(fun () -> Lsp.close client)
    (fun () ->
      wait_for client
        (List.exists (function Lsp.Initialized -> true | _ -> false));
      let ready = Unix.gettimeofday () in
      ignore (Lsp.request_hover client ~byte_offset:0 |> must);
      wait_for client
        (List.exists (function Lsp.Hover_result _ -> true | _ -> false));
      let hover = Unix.gettimeofday () in
      print_endline
        "Zenbu M11 fake language-server benchmark (single process; lower is \
         better)";
      report "fake server initialize + didOpen" (ready -. started);
      report "fake server hover round trip" (hover -. ready))
