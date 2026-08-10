open Zenbu_kernel
open Zenbu_model_api
module Backend = Lua_backend

type event = Document_changed | After_save

type scope =
  | Global
  | Model of string
  | Model_status of { model : string; status : string }

type binding = {
  input : Input_event.t;
  command : string;
  scope : scope;
  provider : Provider.t;
}

type hook = {
  event : event;
  run : Editor_context.t -> (Model_effect.t list, Error.t) result;
}

type t = {
  generation_id : int;
  source : string;
  provider : Provider.t;
  backend : Backend.t;
  commands : Command.t list;
  semantic_behaviors : Semantic_behavior_registry.t;
  bindings : binding list;
  hooks : hook list;
  descriptors : Semantic_descriptor.t list;
}

type config = Default | Explicit of string | Disabled

let api_version = 1

let script_error phase source message =
  Error.Script_error { phase; source = Some source; line = None; message }

let default_path () =
  let root =
    match Sys.getenv_opt "XDG_CONFIG_HOME" with
    | Some path when String.length path > 0 -> path
    | None | Some _ -> (
        match Sys.getenv_opt "HOME" with
        | Some path when String.length path > 0 ->
            Filename.concat path ".config"
        | None | Some _ -> ".config")
  in
  Filename.concat root "zenbu/init.lua"

let read_file path =
  try
    let channel = open_in_bin path in
    Fun.protect
      ~finally:(fun () -> close_in_noerr channel)
      (fun () -> Ok (really_input_string channel (in_channel_length channel)))
  with Sys_error message -> Error (script_error "load" path message)

let field value name = Extension_value.find value name

let required_text source field_name value =
  match field value field_name with
  | Some (Extension_value.Text text) when String.length text > 0 -> Ok text
  | _ ->
      Error
        (script_error "action" source ("missing string field " ^ field_name))

let optional_value value field_name =
  Option.value ~default:Extension_value.Nil (field value field_name)

let selector_of_id = function
  | "current-selections" -> Some Model_intent.Current_selections
  | "document" -> Some Model_intent.Document
  | "next-text-unit" -> Some Model_intent.Next_text_unit
  | "previous-text-unit" -> Some Model_intent.Previous_text_unit
  | "next-word" -> Some Model_intent.Next_word
  | "previous-word" -> Some Model_intent.Previous_word
  | "word-end" -> Some Model_intent.Word_end
  | "current-word" -> Some Model_intent.Current_word
  | "around-word" -> Some Model_intent.Around_word
  | "current-line" -> Some Model_intent.Current_line
  | "line-start" -> Some Model_intent.Line_start
  | "line-end" -> Some Model_intent.Line_end
  | "first-nonblank" -> Some Model_intent.First_nonblank
  | "document-start" -> Some Model_intent.Document_start
  | "document-end" -> Some Model_intent.Document_end
  | "next-line" -> Some Model_intent.Next_line
  | "previous-line" -> Some Model_intent.Previous_line
  | "all-occurrences" -> Some Model_intent.All_occurrences
  | _ -> None

let transformation_of_id = function
  | "select" -> Some Model_intent.Select
  | "delete" -> Some Model_intent.Delete
  | "collapse-to-start" -> Some Model_intent.Collapse_to_start
  | "collapse-to-end" -> Some Model_intent.Collapse_to_end
  | _ -> None

let semantic_operation source value =
  match
    ( required_text source "selector" value,
      required_text source "transformation" value )
  with
  | Ok selector_id, Ok transformation_id ->
      let arguments = optional_value value "args" in
      let selector =
        match selector_of_id selector_id with
        | Some selector -> Semantic_operation.Builtin_selector selector
        | None ->
            Semantic_operation.Registered_selector
              { id = selector_id; arguments }
      in
      let transformation =
        match transformation_of_id transformation_id with
        | Some transformation ->
            Semantic_operation.Builtin_transformation transformation
        | None ->
            Semantic_operation.Registered_transformation
              { id = transformation_id; arguments }
      in
      Ok Semantic_operation.{ selector; transformation }
  | Error error, _ | _, Error error -> Error error

