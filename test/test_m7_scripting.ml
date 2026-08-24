open Zenbu_kernel
open Zenbu_model_api

exception Test_failure of string

let failf format =
  Printf.ksprintf (fun message -> raise (Test_failure message)) format

let expect condition format =
  Printf.ksprintf
    (fun message -> if not condition then raise (Test_failure message))
    format

let must = function
  | Ok value -> value
  | Error error -> failf "%s" (Error.to_string error)

let ctrl text =
  Input_event.logical_text (String.lowercase_ascii text)
  |> must
  |> Input_event.key_press ~modifiers:[ Input_event.Control ]

let ctrl_shift text =
  Input_event.logical_text text
  |> must
  |> Input_event.key_press ~modifiers:[ Input_event.Shift; Input_event.Control ]

let key text = Input_event.logical_text text |> must |> Input_event.key_press
let text_input text = Input_event.text_input text |> must
let dimensions = Zenbu_view.Renderer.{ columns = 120; rows = 40 }

let write path text =
  let channel = open_out_bin path in
  Fun.protect
    ~finally:(fun () -> close_out_noerr channel)
    (fun () -> output_string channel text)

let contains text fragment =
  let text_length = String.length text in
  let fragment_length = String.length fragment in
  let rec loop offset =
    if offset + fragment_length > text_length then false
    else if String.sub text offset fragment_length = fragment then true
    else loop (offset + 1)
  in
  fragment_length = 0 || loop 0

let wait_for_background_job session expected =
  let deadline = Unix.gettimeofday () +. 2.0 in
  let rec wait session =
    let lines = Zenbu_app.Session.inspect session Zenbu_app.Session.Jobs in
    if List.exists (fun line -> contains line expected) lines then session
    else if Unix.gettimeofday () >= deadline then
      failf "background job did not report %S: %s" expected
        (String.concat " | " lines)
    else
      let wakeup =
        match Zenbu_app.Session.background_job_wakeup_fd session with
        | Some fd -> fd
        | None -> failf "background job did not allocate a wakeup descriptor"
      in
      ignore (Unix.select [ wakeup ] [] [] 0.05);
      wait (Zenbu_app.Session.poll_background session)
  in
  wait session

let invoke_palette_text_argument session command value =
  let session =
    match
      Zenbu_app.Session.handle_host session Zenbu_app.Session.Open_palette
    with
    | Zenbu_app.Session.Continue session -> session
    | Zenbu_app.Session.Exit _ -> failf "opening the command palette exited"
  in
  let session = Zenbu_app.Session.handle_input session (text_input command) in
  let session =
    Zenbu_app.Session.handle_input session
      (Input_event.key_press (Input_event.named_key Input_event.Enter))
  in
  expect
    (Model_status.id (Zenbu_app.Session.status session)
    = "host-command-argument")
    "command %s did not open its required argument prompt" command;
  let session = Zenbu_app.Session.handle_input session (text_input value) in
  Zenbu_app.Session.handle_input session
    (Input_event.key_press (Input_event.named_key Input_event.Enter))

let base_commands () =
  Command_registry.register Command_registry.empty
    Zenbu_proof_models.Semantic_commands.apply_command
  |> must

let base_semantics () =
  Inspector.semantic_registry () |> Semantic_registry.descriptors

let surround_config prefix suffix =
  Printf.sprintf
    {|
zenbu.selector {
  id = "user.whole-document",
  title = "Whole document",
  description = "Select the current document.",
  run = function(call)
    local length = call.context.document.length
    return { selections = {{ anchor = 0, head = length }}, primary = 1 }
  end,
}

zenbu.transform {
  id = "user.surround",
  title = "Surround",
  description = "Surround selections atomically.",
  run = function(call)
    local edits = {}
    for _, selection in ipairs(call.arguments.selection_set) do
      local start = math.min(selection.anchor, selection.head)
      local stop = math.max(selection.anchor, selection.head)
      table.insert(edits, { start = start, stop = start, text = %S })
      table.insert(edits, { start = stop, stop = stop, text = %S })
    end
    return { edits = edits }
  end,
}

zenbu.command {
  id = "user.wrap",
  title = "Wrap document",
  description = "Apply the scripted selector and transformation.",
  run = function(_)
    return {{ kind = "apply", selector = "user.whole-document", transformation = "user.surround" }}
  end,
}

zenbu.bind { input = "Ctrl-K", command = "user.wrap", scope = "global" }
zenbu.on {
  event = "document-changed",
  run = function(_) return {{ kind = "message", text = "script change observed" }} end,
}
|}
    prefix suffix

let test_load_reload_and_dynamic_semantics () =
  let path = Filename.temp_file "zenbu-m7" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path (surround_config "(" ")");
      let trace = Trace.enabled ~capacity:128 |> must in
      let profiler = Profiler.enabled ~capacity:128 |> must in
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~trace ~profiler ~config:(Zenbu_scripting.Scripting.Explicit path)
          ~dimensions ()
        |> must
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      if not (Zenbu_app.Session.contents session = "(alpha)") then
        failf
          "script command did not commit the configured surround transaction: \
           %S; scripts: %s"
          (Zenbu_app.Session.contents session)
          (Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
           @ Zenbu_app.Session.inspect session Zenbu_app.Session.Why
          |> String.concat " | ");
      let scripts =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
        |> String.concat "\n"
      in
      expect
        (String.contains scripts 'g')
        "script generation inspection is empty";
      let why =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Why
        |> String.concat "\n"
      in
      expect (String.contains why 'u')
        "script command provenance is absent from why";
      let events = Trace.events trace in
      expect
        (List.exists
           (function
             | Trace_event.Script_lifecycle
                 { phase = "load"; outcome = "succeeded"; _ }
             | Trace_event.Script_callback { kind = "command"; _ }
             | Trace_event.Binding_resolved _ ->
                 true
             | _ -> false)
           events)
        "script lifecycle, callback, or binding trace events are absent";
      expect
        (List.exists
           (fun aggregate ->
             Profiler.aggregate_stage aggregate = Profiler.Script_command)
           (Profiler.aggregates profiler))
        "script command profiling is absent";
      write path (surround_config "[" "]");
      let session = Zenbu_app.Session.reload_config session in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      expect
        (Zenbu_app.Session.contents session = "[(alpha)]")
        "successful reload did not replace the active transform";
      write path "this is not valid lua";
      let session = Zenbu_app.Session.reload_config session in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      expect
        (Zenbu_app.Session.contents session = "[[(alpha)]]")
        "failed reload replaced the previous working generation";
      write path
        "zenbu.on { event = 'after-save', run = function(_) return nil end }";
      let session = Zenbu_app.Session.reload_config session in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      expect
        (Zenbu_app.Session.contents session = "[[(alpha)]]")
        "removed script binding remained reachable after reload")

let test_config_validation () =
  let path = Filename.temp_file "zenbu-m7-check" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path (surround_config "🙂" "!");
      let base_commands =
        Command_registry.register Command_registry.empty
          Zenbu_proof_models.Semantic_commands.apply_command
        |> must
      in
      let base_semantics =
        Inspector.semantic_registry () |> Semantic_registry.descriptors
      in
      let commands, selectors, transformations, bindings, hooks =
        Zenbu_scripting.Scripting.check_file ~base_commands ~base_semantics path
        |> must
      in
      expect
        (commands = 1 && selectors = 1 && transformations = 1)
        "config check did not report executable registrations";
      expect
        (bindings = 1 && hooks = 1)
        "config check did not report bindings and hooks";
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"é"
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions ()
        |> must
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      expect
        (Zenbu_app.Session.contents session = "🙂é!")
        "Unicode script edit did not preserve UTF-8 boundaries")

