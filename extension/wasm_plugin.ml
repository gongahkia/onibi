open Zenbu_kernel
open Zenbu_model_api
module Backend = Wasmtime_backend
module Host = Extension_host
module Registration = Extension_registration

type definition = {
  contribution : string;
  id : string;
  callback : string;
  title : string;
  description : string;
  requires_syntax : bool;
  input : string;
  scope : string;
  event : string;
}

type limits = { fuel : int; memory_bytes : int }
type health = Healthy | Unavailable of Error.t

let max_registrations = 128
let max_actions = 256
let max_selections = 1_024
let max_edits = 4_096

let default_limits =
  let value = Backend.default_limits in
  { fuel = value.fuel; memory_bytes = value.memory_bytes }

type runtime_event = {
  stage : string;
  operation : string option;
  outcome : string;
  duration_seconds : float;
  fuel_consumed : int option;
  reason : string option;
}

type t = {
  provider : Provider.t;
  backend : Backend.t;
  commands : Command.t list;
  semantic_behaviors : Semantic_behavior_registry.t;
  bindings : Registration.binding list;
  hooks : Registration.hook list;
  descriptors : Semantic_descriptor.t list;
  limits : limits;
  health : health ref;
  runtime_events : runtime_event Queue.t;
}

let contains ~substring text =
  let text = String.lowercase_ascii text in
  let substring = String.lowercase_ascii substring in
  let text_length = String.length text in
  let substring_length = String.length substring in
  let rec at index offset =
    if offset = substring_length then true
    else if text.[index + offset] <> substring.[offset] then false
    else at index (offset + 1)
  in
  let rec search index =
    if index + substring_length > text_length then false
    else if at index 0 then true
    else search (index + 1)
  in
  substring_length = 0 || search 0

let runtime_error_code message =
  if contains ~substring:"all fuel consumed" message then
    Error.Extension_fuel_exhausted
  else if contains ~substring:"component response exceeds Zenbu limit" message
  then Error.Extension_response_limit
  else if
    contains ~substring:"memory allocation denied" message
    || contains ~substring:"cannot grow memory" message
    || contains ~substring:"memory minimum size" message
  then Error.Extension_memory_exhausted
  else if
    contains ~substring:"wasm trap" message
    || contains ~substring:"wasm backtrace" message
  then Error.Extension_trap
  else if
    contains ~substring:"does not export zenbu:plugin/control@1.0.0" message
    || contains ~substring:"component control export" message
    || contains ~substring:"type mismatch" message
    || contains ~substring:"unknown import" message
    || contains ~substring:"matching implementation was not found" message
    || contains ~substring:"function implementation is missing" message
  then Error.Extension_abi_mismatch
  else Error.Extension_runtime_error

let bounded_message message =
  let limit = 240 in
  let message = String.trim message in
  if String.length message <= limit then message
  else String.sub message 0 limit ^ "…"

let runtime_message code message =
  match code with
  | Error.Extension_fuel_exhausted ->
      "component exhausted its fuel budget (all fuel consumed); reload the \
       plugin"
  | Error.Extension_memory_exhausted ->
      "component exceeded its memory limit (memory allocation denied); reload \
       the plugin"
  | Error.Extension_trap -> "component trapped (wasm trap); reload the plugin"
  | _ -> bounded_message message

let extension_error provider capabilities ?operation message =
  let code = runtime_error_code message in
  Error.Extension_error
    {
      code;
      plugin_id = Provider.plugin_id provider;
      provider = Some (Provider.id provider);
      operation;
      required = None;
      granted = capabilities;
      message = runtime_message code message;
    }

let runtime_unavailable provider capabilities ?operation error =
  Error.Extension_error
    {
      code = Error.Extension_runtime_unavailable;
      plugin_id = Provider.plugin_id provider;
      provider = Some (Provider.id provider);
      operation;
      required = None;
      granted = capabilities;
      message =
        "component runtime is unavailable after a fatal callback; reload the \
         plugin" ^ " (previous error: "
        ^ Error.extension_error_code_name
            (match error with
            | Error.Extension_error { code; _ } -> code
            | _ -> Error.Extension_runtime_error)
        ^ ")";
    }