let selection_action source value =
  match (field value "selections", field value "primary") with
  | ( Some (Extension_value.List selections),
      Some (Extension_value.Integer primary) ) ->
      let selection = function
        | Extension_value.Record fields -> (
            match
              (List.assoc_opt "anchor" fields, List.assoc_opt "head" fields)
            with
            | ( Some (Extension_value.Integer anchor),
                Some (Extension_value.Integer head) ) ->
                Ok (anchor, head)
            | _ ->
                Error
                  (script_error "action" source
                     "selection entries require integer anchor and head fields")
            )
        | _ ->
            Error
              (script_error "action" source "selection entry must be a table")
      in
      let rec collect values = function
        | [] -> Ok (List.rev values)
        | value :: rest -> (
            match selection value with
            | Error _ as error -> error
            | Ok value -> collect (value :: values) rest)
      in
      Result.bind (collect [] selections) (fun selections ->
          Model_intent.set_selections ~selections ~primary:(primary - 1))
      |> Result.map (fun intent -> Model_effect.Execute_intent intent)
  | _ ->
      Error
        (script_error "action" source
           "set-selections requires selections and primary fields")

let action source value =
  match required_text source "kind" value with
  | Error _ as error -> error
  | Ok "message" ->
      Result.bind (required_text source "text" value) (fun text ->
          Model_effect.message ~level:Model_effect.Info ~text)
  | Ok "insert" ->
      required_text source "text" value
      |> Result.map (fun text ->
          Model_effect.Execute_intent (Model_intent.insert_text text))
  | Ok "delete" ->
      Ok (Model_effect.Execute_intent Model_intent.delete_selected_ranges)
  | Ok "replace" ->
      required_text source "text" value
      |> Result.map (fun text ->
          Model_effect.Execute_intent
            (Model_intent.replace_selected_ranges text))
  | Ok "set-selections" -> selection_action source value
  | Ok "apply" ->
      semantic_operation source value
      |> Result.map Model_effect.execute_semantic_operation
  | Ok "command" ->
      Result.bind (required_text source "id" value) (fun id ->
          Result.bind (Command_id.of_string id) (fun id ->
              Command_invocation.create ~id ~arguments:[]))
      |> Result.map (fun invocation -> Model_effect.Invoke_command invocation)
  | Ok kind ->
      Error (script_error "action" source ("unknown action kind " ^ kind))

let actions source = function
  | Extension_value.Nil -> Ok []
  | Extension_value.List values ->
      let rec collect results = function
        | [] -> Ok (List.rev results)
        | value :: rest -> (
            match action source value with
            | Error _ as error -> error
            | Ok action -> collect (action :: results) rest)
      in
      collect [] values
  | value -> action source value |> Result.map (fun action -> [ action ])

let behavior_selection source = function
  | Extension_value.Record fields -> (
      match
        (List.assoc_opt "selections" fields, List.assoc_opt "primary" fields)
      with
      | ( Some (Extension_value.List values),
          Some (Extension_value.Integer primary) ) ->
          let entry = function
            | Extension_value.Record fields -> (
                match
                  (List.assoc_opt "anchor" fields, List.assoc_opt "head" fields)
                with
                | ( Some (Extension_value.Integer anchor),
                    Some (Extension_value.Integer head) ) ->
                    Ok
                      Semantic_behavior.
                        { anchor_offset = anchor; head_offset = head }
                | _ ->
                    Error
                      (script_error "selector" source
                         "selection requires integer anchor and head fields"))
            | _ ->
                Error
                  (script_error "selector" source "selection must be a table")
          in
          let rec collect values = function
            | [] -> Ok (List.rev values)
            | value :: rest -> (
                match entry value with
                | Error _ as error -> error
                | Ok value -> collect (value :: values) rest)
          in
          collect [] values
          |> Result.map (fun selections ->
              Semantic_behavior.{ selections; primary = primary - 1 })
      | _ ->
          Error
            (script_error "selector" source
               "selector result requires selections and primary fields"))
  | _ ->
      Error (script_error "selector" source "selector result must be a table")