let modal_model_config suffix =
  Printf.sprintf
    {|
zenbu.model {
  id = "workload.modal",
  title = "Workload modal editor",
  description = "A persistent Lua-owned modal grammar.",
  initial_state = { mode = "normal", changes = 0 },
  initial_status = { id = "normal", label = "NORMAL", input_mode = "keys" },
  run = function(call)
    local input = call.arguments.input
    local state = call.arguments.state
    local function result(next, status, effects)
      return { state = next, status = status, effects = effects or {} }
    end
    if input.kind == "key" and input.key == "i" then
      return result(
        { mode = "insert", changes = state.changes },
        { id = "insert", label = "INSERT", input_mode = "text" })
    elseif input.kind == "key" and input.key == "Escape" then
      return result(
        { mode = "normal", changes = state.changes },
        { id = "normal", label = "NORMAL", input_mode = "keys" })
    elseif input.kind == "text" and state.mode == "insert" then
      return result(
        { mode = "insert", changes = state.changes + 1 },
        { id = "insert", label = "INSERT", input_mode = "text" },
        {{ kind = "insert", text = input.text .. %S }})
    else
      local input_mode = state.mode == "insert" and "text" or "keys"
      return result(state, { id = state.mode, label = string.upper(state.mode), input_mode = input_mode })
    end
  end,
}
|}
    suffix

let test_script_owned_model_state_and_reload () =
  let path = Filename.temp_file "zenbu-m7-model" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path (modal_model_config "!");
      let trace = Trace.enabled ~capacity:128 |> must in
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Script
          ~contents:"alpha" ~trace
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions ()
        |> must
      in
      expect
        (Zenbu_app.Session.model session = Zenbu_app.Session.Script
        && Model_status.id (Zenbu_app.Session.status session) = "normal")
        "script model did not initialize its declared persistent state";
      expect
        (Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
        |> List.exists (String.equal "model: workload.modal"))
        "Scripts inspection did not identify the configured model";
      let session = Zenbu_app.Session.handle_input session (key "i") in
      expect
        (Model_status.id (Zenbu_app.Session.status session) = "insert")
        "script model did not transition into its declared text state: %s; %s"
        (Model_status.id (Zenbu_app.Session.status session))
        (Trace.events trace
        |> List.filter_map (function
          | Trace_event.Error_reported { reason; _ } -> Some reason
          | _ -> None)
        |> String.concat " | ");
      let session = Zenbu_app.Session.handle_input session (text_input "β") in
      expect
        (Zenbu_app.Session.contents session = "β!alpha"
        && Model_status.id (Zenbu_app.Session.status session) = "insert")
        "script model state or declarative insert effect was not applied";
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.key_press (Input_event.named_key Input_event.Escape))
      in
      expect
        (Model_status.id (Zenbu_app.Session.status session) = "normal")
        "script model did not retain state across callbacks";
      let why =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Why
        |> String.concat "\n"
      in
      expect
        (contains why "workload.modal")
        "script model identity is absent from the normal provenance path";
      expect
        (List.exists
           (function
             | Trace_event.Script_callback { kind = "model"; _ } -> true
             | _ -> false)
           (Trace.events trace))
        "script model callback is absent from trace output";
      write path (modal_model_config "?");
      let session = Zenbu_app.Session.reload_config session in
      expect
        (Model_status.id (Zenbu_app.Session.status session) = "normal")
        "reloading a script model did not reset its explicit model state";
      let session = Zenbu_app.Session.handle_input session (key "i") in
      let session = Zenbu_app.Session.handle_input session (text_input "γ") in
      expect
        (Zenbu_app.Session.contents session = "β!γ?alpha")
        "script-model reload retained a disposed callback or stale definition";
      write path
        {|
zenbu.model {
  id = "workload.invalid",
  initial_state = {},
  initial_status = { id = "normal", label = "NORMAL" },
  run = function(_) return { state = {} } end,
}
|};
      let broken = Zenbu_app.Session.reload_config session in
      let broken = Zenbu_app.Session.handle_input broken (key "x") in
      expect
        (Zenbu_app.Session.contents broken = "β!γ?alpha")
        "an invalid script-model callback partially mutated the document";
      let error_lines =
        Trace.events trace
        |> List.filter_map (function
          | Trace_event.Error_reported { reason; _ } -> Some reason
          | _ -> None)
        |> String.concat "\n"
      in
      expect
        (contains error_lines "model result requires status")
        "script-model response validation did not report its boundary failure")

let counter_model_config ?(persistence = "") ?(extra = "") version =
  Printf.sprintf
    {|
local function counter_status(state)
  return {
    id = "counter-" .. tostring(state.version),
    label = "COUNTER " .. tostring(state.count),
  }
end

zenbu.model {
  id = "workload.counter",
  title = "Reload counter",
  initial_state = { version = %d, count = 0 },
  initial_status = { id = "counter-%d", label = "COUNTER 0" },
  %s
  run = function(call)
    local state = call.arguments.state
    local input = call.arguments.input
    if input.kind == "key" and input.key == "a" then
      local next = { version = state.version, count = state.count + 1 }
      return { state = next, status = counter_status(next) }
    elseif input.kind == "key" and input.key == "x" then
      return {
        state = state,
        status = counter_status(state),
        effects = {{ kind = "insert", text = "v" .. tostring(state.version) .. ":" .. tostring(state.count) }},
      }
    end
    return { state = state, status = counter_status(state) }
  end,
}
%s
|}
    version version persistence extra

let counter_persistence_v1 =
  {|
persistence = {
  schema = "workload.counter",
  version = 1,
  export = function(call)
    return { count = call.arguments.state.count }
  end,
  import = function(call)
    local source = call.arguments
    if source.from_schema ~= "workload.counter" or source.from_version ~= 2 then
      error("counter v1 only imports schema version 2")
    end
    local state = { version = 1, count = source.state.count - 10 }
    return { state = state, status = counter_status(state) }
  end,
},
|}

let counter_persistence_v2 =
  {|
persistence = {
  schema = "workload.counter",
  version = 2,
  export = function(call)
    return { count = call.arguments.state.count }
  end,
  import = function(call)
    local source = call.arguments
    if source.from_schema ~= "workload.counter" then
      error("counter schema changed")
    end
    local count = source.state.count
    if source.from_version == 1 then
      count = count + 10
    elseif source.from_version ~= 2 then
      error("counter v2 only imports schema versions 1 or 2")
    end
    local state = { version = 2, count = count }
    return { state = state, status = counter_status(state) }
  end,
},
|}

let counter_persistence_rejected =
  {|
persistence = {
  schema = "workload.counter",
  version = 2,
  export = function(call)
    return call.arguments.state
  end,
  import = function(_) error("counter migration rejected") end,
},
|}

let counter_persistence_effectful =
  {|
persistence = {
  schema = "workload.counter",
  version = 2,
  export = function(call)
    return call.arguments.state
  end,
  import = function(call)
    local state = { version = 2, count = call.arguments.state.count }
    return {
      state = state,
      status = counter_status(state),
      effects = {{ kind = "insert", text = "must-not-commit" }},
    }
  end,
},
|}

let counter_persistence_large_export =
  {|
persistence = {
  schema = "workload.counter",
  version = 1,
  export = function(_) return { data = string.rep("x", 1048577) } end,
  import = function(call)
    return { state = call.arguments.state, status = counter_status(call.arguments.state) }
  end,
},
|}

let counter_persistence_invalid_version =
  {|
persistence = {
  schema = "workload.counter",
  version = 0,
  export = function(call) return call.arguments.state end,
  import = function(call)
    return { state = call.arguments.state, status = counter_status(call.arguments.state) }
  end,
},
|}

