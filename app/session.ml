open Zenbu_kernel
open Zenbu_model_api
open Zenbu_syntax
open Zenbu_proof_models
open Zenbu_structural_model
module Vim_runtime = Model_runtime.Make (Vim_model)
module Selection_runtime = Model_runtime.Make (Selection_model)
module Structural_runtime = Model_runtime.Make (Structural_model)

type model = Vim | Selection | Structural
type host_command = Save | Quit | Force_quit
type inspection = Why | Bindings | Commands | History | Selection_view | Syntax | Profile | Api

type outcome = Continue of t | Exit of t

and active =
  | Vim_runtime of Vim_runtime.t
  | Selection_runtime of Selection_runtime.t
  | Structural_runtime of Structural_runtime.t

and t = {
  active : active;
  file_path : string option;
  saved_version : int;
  saved_contents : string;
  viewport : Zenbu_view.Viewport.t;
  dimensions : Zenbu_view.Renderer.dimensions;
  message : string option;
  quit_armed : bool;
  inspector : string list option;
}

let static = function
  | Ok value -> value
  | Error error -> failwith (Error.to_string error)

let commands () =
  match
    Command_registry.register Command_registry.empty
      Semantic_commands.apply_command
  with
  | Error _ as error -> error
  | Ok registry ->
      List.fold_left
        (fun registry command ->
          match registry with
          | Error _ -> registry
          | Ok registry -> Command_registry.register registry command)
        (Ok registry)
        (Syntax_commands.commands ())

let document ~contents =
  Document.create
    ~id:(static (Document_id.of_string "terminal-buffer"))
    ~contents ()

let syntax_service ?language file_path =
  match language with
  | Some id -> (
      match Syntax.Language.find id with
      | Some language -> Ok (Some (Syntax.Service.create language))
      | None ->
          Error (Error.Invalid_command_arguments ("unknown language: " ^ id)))
  | None -> (
      match Option.bind file_path Syntax.Language.detect_path with
      | Some language -> Ok (Some (Syntax.Service.create language))
      | None -> Ok None)

let create ~model ?language ?file_path ?(contents = "") ?trace ?profiler
    ~dimensions () =
  match document ~contents with
  | Error _ as error -> error
  | Ok document -> (
      match syntax_service ?language file_path with
      | Error _ as error -> error
      | Ok syntax_service -> (
          match commands () with
          | Error _ as error -> error
          | Ok commands ->
              let runtime =
                match model with
                | Vim ->
                    Vim_runtime.create ~commands ?syntax_service ?trace ?profiler
                      ~document ()
                    |> Result.map (fun runtime -> Vim_runtime runtime)
                | Selection ->
                    Selection_runtime.create ~commands ?syntax_service ?trace
                      ?profiler ~document ()
                    |> Result.map (fun runtime -> Selection_runtime runtime)
                | Structural ->
                    Structural_runtime.create ~commands ?syntax_service ?trace
                      ?profiler ~document ()
                    |> Result.map (fun runtime -> Structural_runtime runtime)
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
                    inspector = None;
                  })))

let context = function
  | { active = Vim_runtime runtime; _ } -> Vim_runtime.context runtime
  | { active = Selection_runtime runtime; _ } ->
      Selection_runtime.context runtime
  | { active = Structural_runtime runtime; _ } ->
      Structural_runtime.context runtime

