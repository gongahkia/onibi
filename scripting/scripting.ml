open Zenbu_kernel
open Zenbu_model_api
module Backend = Lua_backend
module Host = Extension_host
module Registration = Extension_registration

type event = Registration.event = Document_changed | After_save

type scope = Registration.scope =
  | Global
  | Model of string
  | Model_status of { model : string; status : string }
  | Mode of string

type mode_transition = Registration.mode_transition =
  | Replace_mode of string
  | Push_mode of string
  | Pop_mode
  | Clear_modes

type binding = Registration.binding
type hook = Registration.hook
type mode = { id : string; title : string; description : string }

type t = {
  generation_id : int;
  source : string;
  provider : Provider.t;
  backend : Backend.t;
  commands : Command.t list;
  semantic_behaviors : Semantic_behavior_registry.t;
  bindings : binding list;
  hooks : hook list;
  modes : mode list;
  descriptors : Semantic_descriptor.t list;
}

type config = Default | Explicit of string | Disabled

let api_version = 1

let trusted_capabilities =
  [
    "document.read";
    "document.edit";
    "selection.read";
    "selection.write";
    "syntax.read";
    "command.invoke";
    "ui.message";
    "event.subscribe";
  ]

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

let require request capability = Host.require request ~capability

let selection_action source request value =
  Result.bind (require request "selection.write") (fun () ->
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
                         "selection entries require integer anchor and head \
                          fields"))
            | _ ->
                Error
                  (script_error "action" source
                     "selection entry must be a table")
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
               "set-selections requires selections and primary fields"))

let action source request value =
  match required_text source "kind" value with
  | Error _ as error -> error
  | Ok "message" ->
      Result.bind (require request "ui.message") (fun () ->
          Result.bind (required_text source "text" value) (fun text ->
              Model_effect.message ~level:Model_effect.Info ~text))
  | Ok "insert" ->
      Result.bind (require request "document.edit") (fun () ->
          required_text source "text" value
          |> Result.map (fun text ->
              Model_effect.Execute_intent (Model_intent.insert_text text)))
  | Ok "delete" ->
      Result.bind (require request "document.edit") (fun () ->
          Ok (Model_effect.Execute_intent Model_intent.delete_selected_ranges))
  | Ok "replace" ->
      Result.bind (require request "document.edit") (fun () ->
          required_text source "text" value
          |> Result.map (fun text ->
              Model_effect.Execute_intent
                (Model_intent.replace_selected_ranges text)))
  | Ok "set-selections" -> selection_action source request value
  | Ok "apply" ->
      semantic_operation source value
      |> Result.map Model_effect.execute_semantic_operation
  | Ok "command" ->
      Result.bind (require request "command.invoke") (fun () ->
          Result.bind (required_text source "id" value) (fun id ->
              Result.bind (Command_id.of_string id) (fun id ->
                  Command_invocation.create ~id ~arguments:[]))
          |> Result.map (fun invocation ->
              Model_effect.Invoke_command invocation))
  | Ok kind ->
      Error (script_error "action" source ("unknown action kind " ^ kind))

let actions source request = function
  | Extension_value.Nil -> Ok []
  | Extension_value.List values ->
      let rec collect results = function
        | [] -> Ok (List.rev results)
        | value :: rest -> (
            match action source request value with
            | Error _ as error -> error
            | Ok action -> collect (action :: results) rest)
      in
      collect [] values
  | value ->
      action source request value |> Result.map (fun action -> [ action ])

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

let inputs_of_string source value =
  Input_event.binding_sequence_of_string value
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
      | [ "mode"; mode ] when String.length mode > 0 -> Ok (Mode mode)
      | _ ->
          Error
            (script_error "registration" source
               "scope must be global, model:<id>, model:<id>:<status>, or \
                mode:<id>"))