let test_script_model_state_persistence_and_migration () =
  let path = Filename.temp_file "zenbu-m9-model" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      let create () =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Script
          ~contents:"alpha" ~config:(Zenbu_scripting.Scripting.Explicit path)
          ~dimensions ()
        |> must
      in
      write path (counter_model_config ~persistence:counter_persistence_v1 1);
      let session =
        create () |> fun session ->
        Zenbu_app.Session.handle_input session (key "a")
      in
      expect
        (Model_status.id (Zenbu_app.Session.status session) = "counter-1")
        "counter v1 did not retain model state before reload";
      write path (counter_model_config ~persistence:counter_persistence_v2 2);
      let session = Zenbu_app.Session.reload_config session in
      expect
        (Model_status.id (Zenbu_app.Session.status session) = "counter-2")
        "successful schema upgrade did not install migrated state";
      let session = Zenbu_app.Session.reload_config session in
      let session = Zenbu_app.Session.handle_input session (key "x") in
      expect
        (Zenbu_app.Session.contents session = "v2:11alpha")
        "same-version reload lost a migrated script state";
      write path (counter_model_config ~persistence:counter_persistence_v1 1);
      let session = Zenbu_app.Session.reload_config session in
      expect
        (Model_status.id (Zenbu_app.Session.status session) = "counter-1")
        "schema downgrade did not restore a v1 state";
      let session = Zenbu_app.Session.handle_input session (key "x") in
      expect
        (Zenbu_app.Session.contents session = "v2:11v1:1alpha")
        "downgraded state was not dispatched through the replacement callback";
      write path
        (counter_model_config ~persistence:counter_persistence_effectful 2);
      let effectful = Zenbu_app.Session.reload_config session in
      let effectful_errors =
        Zenbu_app.Session.inspect effectful Zenbu_app.Session.Scripts
        |> String.concat "\n"
      in
      expect
        (Zenbu_app.Session.contents effectful = "v2:11v1:1alpha"
        && contains effectful_errors "state import must not return effects")
        "an effectful import mutated the document or was not rejected: %s"
        effectful_errors;
      let candidate_binding =
        {|
zenbu.command {
  id = "user.migration-candidate",
  run = function(_) return {{ kind = "insert", text = "candidate" }} end,
}
zenbu.bind { input = "Ctrl-K", command = "user.migration-candidate" }
|}
      in
      write path
        (counter_model_config ~persistence:counter_persistence_rejected
           ~extra:candidate_binding 2);
      let rejected = Zenbu_app.Session.reload_config effectful in
      let errors =
        Zenbu_app.Session.inspect rejected Zenbu_app.Session.Scripts
        |> String.concat "\n"
      in
      expect
        (Model_status.id (Zenbu_app.Session.status rejected) = "counter-1"
        && contains errors "counter migration rejected")
        "rejected migration did not retain an inspectable last-known-good \
         state: %s"
        errors;
      let after_rejected_binding =
        Zenbu_app.Session.handle_input rejected (ctrl "K")
      in
      expect
        (Zenbu_app.Session.contents after_rejected_binding = "v2:11v1:1alpha")
        "a failed migration half-installed the replacement binding";
      let after_rejected_callback =
        Zenbu_app.Session.handle_input after_rejected_binding (key "x")
      in
      expect
        (Zenbu_app.Session.contents after_rejected_callback
        = "v2:11v1:1v1:1alpha")
        "a rejected migration disposed the last healthy callback";
      write path (counter_model_config ~persistence:counter_persistence_v2 2);
      let session = Zenbu_app.Session.reload_config after_rejected_callback in
      let session = Zenbu_app.Session.handle_input session (key "x") in
      expect
        (Zenbu_app.Session.contents session = "v2:11v1:1v1:1v2:11alpha")
        "a retry after a rejected migration did not recover atomically";
      write path (counter_model_config 3);
      let session = Zenbu_app.Session.reload_config session in
      expect
        (Model_status.id (Zenbu_app.Session.status session) = "counter-3")
        "omitting persistence did not preserve the explicit reset behavior";
      let session = Zenbu_app.Session.handle_input session (key "x") in
      expect
        (Zenbu_app.Session.contents session = "v2:11v1:1v1:1v2:11v3:0alpha")
        "disabled persistence did not reset to the replacement initial state";
      write path
        (counter_model_config ~persistence:counter_persistence_large_export 1);
      let bounded = create () in
      write path (counter_model_config ~persistence:counter_persistence_v2 2);
      let bounded = Zenbu_app.Session.reload_config bounded in
      let bounded_errors =
        Zenbu_app.Session.inspect bounded Zenbu_app.Session.Scripts
        |> String.concat "\n"
      in
      expect
        (Model_status.id (Zenbu_app.Session.status bounded) = "counter-1"
        && contains bounded_errors "maximum total string data")
        "oversized exported state was accepted or replaced the live generation";
      write path
        (counter_model_config ~persistence:counter_persistence_invalid_version 1);
      match
        Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
          ~base_semantics:(base_semantics ()) path
      with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "state persistence version")
            "invalid persistence version was not rejected: %s" message
      | Error error ->
          failf "wrong persistence-version validation error: %s"
            (Error.to_string error)
      | Ok _ -> failf "invalid persistence version was accepted")

let test_script_model_state_migration_across_buffers () =
  let path = Filename.temp_file "zenbu-m9-model-buffers" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path (counter_model_config ~persistence:counter_persistence_v1 1);
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Script
          ~contents:"alpha" ~config:(Zenbu_scripting.Scripting.Explicit path)
          ~dimensions ()
        |> must
        |> fun session -> Zenbu_app.Session.handle_input session (key "a")
      in
      let session =
        match
          Zenbu_app.Session.handle_host session Zenbu_app.Session.New_buffer
        with
        | Zenbu_app.Session.Continue session -> session
        | Zenbu_app.Session.Exit _ ->
            failf "new script buffer unexpectedly exited"
      in
      let session = Zenbu_app.Session.handle_input session (key "a") in
      write path (counter_model_config ~persistence:counter_persistence_v2 2);
      let session = Zenbu_app.Session.reload_config session in
      let current = Zenbu_app.Session.handle_input session (key "x") in
      expect
        (Zenbu_app.Session.contents current = "v2:11")
        "active script buffer did not receive migrated state";
      let original =
        match
          Zenbu_app.Session.handle_host current
            Zenbu_app.Session.Previous_buffer
        with
        | Zenbu_app.Session.Continue session -> session
        | Zenbu_app.Session.Exit _ ->
            failf "switching to the original script buffer unexpectedly exited"
      in
      let original = Zenbu_app.Session.handle_input original (key "x") in
      expect
        (Zenbu_app.Session.contents original = "v2:11alpha")
        "inactive script buffer did not receive migrated state")

let test_script_model_value_limits () =
  let path = Filename.temp_file "zenbu-m7-model-limit" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.model {
  id = "workload.cyclic",
  initial_state = {},
  initial_status = { id = "normal", label = "NORMAL" },
  run = function(_)
    local state = {}
    state.self = state
    return {
      state = state,
      status = { id = "normal", label = "NORMAL" },
    }
  end,
}
|};
      let trace = Trace.enabled ~capacity:32 |> must in
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Script
          ~contents:"alpha" ~trace
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions ()
        |> must
      in
      let session = Zenbu_app.Session.handle_input session (key "x") in
      expect
        (Zenbu_app.Session.contents session = "alpha"
        && Model_status.id (Zenbu_app.Session.status session) = "normal")
        "a cyclic script state changed editor state";
      let errors =
        Trace.events trace
        |> List.filter_map (function
          | Trace_event.Error_reported { reason; _ } -> Some reason
          | _ -> None)
        |> String.concat "\n"
      in
      expect
        (contains errors "maximum nesting depth")
        "cyclic script state did not hit the bounded value conversion path")