let failure_needs_reload = function
  | Error.Extension_error
      {
        code =
          ( Error.Extension_fuel_exhausted | Error.Extension_memory_exhausted
          | Error.Extension_trap );
        _;
      } ->
      true
  | _ -> false

let response_limit provider capabilities phase detail =
  Error.Extension_error
    {
      code = Error.Extension_response_limit;
      plugin_id = Provider.plugin_id provider;
      provider = Some (Provider.id provider);
      operation = Some phase;
      required = None;
      granted = capabilities;
      message = "component response exceeds Zenbu limit: " ^ detail;
    }

let contribution_error provider capabilities contribution =
  Error.Extension_error
    {
      code = Error.Contribution_not_declared;
      plugin_id = Provider.plugin_id provider;
      provider = Some (Provider.id provider);
      operation = Some contribution;
      required = None;
      granted = capabilities;
      message = "component registered a contribution absent from its manifest";
    }

let namespace_error provider capabilities id =
  Error.Extension_error
    {
      code = Error.Namespace_violation;
      plugin_id = Provider.plugin_id provider;
      provider = Some (Provider.id provider);
      operation = Some id;
      required = None;
      granted = capabilities;
      message = "component contribution does not use its plugin ID namespace";
    }

let field value name = Extension_value.find value name

let required_text ~provider ~capabilities ~phase fields name =
  match List.assoc_opt name fields with
  | Some (Extension_value.Text value) when String.length value > 0 -> Ok value
  | _ ->
      Error
        (extension_error provider capabilities ~operation:phase
           ("component value requires a nonempty string field " ^ name))

let required_bool ~provider ~capabilities ~phase fields name =
  match List.assoc_opt name fields with
  | Some (Extension_value.Bool value) -> Ok value
  | _ ->
      Error
        (extension_error provider capabilities ~operation:phase
           ("component value requires a boolean field " ^ name))

let required_string ~provider ~capabilities ~phase fields name =
  match List.assoc_opt name fields with
  | Some (Extension_value.Text value) -> Ok value
  | _ ->
      Error
        (extension_error provider capabilities ~operation:phase
           ("component value requires a string field " ^ name))

let definition_of_value ~provider ~capabilities = function
  | Extension_value.Record fields ->
      let ( let* ) = Result.bind in
      let* contribution =
        required_text ~provider ~capabilities ~phase:"register" fields
          "contribution"
      in
      let* id =
        required_text ~provider ~capabilities ~phase:"register" fields "id"
      in
      let* callback =
        required_text ~provider ~capabilities ~phase:"register" fields
          "callback"
      in
      let* title =
        required_text ~provider ~capabilities ~phase:"register" fields "title"
      in
      let* description =
        required_text ~provider ~capabilities ~phase:"register" fields
          "description"
      in
      let* requires_syntax =
        required_bool ~provider ~capabilities ~phase:"register" fields
          "requires-syntax"
      in
      let* input =
        required_string ~provider ~capabilities ~phase:"register" fields "input"
      in
      let* scope =
        required_string ~provider ~capabilities ~phase:"register" fields "scope"
      in
      let* event =
        required_string ~provider ~capabilities ~phase:"register" fields "event"
      in
      Ok
        {
          contribution;
          id;
          callback;
          title;
          description;
          requires_syntax;
          input;
          scope;
          event;
        }
  | _ ->
      Error
        (extension_error provider capabilities ~operation:"register"
           "component registration must be a record")

