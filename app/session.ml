open Zenbu_kernel
open Zenbu_model_api
open Zenbu_proof_models
module Vim_runtime = Model_runtime.Make (Vim_model)
module Selection_runtime = Model_runtime.Make (Selection_model)

type model = Vim | Selection
type host_command = Save | Quit | Force_quit

type outcome = Continue of t | Exit of t

and active =
  | Vim_runtime of Vim_runtime.t
  | Selection_runtime of Selection_runtime.t

and t = {
  active : active;
  file_path : string option;
  saved_version : int;
  saved_contents : string;
  viewport : Zenbu_view.Viewport.t;
  dimensions : Zenbu_view.Renderer.dimensions;
  message : string option;
  quit_armed : bool;
}

let static = function
  | Ok value -> value
  | Error error -> failwith (Error.to_string error)

let commands () =
  Command_registry.register Command_registry.empty
    Semantic_commands.apply_command

let document ~contents =
  Document.create
    ~id:(static (Document_id.of_string "terminal-buffer"))
    ~contents ()

let create ~model ?file_path ?(contents = "") ~dimensions () =
  match document ~contents with
  | Error _ as error -> error
  | Ok document -> (
      match commands () with
      | Error _ as error -> error
      | Ok commands ->
          let runtime =
            match model with
            | Vim ->
                Vim_runtime.create ~commands ~document ()
                |> Result.map (fun runtime -> Vim_runtime runtime)
            | Selection ->
                Selection_runtime.create ~commands ~document ()
                |> Result.map (fun runtime -> Selection_runtime runtime)
          in
          runtime
          |> Result.map (fun active ->
              {
                active;
                file_path;
                saved_version = 0;
                saved_contents = contents;
                viewport = Zenbu_view.Viewport.origin;
                dimensions;
                message = None;
                quit_armed = false;
              }))

let context = function
  | { active = Vim_runtime runtime; _ } -> Vim_runtime.context runtime
  | { active = Selection_runtime runtime; _ } ->
      Selection_runtime.context runtime

let status = function
  | { active = Vim_runtime runtime; _ } -> Vim_runtime.status runtime
  | { active = Selection_runtime runtime; _ } ->
      Selection_runtime.status runtime

let filename session =
  match session.file_path with
  | None -> "[No Name]"
  | Some path -> Filename.basename path

let dirty session =
  let context = context session in
  Editor_context.document_version context <> session.saved_version
  && not (String.equal (Editor_context.contents context) session.saved_contents)

let last_message messages =
  match List.rev messages with
  | [] -> None
  | message :: _ -> Some message.Model_effect.text

let handle_input session input =
  let next =
    match session.active with
    | Vim_runtime runtime -> (
        match Vim_runtime.handle_input runtime input with
        | Error error ->
            {
              session with
              message = Some (Error.to_string error);
              quit_armed = false;
            }
        | Ok (runtime, step) ->
            {
              session with
              active = Vim_runtime runtime;
              message = last_message (Vim_runtime.messages step);
              quit_armed = false;
            })
    | Selection_runtime runtime -> (
        match Selection_runtime.handle_input runtime input with
        | Error error ->
            {
              session with
              message = Some (Error.to_string error);
              quit_armed = false;
            }
        | Ok (runtime, step) ->
            {
              session with
              active = Selection_runtime runtime;
              message = last_message (Selection_runtime.messages step);
              quit_armed = false;
            })
  in
  next

let save session =
  match session.file_path with
  | None ->
      {
        session with
        message = Some "save-as is not implemented for unnamed buffers";
        quit_armed = false;
      }
  | Some path -> (
      match
        File_io.save_atomic ~path
          ~contents:(Editor_context.contents (context session))
      with
      | Ok () ->
          {
            session with
            saved_version = Editor_context.document_version (context session);
            saved_contents = Editor_context.contents (context session);
            message = Some ("saved " ^ path);
            quit_armed = false;
          }
      | Error error ->
          {
            session with
            message = Some (File_io.to_string error);
            quit_armed = false;
          })

let handle_host session = function
  | Save -> Continue (save session)
  | Force_quit -> Exit session
  | Quit when not (dirty session) -> Exit session
  | Quit when session.quit_armed -> Exit session
  | Quit ->
      Continue
        {
          session with
          message = Some "unsaved changes: press Ctrl-Q again to force quit";
          quit_armed = true;
        }

let resize session ~columns ~rows =
  {
    session with
    dimensions =
      { Zenbu_view.Renderer.columns = max 0 columns; rows = max 0 rows };
  }

let render session =
  let rendered =
    Zenbu_view.Renderer.render ~context:(context session)
      ~status:(status session) ~filename:(filename session)
      ~dirty:(dirty session) ~message:session.message ~viewport:session.viewport
      ~dimensions:session.dimensions
  in
  ({ session with viewport = rendered.viewport }, rendered.frame)

let contents session = Editor_context.contents (context session)
let file_path session = session.file_path
let dimensions session = session.dimensions

let notice session message =
  { session with message = Some message; quit_armed = false }