let test_script_external_filter () =
  let path = Filename.temp_file "zenbu-m7-filter" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.model {
  id = "workload.filter",
  initial_state = {},
  initial_status = { id = "normal", label = "NORMAL" },
  run = function(call)
    local key = call.arguments.input.key
    local effects = {
      {
        kind = "set-selections",
        selections = {{ anchor = 0, head = 5 }, { anchor = 6, head = 10 }},
        primary = 1,
      },
    }
    if key == "f" then
      table.insert(effects, {
        kind = "external-filter",
        program = "/usr/bin/tr",
        arguments = {"a-z", "A-Z"},
      })
    elseif key == "x" then
      table.insert(effects, {
        kind = "external-filter",
        program = "tr",
        arguments = {"a-z", "A-Z"},
      })
    elseif key == "e" then
      table.insert(effects, {
        kind = "external-filter",
        program = "/usr/bin/false",
      })
    end
    return {
      state = {},
      status = { id = "normal", label = "NORMAL" },
      effects = effects,
    }
  end,
}
|};
      let trace = Trace.enabled ~capacity:64 |> must in
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Script
          ~contents:"alpha beta" ~trace
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions ()
        |> must
      in
      let session = Zenbu_app.Session.handle_input session (key "f") in
      expect
        (Zenbu_app.Session.contents session = "ALPHA BETA")
        "external filter did not replace all selected ranges atomically";
      let why =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Why
        |> String.concat "\n"
      in
      expect
        (contains why "workload.filter" && contains why "host.external-filter")
        "external filter transaction did not retain model and host provenance";
      let failed = Zenbu_app.Session.handle_input session (key "x") in
      expect
        (Zenbu_app.Session.contents failed = "ALPHA BETA")
        "a rejected external filter changed the document";
      let scripts =
        Zenbu_app.Session.inspect failed Zenbu_app.Session.Scripts
        |> String.concat "\n"
      in
      expect
        (contains scripts "program must be a non-empty absolute executable path")
        "external filter rejected a relative executable without a useful error";
      let failed = Zenbu_app.Session.handle_input failed (key "e") in
      expect
        (Zenbu_app.Session.contents failed = "ALPHA BETA")
        "a failing external program changed the document";
      let scripts =
        Zenbu_app.Session.inspect failed Zenbu_app.Session.Scripts
        |> String.concat "\n"
      in
      expect
        (contains scripts "/usr/bin/false exited with status 1")
        "external filter did not report the child process failure")

let test_script_background_process () =
  let path = Filename.temp_file "zenbu-m7-background" ".lua" in
  let session = ref None in
  Fun.protect
    ~finally:(fun () ->
      Option.iter Zenbu_app.Session.close !session;
      Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.model {
  id = "workload.background",
  initial_state = {},
  initial_status = { id = "normal", label = "NORMAL" },
  run = function(call)
    local key = call.arguments.input.key
    local effects = {}
    if key == "b" then
      table.insert(effects, {
        kind = "background-process",
        program = "/usr/bin/printf",
        arguments = {"job-output"},
      })
    elseif key == "f" then
      table.insert(effects, {
        kind = "background-process",
        program = "/usr/bin/false",
      })
    elseif key == "r" then
      table.insert(effects, {
        kind = "background-process",
        program = "printf",
      })
    elseif key == "u" then
      table.insert(effects, {
        kind = "background-process",
        program = "/usr/bin/printf",
        arguments = {string.rep("界", 6000)},
      })
    elseif key == "c" then
      table.insert(effects, {
        kind = "background-process",
        program = "/bin/cat",
      })
    elseif key == "s" then
      table.insert(effects, {
        kind = "background-process",
        program = "/bin/sleep",
        arguments = {"5"},
      })
    end
    return {
      state = {},
      status = { id = "normal", label = "NORMAL" },
      effects = effects,
    }
  end,
}
|};
      let started =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Script
          ~contents:"alpha" ~config:(Zenbu_scripting.Scripting.Explicit path)
          ~dimensions ()
        |> must
      in
      session := Some started;
      let started = Zenbu_app.Session.handle_input started (key "b") in
      session := Some started;
      expect
        (Zenbu_app.Session.contents started = "alpha")
        "a background process mutated the document before completion";
      let wakeup =
        match Zenbu_app.Session.background_job_wakeup_fd started with
        | Some wakeup -> wakeup
        | None ->
            failf "background process did not allocate a wakeup descriptor"
      in
      expect
        (List.mem wakeup (Zenbu_app.Session.wakeup_fds started))
        "the terminal wakeup set did not include the background job descriptor";
      let finished = wait_for_background_job started "stdout: job-output" in
      session := Some finished;
      expect
        (Zenbu_app.Session.contents finished = "alpha")
        "a completed background process mutated the document";
      let output_buffer =
        invoke_palette_text_argument finished "process.job.open-output" "1"
      in
      expect
        (Zenbu_app.Session.buffer_count output_buffer = 2)
        "opening a background job report did not create a workspace buffer";
      expect
        (Zenbu_app.Session.contents output_buffer = "job-output")
        "the completed background-job output buffer did not retain stdout";
      expect
        (Zenbu_app.Session.filename output_buffer = "*job 1 output*")
        "the completed background-job output buffer was not named";
      expect
        (not (Zenbu_app.Session.dirty output_buffer))
        "opening a background-job output buffer marked the workspace dirty";
      let finished =
        match
          Zenbu_app.Session.handle_host output_buffer
            Zenbu_app.Session.Previous_buffer
        with
        | Zenbu_app.Session.Continue session -> session
        | Zenbu_app.Session.Exit _ ->
            failf "switching back from job output exited"
      in
      expect
        (Zenbu_app.Session.contents finished = "alpha")
        "opening job output replaced the original editing buffer";
      let finished = Zenbu_app.Session.handle_input finished (key "f") in
      session := Some finished;
      let failed = wait_for_background_job finished "exited with status 1" in
      session := Some failed;
      expect
        (Zenbu_app.Session.contents failed = "alpha")
        "a failed background process mutated the document";
      let failure_report =
        Zenbu_app.Session.open_background_job_output failed ~job_id:2
      in
      expect
        (contains
           (Zenbu_app.Session.contents failure_report)
           "background job 2 failed: exited with status 1")
        "the failed background-job report omitted the failure diagnosis";
      let success_report =
        match
          Zenbu_app.Session.handle_host failure_report
            Zenbu_app.Session.Previous_buffer
        with
        | Zenbu_app.Session.Continue session -> session
        | Zenbu_app.Session.Exit _ ->
            failf "switching back from failure report exited"
      in
      let failed =
        match
          Zenbu_app.Session.handle_host success_report
            Zenbu_app.Session.Previous_buffer
        with
        | Zenbu_app.Session.Continue session -> session
        | Zenbu_app.Session.Exit _ ->
            failf "switching back from success report exited"
      in
      expect
        (Zenbu_app.Session.contents failed = "alpha")
        "opening a failure report lost the original editing buffer";
      let rejected = Zenbu_app.Session.handle_input failed (key "r") in
      session := Some rejected;
      let scripts =
        Zenbu_app.Session.inspect rejected Zenbu_app.Session.Scripts
        |> String.concat "\n"
      in
      expect
        (contains scripts "program must be a non-empty absolute executable path")
        "background process accepted a relative executable path";
      let jobs =
        Zenbu_app.Session.inspect rejected Zenbu_app.Session.Jobs
        |> String.concat "\n"
      in
      expect
        (contains jobs "1: /usr/bin/printf" && contains jobs "2: /usr/bin/false")
        "background jobs were not retained for inspection";
      let unicode = Zenbu_app.Session.handle_input rejected (key "u") in
      session := Some unicode;
      let unicode = wait_for_background_job unicode "界" in
      session := Some unicode;
      let rendered_jobs =
        Zenbu_app.Session.inspect unicode Zenbu_app.Session.Jobs
        |> String.concat "\n"
      in
      expect
        (Result.is_ok (Text_buffer.of_utf8 rendered_jobs))
        "a bounded Unicode background-output preview broke UTF-8";
      let no_stdin = Zenbu_app.Session.handle_input unicode (key "c") in
      session := Some no_stdin;
      let no_stdin = wait_for_background_job no_stdin "/bin/cat succeeded" in
      session := Some no_stdin;
      expect
        (Zenbu_app.Session.contents no_stdin = "alpha")
        "a no-stdin background process mutated the document";
      let slow = Zenbu_app.Session.handle_input no_stdin (key "s") in
      session := Some slow;
      let cancelled = Zenbu_app.Session.cancel_background_job slow ~job_id:5 in
      session := Some cancelled;
      let jobs =
        Zenbu_app.Session.inspect cancelled Zenbu_app.Session.Jobs
        |> String.concat "\n"
      in
      expect
        (contains jobs "5: /bin/sleep 5 cancelled")
        "host cancellation did not mark a running background job cancelled";
      let cancellation_report =
        Zenbu_app.Session.open_background_job_output cancelled ~job_id:5
      in
      expect
        (Zenbu_app.Session.contents cancellation_report
        = "background job 5 was cancelled")
        "the cancelled background-job report was not available as a buffer";
      expect
        (List.exists
           (fun descriptor ->
             Command_descriptor.id descriptor
             |> Command_id.to_string
             |> String.equal "process.job.cancel")
           (Zenbu_app.Session.host_command_descriptors ()))
        "background-job cancellation is not discoverable through the host \
         palette";
      expect
        (List.exists
           (fun descriptor ->
             Command_descriptor.id descriptor
             |> Command_id.to_string
             |> String.equal "process.job.open-output")
           (Zenbu_app.Session.host_command_descriptors ()))
        "opening background-job output is not discoverable through the host \
         palette")