let behavior_transformation source = function
  | Extension_value.Record fields -> (
      match List.assoc_opt "edits" fields with
      | Some (Extension_value.List values) ->
          let edit = function
            | Extension_value.Record fields -> (
                match
                  ( List.assoc_opt "start" fields,
                    List.assoc_opt "stop" fields,
                    List.assoc_opt "text" fields )
                with
                | ( Some (Extension_value.Integer start_offset),
                    Some (Extension_value.Integer stop_offset),
                    Some (Extension_value.Text replacement) ) ->
                    Ok
                      Semantic_behavior.
                        { start_offset; stop_offset; replacement }
                | _ ->
                    Error
                      (script_error "transformation" source
                         "edits require integer start/stop and string text \
                          fields"))
            | _ ->
                Error
                  (script_error "transformation" source "edit must be a table")
          in
          let rec collect values = function
            | [] -> Ok (List.rev values)
            | value :: rest -> (
                match edit value with
                | Error _ as error -> error
                | Ok value -> collect (value :: values) rest)
          in
          collect [] values
          |> Result.map (fun edits ->
              Semantic_behavior.{ edits; selections = None })
      | _ ->
          Error
            (script_error "transformation" source
               "transformation result requires an edits list"))
  | _ ->
      Error
        (script_error "transformation" source
           "transformation result must be a table")

let input_of_string source value =
  let control_prefix = "Ctrl-" in
  let named =
    [
      ("Escape", Input_event.Escape);
      ("Enter", Input_event.Enter);
      ("Backspace", Input_event.Backspace);
      ("Tab", Input_event.Tab);
      ("ArrowUp", Input_event.Arrow_up);
      ("ArrowDown", Input_event.Arrow_down);
      ("ArrowLeft", Input_event.Arrow_left);
      ("ArrowRight", Input_event.Arrow_right);
    ]
  in
  match List.assoc_opt value named with
  | Some named -> Ok (Input_event.key_press (Input_event.named_key named))
  | None ->
      let modifiers, text =
        if String.starts_with ~prefix:control_prefix value then
          ( [ Input_event.Control ],
            String.sub value
              (String.length control_prefix)
              (String.length value - String.length control_prefix)
            |> String.lowercase_ascii )
        else ([], value)
      in
      Input_event.logical_text text
      |> Result.map (Input_event.key_press ~modifiers)
      |> Result.map_error (fun error ->
          script_error "registration" source (Error.to_string error))

let scope_of_string source = function
  | None | Some "global" -> Ok Global
  | Some value -> (
      match String.split_on_char ':' value with
      | [ "model"; model ] when String.length model > 0 -> Ok (Model model)
      | [ "model"; model; status ]
        when String.length model > 0 && String.length status > 0 ->
          Ok (Model_status { model; status })
      | _ ->
          Error
            (script_error "registration" source
               "scope must be global, model:<id>, or model:<id>:<status>"))

let reserved_host_input input =
  [ "s"; "q" ]
  |> List.exists (fun text ->
      Input_event.logical_text text
      |> Result.map (Input_event.key_press ~modifiers:[ Input_event.Control ])
      |> Result.map (fun reserved ->
          String.equal
            (String.lowercase_ascii (Input_event.to_string input))
            (String.lowercase_ascii (Input_event.to_string reserved)))
      |> Result.value ~default:false)

let event_of_string source = function
  | "document-changed" -> Ok Document_changed
  | "after-save" -> Ok After_save
  | value ->
      Error (script_error "registration" source ("unknown event " ^ value))

let semantic_descriptor provider kind descriptor =
  Semantic_descriptor.create ~id:descriptor.Backend.id ~title:descriptor.title
    ~description:descriptor.description ~provider ~kind
    ~requires_syntax:descriptor.requires_syntax ()

let validate_id source id =
  Command_id.of_string id
  |> Result.map_error (fun error ->
      script_error "registration" source (Error.to_string error))