let status = function
  | { active = Vim_runtime runtime; _ } -> Vim_runtime.status runtime
  | { active = Selection_runtime runtime; _ } ->
      Selection_runtime.status runtime
  | { active = Structural_runtime runtime; _ } ->
      Structural_runtime.status runtime

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
              inspector = None;
            }
        | Ok (runtime, step) ->
            {
              session with
              active = Vim_runtime runtime;
              message = last_message (Vim_runtime.messages step);
              quit_armed = false;
              inspector = None;
            })
    | Selection_runtime runtime -> (
        match Selection_runtime.handle_input runtime input with
        | Error error ->
            {
              session with
              message = Some (Error.to_string error);
              quit_armed = false;
              inspector = None;
            }
        | Ok (runtime, step) ->
            {
              session with
              active = Selection_runtime runtime;
              message = last_message (Selection_runtime.messages step);
              quit_armed = false;
              inspector = None;
            })
    | Structural_runtime runtime -> (
        match Structural_runtime.handle_input runtime input with
        | Error error ->
            {
              session with
              message = Some (Error.to_string error);
              quit_armed = false;
              inspector = None;
            }
        | Ok (runtime, step) ->
            {
              session with
              active = Structural_runtime runtime;
              message = last_message (Structural_runtime.messages step);
              quit_armed = false;
              inspector = None;
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
    Zenbu_view.Renderer.render_with_inspector ~context:(context session)
      ~status:(status session) ~filename:(filename session)
      ~dirty:(dirty session) ~message:session.message ~viewport:session.viewport
      ~dimensions:session.dimensions ~inspector:session.inspector
  in
  ({ session with viewport = rendered.viewport }, rendered.frame)

let contents session = Editor_context.contents (context session)
let file_path session = session.file_path
let dimensions session = session.dimensions

let notice session message =
  { session with message = Some message; quit_armed = false; inspector = None }

let all_models = [ Vim_model.descriptor; Selection_model.descriptor; Structural_model.descriptor ]

let inspect session inspection =
  let format ~last_execution ~trace ~model_descriptor ~model_status ~rules
      ~command_registry ~runtime_history ~runtime_context ~profiler =
    match inspection with
    | Why -> (
        match last_execution with
        | None -> [ "Why"; "no completed input execution" ]
        | Some execution_id -> (
            match Inspector.why trace ~execution_id with
            | None -> [ "Why"; "trace is disabled; restart with --trace" ]
            | Some why -> "Why" :: Inspector.format_why why))
    | Bindings ->
        "Bindings"
        :: Inspector.format_bindings model_descriptor model_status rules
    | Commands -> "Commands" :: Inspector.format_commands (Inspector.commands command_registry)
    | History ->
        "History"
        :: Inspector.format_history
             (Inspector.history ~saved_version:session.saved_version
                runtime_history)
    | Selection_view ->
        "Selection" :: Inspector.format_selection (Inspector.selections runtime_context)
    | Syntax -> (
        match Inspector.syntax runtime_context with
        | None -> [ "Syntax"; "syntax is unavailable" ]
        | Some syntax -> "Syntax" :: Inspector.format_syntax syntax)
    | Profile -> "Profile" :: Inspector.format_profile profiler
    | Api ->
        "API"
        :: Inspector.format_api
             (Inspector.api ~models:all_models ~commands:command_registry)
  in
  match session.active with
  | Vim_runtime runtime ->
      format ~last_execution:(Vim_runtime.last_execution runtime)
        ~trace:(Vim_runtime.trace runtime)
        ~model_descriptor:(Vim_runtime.model_descriptor runtime)
        ~model_status:(Vim_runtime.status runtime)
        ~rules:(Vim_runtime.input_rules runtime)
        ~command_registry:(Vim_runtime.commands runtime)
        ~runtime_history:(Vim_runtime.history runtime)
        ~runtime_context:(Vim_runtime.context runtime)
        ~profiler:(Vim_runtime.profiler runtime)
  | Selection_runtime runtime ->
      format ~last_execution:(Selection_runtime.last_execution runtime)
        ~trace:(Selection_runtime.trace runtime)
        ~model_descriptor:(Selection_runtime.model_descriptor runtime)
        ~model_status:(Selection_runtime.status runtime)
        ~rules:(Selection_runtime.input_rules runtime)
        ~command_registry:(Selection_runtime.commands runtime)
        ~runtime_history:(Selection_runtime.history runtime)
        ~runtime_context:(Selection_runtime.context runtime)
        ~profiler:(Selection_runtime.profiler runtime)
  | Structural_runtime runtime ->
      format ~last_execution:(Structural_runtime.last_execution runtime)
        ~trace:(Structural_runtime.trace runtime)
        ~model_descriptor:(Structural_runtime.model_descriptor runtime)
        ~model_status:(Structural_runtime.status runtime)
        ~rules:(Structural_runtime.input_rules runtime)
        ~command_registry:(Structural_runtime.commands runtime)
        ~runtime_history:(Structural_runtime.history runtime)
        ~runtime_context:(Structural_runtime.context runtime)
        ~profiler:(Structural_runtime.profiler runtime)

let toggle_inspector session =
  match session.inspector with
  | Some _ -> { session with inspector = None }
  | None -> { session with inspector = Some (inspect session Why) }

let inspector_open session = Option.is_some session.inspector