let test_errors_and_registration_conflicts () =
  let path = Filename.temp_file "zenbu-m7-errors" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path "local =";
      (match
         Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
           ~base_semantics:(base_semantics ()) path
       with
      | Error (Error.Script_error { source = Some source; line = Some line; _ })
        ->
          expect (source = path) "Lua parse error reported source %s" source;
          expect (line = 1) "Lua parse error reported line %d" line
      | Error error ->
          failf "missing structured Lua location: %s" (Error.to_string error)
      | Ok _ -> failf "invalid Lua configuration was accepted");
      write path
        {|
zenbu.command { id = "user.one", run = function(_) return nil end }
zenbu.bind { input = "Ctrl-K", command = "user.one" }
zenbu.bind { input = "Ctrl-K", command = "user.one" }
|};
      (match
         Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
           ~base_semantics:(base_semantics ()) path
       with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "duplicate binding")
            "duplicate binding error is not actionable"
      | Error error ->
          failf "wrong duplicate binding error: %s" (Error.to_string error)
      | Ok _ -> failf "duplicate script binding was accepted");
      write path
        {|
zenbu.command { id = "user.save", run = function(_) return nil end }
zenbu.bind { input = "Ctrl-S", command = "user.save" }
|};
      (match
         Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
           ~base_semantics:(base_semantics ()) path
       with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "reserved host")
            "reserved host binding error is not actionable"
      | Error error ->
          failf "wrong reserved host binding error: %s" (Error.to_string error)
      | Ok _ -> failf "reserved host binding was accepted");
      write path
        {|
zenbu.command {
  id = "user.invalid-parameter",
  parameters = {
    { name = "value", description = "Invalid parameter kind.", kind = "unknown" },
  },
  run = function(_) return nil end,
}
|};
      match
        Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
          ~base_semantics:(base_semantics ()) path
      with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "unknown command parameter kind")
            "invalid parameter kind error is not actionable"
      | Error error ->
          failf "wrong parameter kind error: %s" (Error.to_string error)
      | Ok _ -> failf "invalid parameter kind was accepted")

let sequence_config =
  {|
zenbu.command {
  id = "user.global-sequence", run = function(_) return {{ kind = "insert", text = "G" }} end,
}
zenbu.command {
  id = "user.model-sequence", run = function(_) return {{ kind = "insert", text = "M" }} end,
}
zenbu.command {
  id = "user.shift-sequence", run = function(_) return {{ kind = "insert", text = "S" }} end,
}
zenbu.bind { input = "Ctrl-X Ctrl-K", command = "user.global-sequence", scope = "global" }
zenbu.bind { input = "Ctrl-X Ctrl-M", command = "user.model-sequence", scope = "model:zenbu.vim-style" }
zenbu.bind { input = "Ctrl-Shift-K", command = "user.shift-sequence", scope = "global" }
|}

let test_binding_sequences () =
  let path = Filename.temp_file "zenbu-m7-sequences" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path sequence_config;
      let create () =
        let trace = Trace.enabled ~capacity:64 |> must in
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~trace ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions
          ()
        |> must
      in
      let prefix = Zenbu_app.Session.handle_input (create ()) (ctrl "X") in
      expect
        (Zenbu_app.Session.contents prefix = "alpha")
        "the first event of a binding sequence edited the document";
      let prefix_view =
        Zenbu_app.Session.inspect prefix Zenbu_app.Session.Bindings
        |> String.concat "\n"
      in
      expect
        (contains prefix_view "Ctrl+text(x) Ctrl+text(k)")
        "binding inspection did not display the complete sequence";
      let global = Zenbu_app.Session.handle_input prefix (ctrl "K") in
      expect
        (Zenbu_app.Session.contents global = "Galpha")
        "a global binding sequence did not resolve after a more-specific prefix";
      let why =
        Zenbu_app.Session.inspect global Zenbu_app.Session.Why
        |> String.concat "\n"
      in
      expect
        (contains why
           "binding Ctrl+text(x) Ctrl+text(k) -> user.global-sequence")
        "binding provenance did not retain the complete input sequence: %s" why;
      let model =
        create () |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl "X") |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl "M")
      in
      expect
        (Zenbu_app.Session.contents model = "Malpha")
        "a model-scoped binding sequence did not resolve over the global scope";
      let rejected =
        create () |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl "X") |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl "L")
      in
      expect
        (Zenbu_app.Session.contents rejected = "alpha")
        "an unbound suffix leaked into the model after a binding prefix";
      let cancelled =
        create () |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl "X") |> fun session ->
        Zenbu_app.Session.handle_input session
          (Input_event.key_press (Input_event.named_key Input_event.Escape))
        |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl "X") |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl "K")
      in
      expect
        (Zenbu_app.Session.contents cancelled = "Galpha")
        "Escape did not cancel a pending sequence before the next binding";
      let shifted =
        create () |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl_shift "K")
      in
      expect
        (Zenbu_app.Session.contents shifted = "Salpha")
        "a multi-modifier binding did not match the parsed input";
      write path
        {|
zenbu.command { id = "user.one", run = function(_) return nil end }
zenbu.bind { input = "Ctrl-X Ctrl-K", command = "user.one" }
zenbu.bind { input = "Ctrl-X Ctrl-K Ctrl-M", command = "user.one" }
|};
      (match
         Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
           ~base_semantics:(base_semantics ()) path
       with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "prefix-ambiguous")
            "prefix-conflicting binding sequences did not fail staging"
      | Error error ->
          failf "wrong prefix-conflict error: %s" (Error.to_string error)
      | Ok _ -> failf "prefix-conflicting binding sequences were accepted");
      write path
        {|
zenbu.command { id = "user.one", run = function(_) return nil end }
zenbu.bind { input = "Ctrl-X Ctrl-S", command = "user.one" }
|};
      match
        Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
          ~base_semantics:(base_semantics ()) path
      with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "reserved host")
            "a host-reserved suffix was accepted inside a binding sequence"
      | Error error ->
          failf "wrong reserved-suffix error: %s" (Error.to_string error)
      | Ok _ -> failf "host-reserved sequence suffix was accepted")

let scoped_config =
  {|
zenbu.command {
  id = "user.global", run = function(_) return {{ kind = "insert", text = "G" }} end,
}
zenbu.command {
  id = "user.specific", run = function(_) return {{ kind = "insert", text = "V" }} end,
}
zenbu.bind { input = "Ctrl-K", command = "user.global", scope = "global" }
zenbu.bind { input = "Ctrl-K", command = "user.specific", scope = "model:zenbu.vim-style:normal" }
|}

