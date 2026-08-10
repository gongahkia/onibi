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
      | Error (Error.Script_error { source = Some source; line = Some _; _ }) ->
          expect (contains source path) "Lua parse error reported source %s"
            source
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
      match
        Zenbu_scripting.Scripting.check_file ~base_commands:(base_commands ())
          ~base_semantics:(base_semantics ()) path
      with
      | Error (Error.Script_error { phase = "registration"; message; _ }) ->
          expect
            (contains message "reserved host")
            "reserved host binding error is not actionable"
      | Error error ->
          failf "wrong reserved host binding error: %s" (Error.to_string error)
      | Ok _ -> failf "reserved host binding was accepted")

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

let tests =
  [
    ( "script load/reload and dynamic semantics",
      test_load_reload_and_dynamic_semantics );
    ("script config validation", test_config_validation);
    ( "script errors and registration conflicts",
      test_errors_and_registration_conflicts );
    ("script scopes, atomicity, and undo", test_scopes_atomicity_and_undo);
    ("script syntax API and reload stress", test_syntax_api_and_reload_stress);
    ( "script event delivery and recursion guard",
      test_event_delivery_and_recursion_guard );
  ]

let () =
  List.iter
    (fun (name, test) ->
      test ();
      print_endline ("ok - " ^ name))
    tests