let reserved_host_input input =
  [
    "Ctrl-S";
    "Ctrl-Shift-S";
    "Ctrl-Q";
    "Alt-R";
    "Ctrl-Alt-R";
    "Ctrl-F";
    "Ctrl-G";
    "Ctrl-Shift-G";
    "Ctrl-P";
    "Ctrl-Space";
    "Alt-M";
    "Meta-M";
    "Alt-H";
    "Meta-H";
    "Ctrl-O";
  ]
  |> List.exists (fun source ->
      Input_event.binding_event_of_string source
      |> Result.map (fun reserved ->
          String.equal
            (Input_event.to_string input)
            (Input_event.to_string reserved))
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

let mode_transition_of_backend = function
  | None -> None
  | Some (Backend.Replace_mode id) -> Some (Replace_mode id)
  | Some (Backend.Push_mode id) -> Some (Push_mode id)
  | Some Backend.Pop_mode -> Some Pop_mode
  | Some Backend.Clear_modes -> Some Clear_modes

let mode_transition_target = function
  | Some (Replace_mode id | Push_mode id) -> Some id
  | None | Some Pop_mode | Some Clear_modes -> None

let load_from_source ?provider ?(capabilities = trusted_capabilities)
    ?contributions ?(runtime = "lua-trusted") ~generation_id ~base_commands
    ~base_semantics ~source text =
  let provider =
    Option.value provider
      ~default:
        (Provider.create_with_source
           ~id:("script." ^ string_of_int generation_id)
           ~kind:Provider.Script ~source
        |> Result.get_ok)
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
          let modes = ref [] in
          let failed = ref None in
          let callbacks = ref [] in
          let next_callback = ref 0 in
          let host =
            Host.create ~runtime ~invoke:(fun invocation request ->
                match
                  List.assoc_opt (Host.invocation_token invocation) !callbacks
                with
                | Some callback -> Backend.call backend callback ~request
                | None ->
                    Error
                      (Error.Extension_error
                         {
                           code = Error.Extension_runtime_error;
                           plugin_id = Provider.plugin_id provider;
                           provider = Some (Provider.id provider);
                           operation = Some request.operation;
                           required = None;
                           granted = capabilities;
                           message = "extension callback is no longer available";
                         }))
          in
          let invocation kind callback =
            next_callback := !next_callback + 1;
            let token = kind ^ ":" ^ string_of_int !next_callback in
            callbacks := (token, callback) :: !callbacks;
            Host.invocation ~token ~provider ~granted:capabilities
          in
          let contribution_allowed contribution =
            match contributions with
            | None -> true
            | Some declared -> List.mem contribution declared
          in
          let verify_registration contribution id =
            if not (contribution_allowed contribution) then
              Error
                (Error.Extension_error
                   {
                     code = Error.Contribution_not_declared;
                     plugin_id = Provider.plugin_id provider;
                     provider = Some (Provider.id provider);
                     operation = Some contribution;
                     required = None;
                     granted = capabilities;
                     message =
                       "registration is not declared by the plugin manifest";
                   })
            else
              match Provider.plugin_id provider with
              | None -> Ok ()
              | Some plugin_id
                when String.starts_with ~prefix:(plugin_id ^ ".") id ->
                  Ok ()
              | Some plugin_id ->
                  Error
                    (Error.Extension_error
                       {
                         code = Error.Namespace_violation;
                         plugin_id = Some plugin_id;
                         provider = Some (Provider.id provider);
                         operation = Some id;
                         required = None;
                         granted = capabilities;
                         message =
                           "plugin registrations must use the plugin ID \
                            namespace";
                       })
          in
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
          let register_mode definition =
            match validate_id source definition.Backend.id with
            | Error error -> fail error
            | Ok _ when List.exists (fun mode -> mode.id = definition.id) !modes
              ->
                fail (Error.Duplicate_descriptor definition.id)
            | Ok _ ->
                modes :=
                  !modes
                  @ [
                      {
                        id = definition.id;
                        title = definition.title;
                        description = definition.description;
                      };
                    ]
          in
          List.iter
            (function
              | Backend.Mode definition when Option.is_none !failed ->
                  register_mode definition
              | Backend.Mode _ | Backend.Binding _ | Backend.Hook _
              | Backend.Command _ | Backend.Selector _
              | Backend.Transformation _ ->
                  ())
            registrations;
          List.iter
            (function
              | Backend.Command (definition, callback)
                when Option.is_none !failed -> (
                  match validate_id source definition.id with
                  | Error error -> fail error
                  | Ok id -> (
                      match verify_registration "commands" definition.id with
                      | Error error -> fail error
                      | Ok () -> (
                          match
                            Command_descriptor.create ~id
                              ~title:definition.title
                              ~description:definition.description
                              ~parameters:definition.parameters ~provider ()
                          with
                          | Error error -> fail error
                          | Ok descriptor -> (
                              let command =
                                Command.create_extension_effectful ~descriptor
                                  ~host
                                  ~invocation:(invocation "command" callback)
                                  ~decode:(actions source)
                              in
                              match
                                Command_registry.register !command_registry
                                  command
                              with
                              | Error error -> fail error
                              | Ok registry ->
                                  command_registry := registry;
                                  script_commands :=
                                    !script_commands @ [ command ]))))
              | Backend.Selector (definition, callback)
                when Option.is_none !failed -> (
                  match validate_id source definition.id with
                  | Error error -> fail error
                  | Ok _ -> (
                      match verify_registration "selectors" definition.id with
                      | Error error -> fail error
                      | Ok () -> (
                          match
                            semantic_descriptor provider
                              Semantic_descriptor.Selector definition
                          with
                          | Error error -> fail error
                          | Ok descriptor -> (
                              register_descriptor descriptor;
                              if Option.is_none !failed then
                                let entry =
                                  Semantic_behavior.extension_selector_entry
                                    ~descriptor ~host
                                    ~invocation:(invocation "selector" callback)
                                    ~decode:(fun _ -> behavior_selection source)
                                in
                                match
                                  Semantic_behavior_registry.register_selector
                                    !behavior_registry entry
                                with
                                | Error error -> fail error
                                | Ok registry -> behavior_registry := registry))
                      ))
              | Backend.Transformation (definition, callback)
                when Option.is_none !failed -> (
                  match validate_id source definition.id with
                  | Error error -> fail error
                  | Ok _ -> (
                      match
                        verify_registration "transformations" definition.id
                      with
                      | Error error -> fail error
                      | Ok () -> (
                          match
                            semantic_descriptor provider
                              Semantic_descriptor.Transformation definition
                          with
                          | Error error -> fail error
                          | Ok descriptor -> (
                              register_descriptor descriptor;
                              if Option.is_none !failed then
                                let entry =
                                  Semantic_behavior
                                  .extension_transformation_entry ~descriptor
                                    ~host
                                    ~invocation:
                                      (invocation "transformation" callback)
                                    ~decode:(fun _ ->
                                      behavior_transformation source)
                                in
                                match
                                  Semantic_behavior_registry
                                  .register_transformation !behavior_registry
                                    entry
                                with
                                | Error error -> fail error
                                | Ok registry -> behavior_registry := registry))
                      ))
              | Backend.Mode _ | Backend.Binding _ | Backend.Hook _
              | Backend.Command _ | Backend.Selector _
              | Backend.Transformation _ ->
                  ())
            registrations;
          List.iter
            (function
              | Backend.Binding definition when Option.is_none !failed -> (
                  match
                    ( inputs_of_string source definition.input,
                      scope_of_string source definition.scope,
                      Command_id.of_string definition.command,
                      Ok (mode_transition_of_backend definition.mode_transition)
                    )
                  with
                  | Ok (head :: tail), Ok scope, Ok command, Ok mode_transition
                    -> (
                      match
                        verify_registration "bindings" definition.command
                      with
                      | Error error -> fail error
                      | Ok () when List.exists reserved_host_input (head :: tail)
                        ->
                          fail
                            (script_error "registration" source
                               "reserved host input cannot appear in a binding")
                      | Ok () -> (
                          let mode_exists = function
                            | None -> true
                            | Some id ->
                                List.exists (fun mode -> mode.id = id) !modes
                          in
                          let scope_mode_exists = function
                            | Mode id ->
                                List.exists (fun mode -> mode.id = id) !modes
                            | Global | Model _ | Model_status _ -> true
                          in
                          if
                            not
                              (mode_transition_target mode_transition
                              |> mode_exists)
                          then
                            fail
                              (script_error "registration" source
                                 "binding mode transition must name a declared \
                                  mode")
                          else if not (scope_mode_exists scope) then
                            fail
                              (script_error "registration" source
                                 "mode binding scope must name a declared mode")
                          else
                            match
                              ( String.equal definition.command "config.reload",
                                Command_registry.find !command_registry command
                              )
                            with
                            | true, _ | false, Ok _ ->
                                let candidate =
                                  Registration.binding_sequence ~mode_transition
                                    ~head ~tail ~command:definition.command
                                    ~scope ~provider
                                in
                                let duplicate =
                                  List.exists
                                    (Registration.bindings_conflict candidate)
                                    !bindings
                                in
                                if duplicate then
                                  fail
                                    (script_error "registration" source
                                       ("duplicate binding or prefix-ambiguous \
                                         sequence for "
                                       ^ Input_event.binding_sequence_to_string
                                           (head :: tail)))
                                else bindings := !bindings @ [ candidate ]
                            | false, Error error -> fail error))
                  | Ok [], _, _, _ ->
                      fail
                        (script_error "registration" source
                           "binding sequence must not be empty")
                  | Error error, _, _, _
                  | _, Error error, _, _
                  | _, _, Error error, _
                  | _, _, _, Error error ->
                      fail error)
              | Backend.Hook definition when Option.is_none !failed -> (
                  match event_of_string source definition.event with
                  | Error error -> fail error
                  | Ok _ when not (contribution_allowed "events") ->
                      fail
                        (Error.Extension_error
                           {
                             code = Error.Contribution_not_declared;
                             plugin_id = Provider.plugin_id provider;
                             provider = Some (Provider.id provider);
                             operation = Some "events";
                             required = None;
                             granted = capabilities;
                             message =
                               "event registration is not declared by the \
                                plugin manifest";
                           })
                  | Ok _ when not (List.mem "event.subscribe" capabilities) ->
                      fail
                        (Error.Extension_error
                           {
                             code = Error.Capability_denied;
                             plugin_id = Provider.plugin_id provider;
                             provider = Some (Provider.id provider);
                             operation = Some "event.subscribe";
                             required = Some "event.subscribe";
                             granted = capabilities;
                             message = "event subscription was denied";
                           })
                  | Ok event ->
                      let callback = invocation "event" definition.callback in
                      hooks :=
                        !hooks
                        @ [
                            Registration.hook ~event ~provider
                              ~run:(fun context ->
                                let event =
                                  match event with
                                  | Document_changed -> "document-changed"
                                  | After_save -> "after-save"
                                in
                                let request =
                                  Host.request callback ~kind:Host.Event
                                    ~operation:"event.deliver" ~context
                                    ~arguments:
                                      (Extension_value.Record
                                         [
                                           ("event", Extension_value.Text event);
                                         ])
                                in
                                Result.bind
                                  (Host.invoke host callback request)
                                  (actions source request));
                          ])
              | Backend.Mode _ | Backend.Binding _ | Backend.Hook _
              | Backend.Command _ | Backend.Selector _
              | Backend.Transformation _ ->
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
                  modes = !modes;
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
let modes value = value.modes

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
let binding_input = Registration.binding_input
let binding_inputs = Registration.binding_inputs
let binding_command = Registration.binding_command
let binding_scope = Registration.binding_scope
let binding_mode_transition = Registration.binding_mode_transition
let binding_provider = Registration.binding_provider
let mode_id value = value.id
let mode_title value = value.title
let mode_description value = value.description
let hook_event = Registration.hook_event
let hook_provider = Registration.hook_provider
let run_hook = Registration.run_hook

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

let load_plugin ~provider ~capabilities ~contributions ~base_commands
    ~base_semantics ~entrypoint =
  Result.bind (read_file entrypoint) (fun text ->
      load_from_source ~provider ~capabilities ~contributions
        ~runtime:
          (Option.value ~default:"lua-trusted" (Provider.runtime provider))
        ~generation_id:0 ~base_commands ~base_semantics ~source:entrypoint text)

let check_file ~base_commands ~base_semantics path =
  Result.bind (read_file path)
    (load_from_source ~generation_id:0 ~base_commands ~base_semantics
       ~source:path)
  |> Result.map (fun generation ->
      let result = counts generation in
      dispose generation;
      result)