let definitions ~provider ~capabilities = function
  | Extension_value.List values when List.length values <= max_registrations ->
      let rec collect result = function
        | [] -> Ok (List.rev result)
        | value :: rest ->
            Result.bind (definition_of_value ~provider ~capabilities value)
              (fun value -> collect (value :: result) rest)
      in
      collect [] values
  | Extension_value.List _ ->
      Error
        (response_limit provider capabilities "register"
           (Printf.sprintf "at most %d registrations are accepted"
              max_registrations))
  | _ ->
      Error
        (extension_error provider capabilities ~operation:"register"
           "component register export must return a list")

let action_error provider capabilities phase message =
  Error (extension_error provider capabilities ~operation:phase message)

let required_action_text provider capabilities phase value name =
  match field value name with
  | Some (Extension_value.Text value) when String.length value > 0 -> Ok value
  | _ ->
      action_error provider capabilities phase ("missing string field " ^ name)

let optional_value value name =
  Option.value ~default:Extension_value.Nil (field value name)

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

let semantic_operation provider capabilities value =
  match
    ( required_action_text provider capabilities "action" value "selector",
      required_action_text provider capabilities "action" value "transformation"
    )
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

let selection_action provider capabilities request value =
  Result.bind (Host.require request ~capability:"selection.write") (fun () ->
      match (field value "selections", field value "primary") with
      | ( Some (Extension_value.List selections),
          Some (Extension_value.Integer primary) ) ->
          if List.length selections > max_selections then
            Error
              (response_limit provider capabilities "action"
                 (Printf.sprintf "at most %d selections are accepted"
                    max_selections))
          else
            let selection = function
              | Extension_value.Record fields -> (
                  match
                    ( List.assoc_opt "anchor" fields,
                      List.assoc_opt "head" fields )
                  with
                  | ( Some (Extension_value.Integer anchor),
                      Some (Extension_value.Integer head) ) ->
                      Ok (anchor, head)
                  | _ ->
                      action_error provider capabilities "action"
                        "selection entries require integer anchor and head \
                         fields")
              | _ ->
                  action_error provider capabilities "action"
                    "selection entry must be a record"
            in
            let rec collect result = function
              | [] -> Ok (List.rev result)
              | candidate :: rest ->
                  Result.bind (selection candidate) (fun selection ->
                      collect (selection :: result) rest)
            in
            Result.bind (collect [] selections) (fun selections ->
                Model_intent.set_selections ~selections ~primary:(primary - 1))
            |> Result.map (fun intent -> Model_effect.Execute_intent intent)
      | _ ->
          action_error provider capabilities "action"
            "set-selections requires selections and primary")

let action provider capabilities request value =
  match required_action_text provider capabilities "action" value "kind" with
  | Error _ as error -> error
  | Ok "message" ->
      Result.bind (Host.require request ~capability:"ui.message") (fun () ->
          Result.bind
            (required_action_text provider capabilities "action" value "text")
            (fun text -> Model_effect.message ~level:Model_effect.Info ~text))
  | Ok "insert" ->
      Result.bind (Host.require request ~capability:"document.edit") (fun () ->
          required_action_text provider capabilities "action" value "text"
          |> Result.map (fun text ->
              Model_effect.Execute_intent (Model_intent.insert_text text)))
  | Ok "delete" ->
      Result.bind (Host.require request ~capability:"document.edit") (fun () ->
          Ok (Model_effect.Execute_intent Model_intent.delete_selected_ranges))
  | Ok "replace" ->
      Result.bind (Host.require request ~capability:"document.edit") (fun () ->
          required_action_text provider capabilities "action" value "text"
          |> Result.map (fun text ->
              Model_effect.Execute_intent
                (Model_intent.replace_selected_ranges text)))
  | Ok "set-selections" -> selection_action provider capabilities request value
  | Ok "apply" ->
      semantic_operation provider capabilities value
      |> Result.map Model_effect.execute_semantic_operation
  | Ok "command" ->
      Result.bind (Host.require request ~capability:"command.invoke") (fun () ->
          Result.bind
            (required_action_text provider capabilities "action" value "id")
            (fun id ->
              Result.bind (Command_id.of_string id) (fun id ->
                  Command_invocation.create ~id ~arguments:[]))
          |> Result.map (fun invocation ->
              Model_effect.Invoke_command invocation))
  | Ok kind ->
      action_error provider capabilities "action" ("unknown action kind " ^ kind)