let test_scopes_atomicity_and_undo () =
  let path = Filename.temp_file "zenbu-m7-scopes" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path scoped_config;
      let create model =
        Zenbu_app.Session.create ~model ~contents:"alpha"
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions ()
        |> must
      in
      let vim =
        create Zenbu_app.Session.Vim |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl "K")
      in
      expect
        (Zenbu_app.Session.contents vim = "Valpha")
        "model-status script binding did not override global binding";
      let selection =
        create Zenbu_app.Session.Selection |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl "K")
      in
      expect
        (Zenbu_app.Session.contents selection = "Galpha")
        "global script binding did not fall back for another model";
      let vim =
        Zenbu_app.Session.handle_input vim
          (Input_event.key_press (Input_event.named_key Input_event.Escape))
      in
      let vim =
        Zenbu_app.Session.handle_input vim
          (Input_event.logical_text "u" |> must |> Input_event.key_press)
      in
      expect
        (Zenbu_app.Session.contents vim = "alpha")
        "undo did not restore a script-originated transaction";
      write path
        {|
zenbu.selector {
  id = "user.document", run = function(call)
    return { selections = {{ anchor = 0, head = call.context.document.length }}, primary = 1 }
  end,
}
zenbu.transform {
  id = "user.invalid", run = function(_) return { edits = {{ start = -1, stop = 0, text = "x" }} } end,
}
zenbu.command {
  id = "user.invalid-apply", run = function(_)
    return {{ kind = "apply", selector = "user.document", transformation = "user.invalid" }}
  end,
}
zenbu.bind { input = "Ctrl-K", command = "user.invalid-apply" }
|};
      let invalid =
        create Zenbu_app.Session.Vim |> fun session ->
        Zenbu_app.Session.handle_input session (ctrl "K")
      in
      expect
        (Zenbu_app.Session.contents invalid = "alpha")
        "invalid script transformation partially mutated the document")

let binding_layer_config =
  {|
zenbu.command {
  id = "user.global", run = function(_) return {{ kind = "insert", text = "G" }} end,
}
zenbu.command {
  id = "user.major", run = function(_) return {{ kind = "insert", text = "A" }} end,
}
zenbu.command {
  id = "user.high", run = function(_) return {{ kind = "insert", text = "B" }} end,
}
zenbu.command {
  id = "user.conflict", run = function(_) return {{ kind = "insert", text = "C" }} end,
}
zenbu.binding_layer {
  id = "user.major", title = "Major", description = "Primary map.", priority = 10,
}
zenbu.binding_layer {
  id = "user.high", title = "High", description = "Higher-priority map.", priority = 20,
}
zenbu.binding_layer {
  id = "user.conflict", title = "Conflict", description = "Equal-priority collision.", priority = 10,
}
zenbu.bind { input = "Ctrl-K", command = "user.global" }
zenbu.bind { input = "Ctrl-K", command = "user.major", layer = "user.major" }
zenbu.bind { input = "Ctrl-X", command = "user.major", layer = "user.major" }
zenbu.bind { input = "Ctrl-K", command = "user.high", layer = "user.high" }
zenbu.bind { input = "Ctrl-X Ctrl-K", command = "user.high", layer = "user.high" }
zenbu.bind { input = "Ctrl-K", command = "user.conflict", layer = "user.conflict" }
|}

let binding_layer_reload_config =
  {|
zenbu.command {
  id = "user.global", run = function(_) return {{ kind = "insert", text = "G" }} end,
}
zenbu.command {
  id = "user.major", run = function(_) return {{ kind = "insert", text = "A" }} end,
}
zenbu.binding_layer {
  id = "user.major", title = "Major", description = "Primary map.", priority = 10,
}
zenbu.bind { input = "Ctrl-K", command = "user.global" }
zenbu.bind { input = "Ctrl-K", command = "user.major", layer = "user.major" }
|}

let test_dynamic_binding_layers () =
  let path = Filename.temp_file "zenbu-m12-layers" ".lua" in
  Fun.protect
    ~finally:(fun () -> try Sys.remove path with Sys_error _ -> ())
    (fun () ->
      write path binding_layer_config;
      let trace = Trace.enabled ~capacity:128 |> must in
      let create () =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~trace ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions
          ()
        |> must
      in
      let session = create () in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      expect
        (Zenbu_app.Session.contents session = "Galpha")
        "an inactive binding layer intercepted the base binding";
      let session =
        invoke_palette_text_argument session "keymap.layer.enable" "user.major"
      in
      let enabled_bindings =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Bindings
        |> String.concat "\n"
      in
      expect
        (contains enabled_bindings
           "binding layer: user.major (priority 10; enabled")
        "the palette did not enable the requested binding layer: %s"
        enabled_bindings;
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      let why_after_enable =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Why
        |> String.concat "\n"
      in
      expect
        (Zenbu_app.Session.contents session = "GAalpha")
        "an enabled layer did not override the base binding: %s\n%s"
        enabled_bindings why_after_enable;
      let bindings =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Bindings
        |> String.concat "\n"
      in
      expect
        (contains bindings "binding layer: user.major (priority 10; enabled"
        && contains bindings "layer user.high (disabled)")
        "binding inspection did not expose lifecycle state: %s" bindings;
      let session =
        Zenbu_app.Session.enable_binding_layer session ~id:"user.high"
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      expect
        (Zenbu_app.Session.contents session = "GABalpha")
        "a higher-priority layer did not win deterministically";
      let why =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Why
        |> String.concat "\n"
      in
      expect
        (contains why "layer=user.high")
        "why inspection did not retain the resolved layer: %s" why;
      let prefixed = Zenbu_app.Session.handle_input session (ctrl "X") in
      let cancelled =
        Zenbu_app.Session.handle_input prefixed
          (Input_event.key_press (Input_event.named_key Input_event.Escape))
      in
      expect
        (Zenbu_app.Session.contents cancelled = "GABalpha")
        "Escape did not cancel a higher-priority layer prefix";
      let session =
        Zenbu_app.Session.disable_binding_layer cancelled ~id:"user.high"
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "X") in
      expect
        (Zenbu_app.Session.contents session = "GABAalpha")
        "disabling a layer did not restore the lower-priority complete binding";
      let session =
        Zenbu_app.Session.enable_binding_layer session ~id:"user.conflict"
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      expect
        (Zenbu_app.Session.contents session = "GABAAalpha")
        "an equal-priority overlapping layer was not rejected atomically";
      let bindings =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Bindings
        |> String.concat "\n"
      in
      expect
        (contains bindings "binding layer: user.conflict (priority 10; disabled")
        "rejected layer activation became active: %s" bindings;
      let fresh =
        match
          Zenbu_app.Session.handle_host session Zenbu_app.Session.New_buffer
        with
        | Zenbu_app.Session.Continue session -> session
        | Zenbu_app.Session.Exit _ -> failf "new buffer unexpectedly exited"
      in
      let fresh = Zenbu_app.Session.handle_input fresh (ctrl "K") in
      expect
        (Zenbu_app.Session.contents fresh = "G")
        "binding layers were not buffer-local";
      let reload = create () in
      let reload =
        Zenbu_app.Session.enable_binding_layer reload ~id:"user.major"
      in
      let reload =
        Zenbu_app.Session.enable_binding_layer reload ~id:"user.high"
      in
      write path binding_layer_reload_config;
      let reload = Zenbu_app.Session.reload_config reload in
      let reload = Zenbu_app.Session.handle_input reload (ctrl "K") in
      expect
        (Zenbu_app.Session.contents reload = "Aalpha")
        "reload did not retain a valid layer while unloading a stale provider \
         layer";
      write path
        {|
zenbu.command { id = "user.one", run = function(_) return nil end }
zenbu.binding_layer { id = "user.one", priority = 0 }
|};
      match
        Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
          ~base_semantics:(base_semantics ()) path
      with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "priority")
            "out-of-range binding-layer priority was accepted: %s" message
      | Error error ->
          failf "wrong binding-layer validation error: %s"
            (Error.to_string error)
      | Ok _ -> failf "out-of-range binding-layer priority was accepted")