let load_from_source ~generation_id ~base_commands ~base_semantics ~source text
    =
  let provider =
    Provider.create_with_source
      ~id:("script." ^ string_of_int generation_id)
      ~kind:Provider.Script ~source
    |> Result.get_ok
  in
  match Backend.create ~source with
  | Error _ as error -> error
  | Ok backend -> (
      match Backend.evaluate backend text with
      | Error error ->
          Backend.dispose backend;
          Error error
      | Ok () -> (
          let registrations = Backend.registrations backend in
          let command_registry = ref base_commands in
          let behavior_registry = ref Semantic_behavior_registry.empty in
          let descriptors = ref [] in
          let script_commands = ref [] in
          let bindings = ref [] in
          let hooks = ref [] in
          let failed = ref None in
          let fail error =
            if Option.is_none !failed then failed := Some error
          in
          let descriptor_taken id =
            List.exists
              (fun descriptor -> Semantic_descriptor.id descriptor = id)
              base_semantics
            || List.exists
                 (fun descriptor -> Semantic_descriptor.id descriptor = id)
                 !descriptors
          in
          let register_descriptor descriptor =
            let id = Semantic_descriptor.id descriptor in
            if descriptor_taken id then fail (Error.Duplicate_descriptor id)
            else descriptors := !descriptors @ [ descriptor ]
          in
          List.iter
            (function
              | Backend.Command (definition, callback)
                when Option.is_none !failed -> (
                  match validate_id source definition.id with
                  | Error error -> fail error
                  | Ok id -> (
                      match
                        Command_descriptor.create ~id ~title:definition.title
                          ~description:definition.description ~provider ()
                      with
                      | Error error -> fail error
                      | Ok descriptor -> (
                          let command =
                            Command.create_effectful ~descriptor
                              ~effect_handler:(fun context _ ->
                                Result.bind
                                  (Backend.call backend callback ~context
                                     ~arguments:Extension_value.Nil)
                                  (actions source))
                          in
                          match
                            Command_registry.register !command_registry command
                          with
                          | Error error -> fail error
                          | Ok registry ->
                              command_registry := registry;
                              script_commands := !script_commands @ [ command ])
                      ))
              | Backend.Selector (definition, callback)
                when Option.is_none !failed -> (
                  match validate_id source definition.id with
                  | Error error -> fail error
                  | Ok _ -> (
                      match
                        semantic_descriptor provider
                          Semantic_descriptor.Selector definition
                      with
                      | Error error -> fail error
                      | Ok descriptor -> (
                          register_descriptor descriptor;
                          if Option.is_none !failed then
                            let entry =
                              Semantic_behavior.selector_entry ~descriptor
                                ~run:(fun context ~arguments ->
                                  Result.bind
                                    (Backend.call backend callback ~context
                                       ~arguments)
                                    (behavior_selection source))
                            in
                            match
                              Semantic_behavior_registry.register_selector
                                !behavior_registry entry
                            with
                            | Error error -> fail error
                            | Ok registry -> behavior_registry := registry)))
              | Backend.Transformation (definition, callback)
                when Option.is_none !failed -> (
                  match validate_id source definition.id with
                  | Error error -> fail error
                  | Ok _ -> (
                      match
                        semantic_descriptor provider
                          Semantic_descriptor.Transformation definition
                      with
                      | Error error -> fail error
                      | Ok descriptor -> (
                          register_descriptor descriptor;
                          if Option.is_none !failed then
                            let entry =
                              Semantic_behavior.transformation_entry ~descriptor
                                ~run:(fun context ~selections ~arguments ->
                                  let selection_set =
                                    Extension_value.List
                                      (List.map
                                         (fun selection ->
                                           Extension_value.Record
                                             [
                                               ( "anchor",
                                                 Extension_value.Integer
                                                   selection
                                                     .Semantic_behavior
                                                      .anchor_offset );
                                               ( "head",
                                                 Extension_value.Integer
                                                   selection.head_offset );
                                             ])
                                         selections.Semantic_behavior.selections)
                                  in
                                  Result.bind
                                    (Backend.call backend callback ~context
                                       ~arguments:
                                         (Extension_value.Record
                                            [
                                              ("selection_set", selection_set);
                                              ("arguments", arguments);
                                            ]))
                                    (behavior_transformation source))
                            in
                            match
                              Semantic_behavior_registry.register_transformation
                                !behavior_registry entry
                            with
                            | Error error -> fail error
                            | Ok registry -> behavior_registry := registry)))
              | Backend.Binding _ | Backend.Hook _ | Backend.Command _
              | Backend.Selector _ | Backend.Transformation _ ->
                  ())
            registrations;
          List.iter
            (function
              | Backend.Binding definition when Option.is_none !failed -> (
                  match
                    ( input_of_string source definition.input,
                      scope_of_string source definition.scope,
                      Command_id.of_string definition.command )
                  with
                  | Ok input, Ok scope, Ok command -> (
                      if reserved_host_input input then
                        fail
                          (script_error "registration" source
                             "Ctrl-S and Ctrl-Q are reserved host controls")
                      else
                        match
                          ( String.equal definition.command "config.reload",
                            Command_registry.find !command_registry command )
                        with
                        | true, _ | false, Ok _ ->
                            let duplicate =
                              List.exists
                                (fun binding ->
                                  Input_event.to_string binding.input
                                  = Input_event.to_string input
                                  && binding.scope = scope)
                                !bindings
                            in
                            if duplicate then
                              fail
                                (script_error "registration" source
                                   ("duplicate binding for "
                                   ^ Input_event.to_string input))
                            else
                              bindings :=
                                !bindings
                                @ [
                                    {
                                      input;
                                      command = definition.command;
                                      scope;
                                      provider;
                                    };
                                  ]
                        | false, Error error -> fail error)
                  | Error error, _, _ | _, Error error, _ | _, _, Error error ->
                      fail error)
              | Backend.Hook definition when Option.is_none !failed -> (
                  match event_of_string source definition.event with
                  | Error error -> fail error
                  | Ok event ->
                      hooks :=
                        !hooks
                        @ [
                            {
                              event;
                              run =
                                (fun context ->
                                  Result.bind
                                    (Backend.call backend definition.callback
                                       ~context
                                       ~arguments:
                                         (Extension_value.Record
                                            [
                                              ( "event",
                                                Extension_value.Text
                                                  definition.event );
                                            ]))
                                    (actions source));
                            };
                          ])
              | Backend.Binding _ | Backend.Hook _ | Backend.Command _
              | Backend.Selector _ | Backend.Transformation _ ->
                  ())
            registrations;
          match !failed with
          | Some error ->
              Backend.dispose backend;
              Error error
          | None ->
              Ok
                {
                  generation_id;
                  source;
                  provider;
                  backend;
                  commands = !script_commands;
                  semantic_behaviors = !behavior_registry;
                  bindings = !bindings;
                  hooks = !hooks;
                  descriptors = !descriptors;
                }))