let actions provider capabilities request = function
  | Extension_value.Nil -> Ok []
  | Extension_value.List values when List.length values <= max_actions ->
      let rec collect result = function
        | [] -> Ok (List.rev result)
        | value :: rest ->
            Result.bind (action provider capabilities request value)
              (fun value -> collect (value :: result) rest)
      in
      collect [] values
  | Extension_value.List _ ->
      Error
        (response_limit provider capabilities request.operation
           (Printf.sprintf "at most %d actions are accepted" max_actions))
  | value ->
      action provider capabilities request value
      |> Result.map (fun value -> [ value ])

let behavior_selection provider capabilities = function
  | Extension_value.Record fields -> (
      match
        (List.assoc_opt "selections" fields, List.assoc_opt "primary" fields)
      with
      | Some (Extension_value.List values), Some (Extension_value.Integer _)
        when List.length values > max_selections ->
          Error
            (response_limit provider capabilities "selector"
               (Printf.sprintf "at most %d selections are accepted"
                  max_selections))
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
                    action_error provider capabilities "selector"
                      "selection requires integer anchor and head fields")
            | _ ->
                action_error provider capabilities "selector"
                  "selection must be a record"
          in
          let rec collect result = function
            | [] -> Ok (List.rev result)
            | value :: rest ->
                Result.bind (entry value) (fun value ->
                    collect (value :: result) rest)
          in
          collect [] values
          |> Result.map (fun selections ->
              Semantic_behavior.{ selections; primary = primary - 1 })
      | _ ->
          action_error provider capabilities "selector"
            "selector result requires selections and primary")
  | _ ->
      action_error provider capabilities "selector"
        "selector result must be a record"

let behavior_transformation provider capabilities = function
  | Extension_value.Record fields -> (
      match List.assoc_opt "edits" fields with
      | Some (Extension_value.List values) when List.length values > max_edits
        ->
          Error
            (response_limit provider capabilities "transformation"
               (Printf.sprintf "at most %d edits are accepted" max_edits))
      | Some (Extension_value.List values) ->
          let entry = function
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
                    action_error provider capabilities "transformation"
                      "edits require integer start/stop and string text")
            | _ ->
                action_error provider capabilities "transformation"
                  "edit must be a record"
          in
          let rec collect result = function
            | [] -> Ok (List.rev result)
            | value :: rest ->
                Result.bind (entry value) (fun value ->
                    collect (value :: result) rest)
          in
          collect [] values
          |> Result.map (fun edits ->
              Semantic_behavior.{ edits; selections = None })
      | _ ->
          action_error provider capabilities "transformation"
            "transformation result requires an edits list")
  | _ ->
      action_error provider capabilities "transformation"
        "transformation result must be a record"

let inputs_of_string provider capabilities value =
  Input_event.binding_sequence_of_string value
  |> Result.map_error (fun error ->
      extension_error provider capabilities ~operation:"registration"
        (Error.to_string error))

let scope_of_string provider capabilities = function
  | "" | "global" -> Ok Registration.Global
  | value -> (
      match String.split_on_char ':' value with
      | [ "model"; model ] when String.length model > 0 ->
          Ok (Registration.Model model)
      | [ "model"; model; status ]
        when String.length model > 0 && String.length status > 0 ->
          Ok (Registration.Model_status { model; status })
      | _ ->
          Error
            (extension_error provider capabilities ~operation:"registration"
               "scope must be global, model:<id>, or model:<id>:<status>"))