let test_syntax_api_and_reload_stress () =
  let path = Filename.temp_file "zenbu-m7-syntax" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.selector {
  id = "user.syntax-node", title = "Syntax node", requires_syntax = true,
  run = function(call)
    local syntax = zenbu.syntax()
    if syntax == nil or syntax.language ~= "ocaml" or syntax.node == nil then error("syntax unavailable") end
    return { selections = {{ anchor = syntax.node.start, head = syntax.node.stop }}, primary = 1 }
  end,
}
zenbu.command {
  id = "user.inspect-syntax", run = function(_)
    return {{ kind = "apply", selector = "user.syntax-node", transformation = "select" }}
  end,
}
zenbu.bind { input = "Ctrl-K", command = "user.inspect-syntax" }
|};
      let generation =
        Zenbu_scripting.Scripting.load ~generation_id:1
          ~base_commands:(base_commands ()) ~base_semantics:(base_semantics ())
          (Zenbu_scripting.Scripting.Explicit path)
        |> must |> Option.get
      in
      expect
        (List.exists Zenbu_kernel.Semantic_descriptor.requires_syntax
           (Zenbu_scripting.Scripting.descriptors generation))
        "requires_syntax did not survive Lua descriptor registration";
      Zenbu_scripting.Scripting.dispose generation;
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Structural
          ~language:"ocaml" ~contents:"let alpha = 1\n"
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions ()
        |> must
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      let selections =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Selection_view
        |> String.concat "\n"
      in
      expect
        (contains selections "primary")
        "script syntax selector did not run through the public syntax view";
      let reloaded = ref session in
      for iteration = 1 to 100 do
        write path
          (if iteration mod 2 = 0 then scoped_config
           else String.concat "\n" [ scoped_config; "-- reload generation" ]);
        reloaded := Zenbu_app.Session.reload_config !reloaded
      done)

let test_event_delivery_and_recursion_guard () =
  let config_path = Filename.temp_file "zenbu-m7-events" ".lua" in
  let file_path = Filename.temp_file "zenbu-m7-events-buffer" ".txt" in
  Fun.protect
    ~finally:(fun () ->
      Sys.remove config_path;
      Sys.remove file_path)
    (fun () ->
      write config_path
        {|
zenbu.command {
  id = "user.change", run = function(_) return {{ kind = "insert", text = "x" }} end,
}
zenbu.bind { input = "Ctrl-K", command = "user.change" }
zenbu.on {
  event = "document-changed", run = function(_)
    return {{ kind = "insert", text = "!" }}
  end,
}
zenbu.on {
  event = "after-save", run = function(call)
    return {{ kind = "message", text = "saved: " .. call.arguments.event }}
  end,
}
|};
      write file_path "alpha";
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~file_path ~config:(Zenbu_scripting.Scripting.Explicit config_path)
          ~dimensions ()
        |> must
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "K") in
      expect
        (Zenbu_app.Session.contents session = "x!alpha")
        "document-change hook did not run once with same-event recursion \
         suppressed";
      let session =
        match Zenbu_app.Session.handle_host session Zenbu_app.Session.Save with
        | Zenbu_app.Session.Continue session -> session
        | Zenbu_app.Session.Exit _ -> failf "save unexpectedly exited"
      in
      let scripts =
        Zenbu_app.Session.inspect session Zenbu_app.Session.Scripts
        |> String.concat "\n"
      in
      expect
        (contains scripts "saved: after-save")
        "after-save event did not receive its documented callback argument")

let test_mode_transition_validation () =
  let path = Filename.temp_file "zenbu-m7-mode-validation" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.command { id = "user.enter", run = function(_) return {} end }
zenbu.bind {
  input = "Ctrl-X", command = "user.enter",
  mode = { action = "push", id = "user.undeclared" },
}
|};
      match
        Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
          ~base_semantics:(base_semantics ()) path
      with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "binding mode transition")
            "an undeclared mode transition was accepted"
      | Error error ->
          failf "wrong mode-transition validation error: %s"
            (Error.to_string error)
      | Ok _ -> failf "an undeclared mode transition was accepted")

let test_text_binding_validation () =
  let path = Filename.temp_file "zenbu-m7-text-validation" ".lua" in
  let check () =
    Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
      ~base_semantics:(base_semantics ()) path
  in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.mode { id = "user.insert", input_mode = "text" }
zenbu.command { id = "user.insert", run = function(_) return {} end }
zenbu.bind { input = "<text>", command = "user.insert", scope = "mode:user.insert" }
|};
      (match check () with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "requires text_argument")
            "a text wildcard without an argument target was accepted"
      | Error error ->
          failf "wrong missing text-argument error: %s" (Error.to_string error)
      | Ok _ -> failf "a text wildcard without an argument target was accepted");
      write path
        {|
zenbu.mode { id = "user.insert", input_mode = "text" }
zenbu.command { id = "user.insert", run = function(_) return {} end }
zenbu.bind {
  input = "<text>", command = "user.insert", text_argument = "text",
  scope = "mode:user.insert",
}
|};
      (match check () with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "declared text command parameter")
            "a text argument not declared by its command was accepted"
      | Error error ->
          failf "wrong undeclared text-parameter error: %s"
            (Error.to_string error)
      | Ok _ -> failf "a text argument not declared by its command was accepted");
      write path
        {|
zenbu.command {
  id = "user.insert",
  parameters = {{ name = "text", description = "Committed text.", kind = "text" }},
  run = function(_) return {} end,
}
zenbu.bind { input = "<text>", command = "user.insert", text_argument = "text" }
|};
      match check () with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "requires a mode with input_mode = text")
            "a text wildcard outside a text-entry custom mode was accepted"
      | Error error ->
          failf "wrong text-mode-scope error: %s" (Error.to_string error)
      | Ok _ ->
          failf "a text wildcard outside a text-entry custom mode was accepted")

let test_custom_text_entry_binding () =
  let path = Filename.temp_file "zenbu-m7-text-mode" ".lua" in
  let host session command =
    match Zenbu_app.Session.handle_host session command with
    | Zenbu_app.Session.Continue session -> session
    | Zenbu_app.Session.Exit _ -> failf "workspace command unexpectedly exited"
  in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.mode {
  id = "user.insert",
  title = "INSERT",
  description = "A script-defined committed-text mode.",
  input_mode = "text",
}
zenbu.command { id = "user.enter-insert", run = function(_) return {} end }
zenbu.command { id = "user.leave-insert", run = function(_) return {} end }
zenbu.command {
  id = "user.insert-text",
  parameters = {
    { name = "text", description = "Committed text.", kind = "text" },
  },
  run = function(call)
    return {{ kind = "insert", text = call.arguments.text }}
  end,
}
zenbu.bind { input = "Ctrl-X", command = "user.enter-insert", mode = "user.insert" }
zenbu.bind {
  input = "<text>", command = "user.insert-text", text_argument = "text",
  scope = "mode:user.insert",
}
zenbu.bind {
  input = "Escape", command = "user.leave-insert", scope = "mode:user.insert",
  mode = "",
}
|};
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions ()
        |> must
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "X") in
      expect
        (Model_status.input_mode (Zenbu_app.Session.status session)
        = Model_status.Text_entry)
        "a text-entry custom mode did not publish text-entry input disposition";
      let session = host session Zenbu_app.Session.New_buffer in
      expect
        (Model_status.id (Zenbu_app.Session.status session) = "normal")
        "a new buffer inherited the previous buffer's custom mode stack";
      let session = host session Zenbu_app.Session.Previous_buffer in
      expect
        (Model_status.id (Zenbu_app.Session.status session)
        = "host-custom-mode:user.insert")
        "switching buffers did not restore the originating buffer's custom mode";
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.text_input "界🙂" |> must)
      in
      expect
        (Zenbu_app.Session.contents session = "界🙂alpha"
        && Model_status.id (Zenbu_app.Session.status session)
           = "host-custom-mode:user.insert")
        "a committed Unicode text binding did not forward its argument \
         atomically";
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.key_press (Input_event.named_key Input_event.Escape))
      in
      expect
        (Model_status.id (Zenbu_app.Session.status session) = "normal")
        "a text-entry custom mode did not return to its base model")

