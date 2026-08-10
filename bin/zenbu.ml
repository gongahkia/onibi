open Zenbu_kernel
open Zenbu_model_api

type options = {
  model : Zenbu_app.Session.model;
  language : string option;
  file_path : string option;
  trace : bool;
  profile : bool;
}

type run_result = Exited | Unsaved_end

let usage =
  "usage: zenbu [--model vim|selection|structural] [--language ID] [--trace] \
   [--profile] [FILE]"

let parse_arguments () =
  let model = ref Zenbu_app.Session.Vim in
  let language = ref None in
  let file_path = ref None in
  let trace = ref false in
  let profile = ref false in
  let set_model = function
    | "vim" -> model := Zenbu_app.Session.Vim
    | "selection" -> model := Zenbu_app.Session.Selection
    | "structural" -> model := Zenbu_app.Session.Structural
    | value -> raise (Arg.Bad ("unknown model: " ^ value))
  in
  let set_file value =
    match !file_path with
    | None -> file_path := Some value
    | Some _ -> raise (Arg.Bad "only one file may be opened")
  in
  let specifications =
    [
      ( "--model",
        Arg.String set_model,
        "vim, selection, or structural (default: vim)" );
      ( "--language",
        Arg.String (fun value -> language := Some value),
        "syntax language ID" );
      ("--trace", Arg.Set trace, "record a bounded local execution trace");
      ("--profile", Arg.Set profile, "record bounded local CPU-time spans");
      ( "--version",
        Arg.Unit
          (fun () ->
            print_endline "zenbu M6";
            exit 0),
        "print version" );
    ]
  in
  try
    Arg.parse specifications set_file usage;
    Ok
      {
        model = !model;
        language = !language;
        file_path = !file_path;
        trace = !trace;
        profile = !profile;
      }
  with
  | Arg.Bad message -> Error message
  | Arg.Help message ->
      print_string message;
      exit 0

let load_contents = function
  | None -> Ok ""
  | Some path ->
      Zenbu_app.File_io.read path
      |> Result.map_error Zenbu_app.File_io.to_string

let create_session options contents backend =
  let columns, rows = Zenbu_terminal.Backend.size backend in
  let trace =
    if options.trace then Zenbu_model_api.Trace.enabled ~capacity:1024
    else Ok (Zenbu_model_api.Trace.disabled ())
  in
  let profiler =
    if options.profile then Zenbu_model_api.Profiler.enabled ~capacity:1024
    else Ok (Zenbu_model_api.Profiler.disabled ())
  in
  Result.bind trace (fun trace ->
      Result.bind profiler (fun profiler ->
          Zenbu_app.Session.create ~model:options.model
            ?language:options.language ?file_path:options.file_path ~contents
            ~trace ~profiler
            ~dimensions:{ Zenbu_view.Renderer.columns; rows }
            ()))

let is_control modifiers = modifiers = [ Zenbu_terminal.Event.Control ]

let rec run backend session =
  let session, frame = Zenbu_app.Session.render session in
  Zenbu_terminal.Backend.draw backend frame;
  match Zenbu_terminal.Backend.read backend with
  | Zenbu_terminal.Event.Resize { columns; rows } ->
      run backend (Zenbu_app.Session.resize session ~columns ~rows)
  | Zenbu_terminal.Event.End ->
      if Zenbu_app.Session.dirty session then Unsaved_end else Exited
  | Zenbu_terminal.Event.Unsupported description ->
      run backend (Zenbu_app.Session.notice session description)
  | Zenbu_terminal.Event.Key { key = Zenbu_terminal.Event.Text "o"; modifiers }
    when is_control modifiers ->
      run backend (Zenbu_app.Session.toggle_inspector session)
  | Zenbu_terminal.Event.Key
      { key = Zenbu_terminal.Event.Escape; modifiers = [] }
    when Zenbu_app.Session.inspector_open session ->
      run backend (Zenbu_app.Session.toggle_inspector session)
  | Zenbu_terminal.Event.Key { key = Zenbu_terminal.Event.Text "s"; modifiers }
    when is_control modifiers -> (
      match Zenbu_app.Session.handle_host session Zenbu_app.Session.Save with
      | Zenbu_app.Session.Continue session -> run backend session
      | Zenbu_app.Session.Exit _ -> Exited)
  | Zenbu_terminal.Event.Key { key = Zenbu_terminal.Event.Text "q"; modifiers }
    when is_control modifiers -> (
      match Zenbu_app.Session.handle_host session Zenbu_app.Session.Quit with
      | Zenbu_app.Session.Continue session -> run backend session
      | Zenbu_app.Session.Exit _ -> Exited)
  | event -> (
      match
        Zenbu_terminal.Input_decoder.decode
          ~input_mode:
            (Model_status.input_mode (Zenbu_app.Session.status session))
          event
      with
      | Error error ->
          run backend (Zenbu_app.Session.notice session (Error.to_string error))
      | Ok None -> run backend session
      | Ok (Some input) ->
          run backend (Zenbu_app.Session.handle_input session input))

let fail message =
  prerr_endline ("zenbu: " ^ message);
  exit 1

let () =
  match parse_arguments () with
  | Error message -> fail message
  | Ok options -> (
      match load_contents options.file_path with
      | Error message -> fail message
      | Ok contents -> (
          match
            Zenbu_terminal.Backend.with_terminal (fun backend ->
                create_session options contents backend
                |> Result.map (run backend))
          with
          | Error message -> fail message
          | Ok (Error error) -> fail (Error.to_string error)
          | Ok (Ok Exited) -> ()
          | Ok (Ok Unsaved_end) ->
              prerr_endline
                "zenbu: input ended with unsaved changes; the file was not \
                 saved";
              exit 1))