let generation_id value = value.generation_id
let source value = value.source
let provider value = value.provider
let commands value = value.commands
let semantic_behaviors value = value.semantic_behaviors
let descriptors value = value.descriptors
let bindings value = value.bindings
let hooks value = value.hooks

let counts value =
  let selectors =
    List.filter
      (fun descriptor ->
        Semantic_descriptor.kind descriptor = Semantic_descriptor.Selector)
      value.descriptors
    |> List.length
  in
  ( List.length value.commands,
    selectors,
    List.length value.descriptors - selectors,
    List.length value.bindings,
    List.length value.hooks )

let dispose value = Backend.dispose value.backend
let binding_input (value : binding) = value.input
let binding_command (value : binding) = value.command
let binding_scope (value : binding) = value.scope
let binding_provider (value : binding) = value.provider
let hook_event (value : hook) = value.event
let run_hook (value : hook) = value.run

let load ~generation_id ~base_commands ~base_semantics = function
  | Disabled -> Ok None
  | Default ->
      let path = default_path () in
      if Sys.file_exists path then
        Result.bind (read_file path)
          (load_from_source ~generation_id ~base_commands ~base_semantics
             ~source:path)
        |> Result.map (fun value -> Some value)
      else Ok None
  | Explicit path ->
      Result.bind (read_file path)
        (load_from_source ~generation_id ~base_commands ~base_semantics
           ~source:path)
      |> Result.map (fun value -> Some value)

let check_file ~base_commands ~base_semantics path =
  Result.bind (read_file path)
    (load_from_source ~generation_id:0 ~base_commands ~base_semantics
       ~source:path)
  |> Result.map (fun generation ->
      let result = counts generation in
      dispose generation;
      result)