let test_initial_custom_mode () =
  let path = Filename.temp_file "zenbu-m7-initial-mode" ".lua" in
  let host session command =
    match Zenbu_app.Session.handle_host session command with
    | Zenbu_app.Session.Continue session -> session
    | Zenbu_app.Session.Exit _ -> failf "workspace command unexpectedly exited"
  in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.mode {
  id = "user.default",
  title = "DEFAULT",
  description = "The adapter's default editing map.",
  initial = true,
}
zenbu.command {
  id = "user.type", run = function(_) return {{ kind = "insert", text = "T" }} end,
}
zenbu.bind { input = "t", command = "user.type", scope = "mode:user.default" }
|};
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions ()
        |> must
      in
      expect
        (Model_status.id (Zenbu_app.Session.status session)
        = "host-custom-mode:user.default")
        "an initial custom mode did not own the first buffer's default status";
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.logical_text "t" |> must |> Input_event.key_press)
      in
      expect
        (Zenbu_app.Session.contents session = "Talpha")
        "the initial custom mode did not receive ordinary input";
      let session = host session Zenbu_app.Session.New_buffer in
      expect
        (Model_status.id (Zenbu_app.Session.status session)
        = "host-custom-mode:user.default")
        "a new buffer did not receive the declared initial custom mode")

let test_initial_mode_validation () =
  let path = Filename.temp_file "zenbu-m7-initial-validation" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      write path
        {|
zenbu.mode { id = "user.one", initial = true }
zenbu.mode { id = "user.two", initial = true }
|};
      match
        Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
          ~base_semantics:(base_semantics ()) path
      with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "only one custom mode")
            "multiple initial custom modes were accepted"
      | Error error ->
          failf "wrong initial-mode validation error: %s"
            (Error.to_string error)
      | Ok _ -> failf "multiple initial custom modes were accepted")

let test_declared_modes_and_modal_bindings () =
  let path = Filename.temp_file "zenbu-m7-modes" ".lua" in
  Fun.protect
    ~finally:(fun () -> Sys.remove path)
    (fun () ->
      let config =
        {|
zenbu.mode {
  id = "user.leader",
  title = "LEADER",
  description = "A transient custom keymap.",
}
zenbu.mode {
  id = "user.goto",
  title = "GOTO",
  description = "A nested transient keymap.",
}
zenbu.command { id = "user.enter-leader", run = function(_) return {} end }
zenbu.command { id = "user.enter-goto", run = function(_) return {} end }
zenbu.command {
  id = "user.insert-leader", run = function(_) return {{ kind = "insert", text = "L" }} end,
}
zenbu.command {
  id = "user.insert-goto", run = function(_) return {{ kind = "insert", text = "G" }} end,
}
zenbu.bind { input = "Ctrl-X", command = "user.enter-leader", mode = "user.leader" }
zenbu.bind {
  input = "g", command = "user.enter-goto", scope = "mode:user.leader",
  mode = { action = "push", id = "user.goto" },
}
zenbu.bind { input = "l", command = "user.insert-leader", scope = "mode:user.leader", mode = "" }
zenbu.bind {
  input = "h", command = "user.insert-goto", scope = "mode:user.goto",
  mode = { action = "pop" },
}
|}
      in
      write path config;
      let session =
        Zenbu_app.Session.create ~model:Zenbu_app.Session.Vim ~contents:"alpha"
          ~config:(Zenbu_scripting.Scripting.Explicit path) ~dimensions ()
        |> must
      in
      let session = Zenbu_app.Session.handle_input session (ctrl "X") in
      expect
        (Model_status.id (Zenbu_app.Session.status session)
        = "host-custom-mode:user.leader")
        "modal binding did not enter the declared mode";
      let rejected =
        Zenbu_app.Session.handle_input session
          (Input_event.logical_text "z" |> must |> Input_event.key_press)
      in
      expect
        (Zenbu_app.Session.contents rejected = "alpha")
        "an unmatched custom-mode input leaked into the base model";
      let session = Zenbu_app.Session.handle_input session (ctrl "X") in
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.logical_text "g" |> must |> Input_event.key_press)
      in
      expect
        (Model_status.id (Zenbu_app.Session.status session)
        = "host-custom-mode:user.goto")
        "a push transition did not enter the nested custom mode";
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.logical_text "l" |> must |> Input_event.key_press)
      in
      expect
        (Zenbu_app.Session.contents session = "Lalpha"
        && Model_status.id (Zenbu_app.Session.status session) = "normal")
        "a lower custom mode did not remain available beneath a nested mode";
      let session = Zenbu_app.Session.handle_input session (ctrl "X") in
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.logical_text "g" |> must |> Input_event.key_press)
      in
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.logical_text "h" |> must |> Input_event.key_press)
      in
      expect
        (Zenbu_app.Session.contents session = "LGalpha"
        && Model_status.id (Zenbu_app.Session.status session)
           = "host-custom-mode:user.leader")
        "a pop transition did not restore the containing custom mode";
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.logical_text "g" |> must |> Input_event.key_press)
      in
      let session = Zenbu_app.Session.reload_config session in
      expect
        (Model_status.id (Zenbu_app.Session.status session)
        = "host-custom-mode:user.goto")
        "reload did not retain a stack whose modes are still declared";
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.key_press (Input_event.named_key Input_event.Escape))
      in
      expect
        (Model_status.id (Zenbu_app.Session.status session)
        = "host-custom-mode:user.leader")
        "an unmatched Escape did not pop the innermost custom mode";
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.logical_text "l" |> must |> Input_event.key_press)
      in
      expect
        (Zenbu_app.Session.contents session = "LGLalpha"
        && Model_status.id (Zenbu_app.Session.status session) = "normal")
        "a resumed lower custom mode did not remain active after Escape";
      let session = Zenbu_app.Session.handle_input session (ctrl "X") in
      let session =
        Zenbu_app.Session.handle_input session
          (Input_event.logical_text "g" |> must |> Input_event.key_press)
      in
      write path
        "zenbu.command { id = 'user.noop', run = function(_) return {} end }";
      let session = Zenbu_app.Session.reload_config session in
      expect
        (Model_status.id (Zenbu_app.Session.status session) = "normal")
        "reload retained a mode no longer declared by the staged generation")

let tests =
  [
    ( "script load/reload and dynamic semantics",
      test_load_reload_and_dynamic_semantics );
    ("script config validation", test_config_validation);
    ( "script-owned model state and reload",
      test_script_owned_model_state_and_reload );
    ( "script-model state persistence and migration",
      test_script_model_state_persistence_and_migration );
    ( "script-model migration across buffers",
      test_script_model_state_migration_across_buffers );
    ("script-model value conversion limits", test_script_model_value_limits);
    ("script external filter", test_script_external_filter);
    ("script background process", test_script_background_process);
    ( "script errors and registration conflicts",
      test_errors_and_registration_conflicts );
    ("script binding sequences and scoped dispatch", test_binding_sequences);
    ("script scopes, atomicity, and undo", test_scopes_atomicity_and_undo);
    ("dynamic binding layers", test_dynamic_binding_layers);
    ("script syntax API and reload stress", test_syntax_api_and_reload_stress);
    ( "script event delivery and recursion guard",
      test_event_delivery_and_recursion_guard );
    ("mode transition validation", test_mode_transition_validation);
    ("text binding validation", test_text_binding_validation);
    ("custom text-entry binding", test_custom_text_entry_binding);
    ("initial custom mode", test_initial_custom_mode);
    ("initial mode validation", test_initial_mode_validation);
    ("declared modes and modal bindings", test_declared_modes_and_modal_bindings);
  ]

let () =
  List.iter
    (fun (name, test) ->
      test ();
      print_endline ("ok - " ^ name))
    tests