let event_of_string provider capabilities = function
  | "document-changed" -> Ok Registration.Document_changed
  | "after-save" -> Ok Registration.After_save
  | value ->
      Error
        (extension_error provider capabilities ~operation:"registration"
           ("unknown event " ^ value))

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

let request_value request =
  Extension_value.Record
    [
      ("kind", Extension_value.Text (Host.kind_name request.Host.kind));
      ("operation", Extension_value.Text request.operation);
      ("context", request.context);
      ("arguments", request.arguments);
    ]

let load ~(limits : limits) ~provider ~capabilities ~contributions
    ~base_commands ~base_semantics ~entrypoint =
  let backend_limits =
    Backend.{ fuel = limits.fuel; memory_bytes = limits.memory_bytes }
  in
  match Backend.load ~entrypoint ~capabilities ~limits:backend_limits with
  | Error message ->
      Error (extension_error provider capabilities ~operation:"load" message)
  | Ok backend -> (
      let runtime_events = Queue.create () in
      let health = ref Healthy in
      let metrics = Backend.metrics backend in
      Queue.add
        {
          stage = "compile";
          operation = None;
          outcome = "succeeded";
          duration_seconds = metrics.compile_seconds;
          fuel_consumed = None;
          reason = None;
        }
        runtime_events;
      Queue.add
        {
          stage = "instantiate";
          operation = None;
          outcome = "succeeded";
          duration_seconds = metrics.instantiate_seconds;
          fuel_consumed = None;
          reason = None;
        }
        runtime_events;
      let dispose_on_error error =
        Backend.dispose backend;
        Error error
      in
      match Backend.register backend with
      | Error message ->
          let metrics = Backend.metrics backend in
          Queue.add
            {
              stage = "register";
              operation = None;
              outcome = "failed";
              duration_seconds = metrics.call_seconds;
              fuel_consumed = Some metrics.fuel_consumed;
              reason = Some message;
            }
            runtime_events;
          dispose_on_error
            (extension_error provider capabilities ~operation:"register" message)
      | Ok registrations ->
          let metrics = Backend.metrics backend in
          Queue.add
            {
              stage = "register";
              operation = None;
              outcome = "succeeded";
              duration_seconds = metrics.call_seconds;
              fuel_consumed = Some metrics.fuel_consumed;
              reason = None;
            }
            runtime_events;
          Result.bind (definitions ~provider ~capabilities registrations)
            (fun definitions ->
              let callbacks = Hashtbl.create (List.length definitions) in
              List.iter
                (fun definition ->
                  Hashtbl.replace callbacks definition.callback ())
                definitions;
              let host =
                Host.create ~runtime:"wasm-component"
                  ~invoke:(fun invocation request ->
                    let token = Host.invocation_token invocation in
                    if not (Hashtbl.mem callbacks token) then
                      Error
                        (extension_error provider capabilities
                           ~operation:request.operation
                           "component callback is no longer available")
                    else
                      match !health with
                      | Unavailable error ->
                          Error
                            (runtime_unavailable provider capabilities
                               ~operation:request.operation error)
                      | Healthy ->
                          let result =
                            Backend.invoke backend ~token
                              ~request:(request_value request)
                          in
                          let metrics = Backend.metrics backend in
                          let result =
                            result
                            |> Result.map_error (fun message ->
                                extension_error provider capabilities
                                  ~operation:request.operation message)
                          in
                          Result.iter_error
                            (fun error ->
                              if failure_needs_reload error then
                                health := Unavailable error)
                            result;
                          Queue.add
                            {
                              stage = "call";
                              operation = Some request.operation;
                              outcome =
                                (match result with
                                | Ok _ -> "succeeded"
                                | Error _ -> "failed");
                              duration_seconds = metrics.call_seconds;
                              fuel_consumed = Some metrics.fuel_consumed;
                              reason =
                                (match result with
                                | Ok _ -> None
                                | Error error -> Some (Error.to_string error));
                            }
                            runtime_events;
                          result)
              in
              let contribution_allowed name = List.mem name contributions in
              let namespaced id =
                Provider.plugin_id provider
                |> Option.map (fun plugin_id ->
                    String.starts_with ~prefix:(plugin_id ^ ".") id)
                |> Option.value ~default:false
              in
              let command_registry = ref base_commands in
              let semantic_registry = ref Semantic_behavior_registry.empty in
              let descriptors = ref [] in
              let commands = ref [] in
              let bindings = ref [] in
              let hooks = ref [] in
              let failed = ref None in
              let fail error =
                if Option.is_none !failed then failed := Some error
              in
              let require_contribution name =
                if contribution_allowed name then Ok ()
                else Error (contribution_error provider capabilities name)
              in
              let register_descriptor descriptor =
                let id = Semantic_descriptor.id descriptor in
                if
                  List.exists
                    (fun existing ->
                      String.equal (Semantic_descriptor.id existing) id)
                    !descriptors
                  || List.exists
                       (fun existing ->
                         String.equal (Semantic_descriptor.id existing) id)
                       base_semantics
                then fail (Error.Duplicate_descriptor id)
                else descriptors := !descriptors @ [ descriptor ]
              in
              let invocation definition =
                Host.invocation ~token:definition.callback ~provider
                  ~granted:capabilities
              in
              List.iter
                (fun definition ->
                  if Option.is_none !failed then
                    match definition.contribution with
                    | "commands" ->
                        Result.bind
                          (Result.bind
                             (Result.bind (require_contribution "commands")
                                (fun () ->
                                  if not (namespaced definition.id) then
                                    Error
                                      (namespace_error provider capabilities
                                         definition.id)
                                  else Command_id.of_string definition.id))
                             (fun id ->
                               Command_descriptor.create ~id
                                 ~title:definition.title
                                 ~description:definition.description ~provider
                                 ()))
                          (fun descriptor ->
                            let command =
                              Command.create_extension_effectful ~descriptor
                                ~host ~invocation:(invocation definition)
                                ~decode:(actions provider capabilities)
                            in
                            Command_registry.register !command_registry command
                            |> Result.map (fun registry ->
                                command_registry := registry;
                                commands := !commands @ [ command ]))
                        |> Result.iter_error fail
                    | "selectors" ->
                        Result.bind
                          (Result.bind (require_contribution "selectors")
                             (fun () ->
                               if not (namespaced definition.id) then
                                 Error
                                   (namespace_error provider capabilities
                                      definition.id)
                               else
                                 Semantic_descriptor.create ~id:definition.id
                                   ~title:definition.title
                                   ~description:definition.description ~provider
                                   ~kind:Semantic_descriptor.Selector
                                   ~requires_syntax:definition.requires_syntax
                                   ()))
                          (fun descriptor ->
                            register_descriptor descriptor;
                            match !failed with
                            | Some error -> Error error
                            | None ->
                                let entry =
                                  Semantic_behavior.extension_selector_entry
                                    ~descriptor ~host
                                    ~invocation:(invocation definition)
                                    ~decode:(fun _ ->
                                      behavior_selection provider capabilities)
                                in
                                Semantic_behavior_registry.register_selector
                                  !semantic_registry entry
                                |> Result.map (fun registry ->
                                    semantic_registry := registry))
                        |> Result.iter_error fail
                    | "transformations" ->
                        Result.bind
                          (Result.bind (require_contribution "transformations")
                             (fun () ->
                               if not (namespaced definition.id) then
                                 Error
                                   (namespace_error provider capabilities
                                      definition.id)
                               else
                                 Semantic_descriptor.create ~id:definition.id
                                   ~title:definition.title
                                   ~description:definition.description ~provider
                                   ~kind:Semantic_descriptor.Transformation
                                   ~requires_syntax:definition.requires_syntax
                                   ()))
                          (fun descriptor ->
                            register_descriptor descriptor;
                            match !failed with
                            | Some error -> Error error
                            | None ->
                                let entry =
                                  Semantic_behavior
                                  .extension_transformation_entry ~descriptor
                                    ~host ~invocation:(invocation definition)
                                    ~decode:(fun _ ->
                                      behavior_transformation provider
                                        capabilities)
                                in
                                Semantic_behavior_registry
                                .register_transformation !semantic_registry
                                  entry
                                |> Result.map (fun registry ->
                                    semantic_registry := registry))
                        |> Result.iter_error fail
                    | "bindings" ->
                        Result.bind (require_contribution "bindings") (fun () ->
                            if not (namespaced definition.id) then
                              Error
                                (namespace_error provider capabilities
                                   definition.id)
                            else
                              Result.bind
                                (inputs_of_string provider capabilities
                                   definition.input) (function
                                | [] ->
                                    Error
                                      (extension_error provider capabilities
                                         ~operation:"registration"
                                         "binding sequence must not be empty")
                                | head :: tail ->
                                    Result.bind
                                      (scope_of_string provider capabilities
                                         definition.scope) (fun scope ->
                                        if
                                          List.exists reserved_host_input
                                            (head :: tail)
                                        then
                                          Error
                                            (extension_error provider
                                               capabilities
                                               ~operation:"registration"
                                               "reserved host input cannot \
                                                appear in a binding")
                                        else
                                          Result.bind
                                            (Command_id.of_string definition.id)
                                            (fun command ->
                                              Command_registry.find
                                                !command_registry command)
                                          |> Result.map (fun _ ->
                                              Registration.binding_sequence
                                                ~mode_transition:None ~head
                                                ~tail ~command:definition.id
                                                ~scope ~provider))))
                        |> fun candidate ->
                        Result.bind candidate (fun binding ->
                            let duplicate =
                              List.exists
                                (Registration.bindings_conflict binding)
                                !bindings
                            in
                            if duplicate then
                              Error
                                (extension_error provider capabilities
                                   ~operation:"registration"
                                   "duplicate component binding")
                            else (
                              bindings := !bindings @ [ binding ];
                              Ok ()))
                        |> Result.iter_error fail
                    | "events" ->
                        Result.bind (require_contribution "events") (fun () ->
                            if not (List.mem "event.subscribe" capabilities)
                            then
                              Error
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
                            else
                              event_of_string provider capabilities
                                definition.event)
                        |> Result.map (fun event ->
                            let callback = invocation definition in
                            Registration.hook ~event ~provider
                              ~run:(fun context ->
                                let event =
                                  match event with
                                  | Registration.Document_changed ->
                                      "document-changed"
                                  | Registration.After_save -> "after-save"
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
                                  (actions provider capabilities request)))
                        |> Result.map (fun hook -> hooks := !hooks @ [ hook ])
                        |> Result.iter_error fail
                    | unknown ->
                        fail
                          (extension_error provider capabilities
                             ~operation:"register"
                             ("unknown component contribution " ^ unknown)))
                definitions;
              match !failed with
              | Some error -> dispose_on_error error
              | None ->
                  Ok
                    {
                      provider;
                      backend;
                      commands = !commands;
                      semantic_behaviors = !semantic_registry;
                      bindings = !bindings;
                      hooks = !hooks;
                      descriptors = !descriptors;
                      limits;
                      health;
                      runtime_events;
                    }))

let provider value = value.provider
let commands value = value.commands
let semantic_behaviors value = value.semantic_behaviors
let descriptors value = value.descriptors
let bindings value = value.bindings
let hooks value = value.hooks
let limits value = value.limits
let health value = !(value.health)

let health_error value =
  match health value with Healthy -> None | Unavailable error -> Some error

let drain_runtime_events value =
  let events = Queue.to_seq value.runtime_events |> List.of_seq in
  Queue.clear value.runtime_events;
  events

let dispose value = Backend.dispose value.backend
