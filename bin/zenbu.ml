open Zenbu_kernel
open Zenbu_model_api

type options = { model : Zenbu_app.Session.model; file_path : string option }
type run_result = Exited | Unsaved_end

let usage = "usage: zenbu [--model vim|selection] [FILE]"

let parse_arguments () =
  let model = ref Zenbu_app.Session.Vim in
  let file_path = ref None in
  let set_model = function
    | "vim" -> model := Zenbu_app.Session.Vim
    | "selection" -> model := Zenbu_app.Session.Selection
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
        "vim or selection (default: vim)" );
      ("--version", Arg.Unit (fun () -> print_endline "zenbu M4"; exit 0), "print version");
    ]
  in
  try
    Arg.parse specifications set_file usage;
    Ok { model = !model; file_path = !file_path }
  with
  | Arg.Bad message -> Error message
  | Arg.Help message ->
      print_string message;
      exit 0

let load_contents = function
  | None -> Ok ""
  | Some path -> Zenbu_app.File_io.read path |> Result.map_error Zenbu_app.File_io.to_string

let create_session options contents backend =
  let columns, rows = Zenbu_terminal.Backend.size backend in
  Zenbu_app.Session.create ~model:options.model ?file_path:options.file_path
    ~contents ~dimensions:{ Zenbu_view.Renderer.columns; rows } ()

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
  | Zenbu_terminal.Event.Key
      { key = Zenbu_terminal.Event.Text "s"; modifiers }
    when is_control modifiers -> (
      match Zenbu_app.Session.handle_host session Zenbu_app.Session.Save with
      | Zenbu_app.Session.Continue session -> run backend session
      | Zenbu_app.Session.Exit _ -> Exited)
  | Zenbu_terminal.Event.Key
      { key = Zenbu_terminal.Event.Text "q"; modifiers }
    when is_control modifiers -> (
      match Zenbu_app.Session.handle_host session Zenbu_app.Session.Quit with
      | Zenbu_app.Session.Continue session -> run backend session
      | Zenbu_app.Session.Exit _ -> Exited)
  | event -> (
      match
        Zenbu_terminal.Input_decoder.decode
          ~input_mode:(Model_status.input_mode (Zenbu_app.Session.status session))
          event
      with
      | Error error ->
          run backend
            (Zenbu_app.Session.notice session (Error.to_string error))
      | Ok None -> run backend session
      | Ok (Some input) -> run backend (Zenbu_app.Session.handle_input session input))

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
                match create_session options contents backend with
                | Ok session -> run backend session
                | Error error -> fail (Error.to_string error))
          with
          | Error message -> fail message
          | Ok Exited -> ()
          | Ok Unsaved_end ->
              prerr_endline
                "zenbu: input ended with unsaved changes; the file was not saved";
              exit 1))
