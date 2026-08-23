open Ctypes
open Foreign
module Error = Zenbu_kernel.Error
module Value = Zenbu_model_api.Extension_value
module Host = Zenbu_model_api.Extension_host
module Descriptor = Zenbu_model_api.Command_descriptor

type callback = int

type descriptor = {
  id : string;
  title : string;
  description : string;
  requires_syntax : bool;
  parameters : Descriptor.parameter list;
}

type binding = {
  input : string;
  command : string;
  scope : string option;
  mode_transition : mode_transition option;
}

and mode_transition =
  | Replace_mode of string
  | Push_mode of string
  | Pop_mode
  | Clear_modes

type hook = { event : string; callback : callback }

type registration =
  | Command of descriptor * callback
  | Selector of descriptor * callback
  | Transformation of descriptor * callback
  | Mode of descriptor
  | Binding of binding
  | Hook of hook

type state = unit ptr

module Callback =
  (val dynamic_funptr ~runtime_lock:true (ptr void @-> returning int))

let lua_library_candidates () =
  let defaults =
    [
      "liblua-5.4.so";
      "liblua5.4.so.0";
      "liblua5.4.so";
      "liblua.5.4.dylib";
      "liblua5.4.dylib";
      "/opt/homebrew/opt/lua@5.4/lib/liblua.5.4.dylib";
      "/usr/local/opt/lua@5.4/lib/liblua.5.4.dylib";
    ]
  in
  match Sys.getenv_opt "ZENBU_LUA_LIBRARY" with
  | Some path when String.length path > 0 -> path :: defaults
  | None | Some _ -> defaults

let library =
  lazy
    (let rec open_library errors = function
       | [] ->
           raise
             (Dl.DL_error
                ("cannot load Lua 5.4; tried "
                ^ String.concat "; " (List.rev errors)))
       | filename :: rest -> (
           try Dl.dlopen ~filename ~flags:[ Dl.RTLD_NOW ]
           with Dl.DL_error message ->
             open_library ((filename ^ ": " ^ message) :: errors) rest)
     in
     open_library [] (lua_library_candidates ()))

let bind name signature = foreign ~from:(Lazy.force library) name signature
let new_state = bind "luaL_newstate" (void @-> returning (ptr void))
let close = bind "lua_close" (ptr void @-> returning void)
let open_libs = bind "luaL_openlibs" (ptr void @-> returning void)

let load_buffer =
  bind "luaL_loadbufferx"
    (ptr void @-> string @-> size_t @-> string @-> ptr void @-> returning int)

let protected_call =
  foreign ~from:(Lazy.force library) ~release_runtime_lock:true "lua_pcallk"
    (ptr void @-> int @-> int @-> int @-> int64_t @-> ptr void @-> returning int)

let get_top = bind "lua_gettop" (ptr void @-> returning int)
let set_top = bind "lua_settop" (ptr void @-> int @-> returning void)
let abs_index = bind "lua_absindex" (ptr void @-> int @-> returning int)
let value_type = bind "lua_type" (ptr void @-> int @-> returning int)
let is_integer = bind "lua_isinteger" (ptr void @-> int @-> returning int)

let to_integer =
  bind "lua_tointegerx" (ptr void @-> int @-> ptr int @-> returning int64_t)

let to_number =
  bind "lua_tonumberx" (ptr void @-> int @-> ptr int @-> returning double)

let to_boolean = bind "lua_toboolean" (ptr void @-> int @-> returning int)

let to_string =
  bind "lua_tolstring" (ptr void @-> int @-> ptr size_t @-> returning (ptr char))

let raw_length = bind "lua_rawlen" (ptr void @-> int @-> returning size_t)
let next = bind "lua_next" (ptr void @-> int @-> returning int)
let get_i = bind "lua_geti" (ptr void @-> int @-> int64_t @-> returning int)

let get_field =
  bind "lua_getfield" (ptr void @-> int @-> string @-> returning int)

let set_field =
  bind "lua_setfield" (ptr void @-> int @-> string @-> returning void)

let set_global = bind "lua_setglobal" (ptr void @-> string @-> returning void)
let get_global = bind "lua_getglobal" (ptr void @-> string @-> returning int)

let raw_get_i =
  bind "lua_rawgeti" (ptr void @-> int @-> int64_t @-> returning int)

let reference = bind "luaL_ref" (ptr void @-> int @-> returning int)

let create_table =
  bind "lua_createtable" (ptr void @-> int @-> int @-> returning void)

let set_i = bind "lua_seti" (ptr void @-> int @-> int64_t @-> returning void)
let set_table = bind "lua_settable" (ptr void @-> int @-> returning void)
let push_nil = bind "lua_pushnil" (ptr void @-> returning void)
let push_boolean = bind "lua_pushboolean" (ptr void @-> int @-> returning void)

let push_integer =
  bind "lua_pushinteger" (ptr void @-> int64_t @-> returning void)

let push_number = bind "lua_pushnumber" (ptr void @-> double @-> returning void)

let push_string =
  bind "lua_pushlstring"
    (ptr void @-> string @-> size_t @-> returning (ptr char))

type t = {
  state : state;
  source : string;
  mutable registrations : registration list;
  mutable callbacks : Callback.t list;
  mutable active_request : Host.request option;
  mutable raised_error : Error.t option;
  mutable disposed : bool;
}

let registry_index = -1001000
let lua_ok = 0
let lua_nil = 0
let lua_boolean = 1
let lua_number = 3
let lua_string = 4
let lua_table = 5
let lua_function = 6

let error ?line phase source message =
  Error.Script_error { phase; source = Some source; line; message }

let normalize_source ~default_source source =
  let prefix = "[string \"" in
  let suffix = "\"]" in
  let source_length = String.length source in
  let prefix_length = String.length prefix in
  let suffix_length = String.length suffix in
  if
    source_length >= prefix_length + suffix_length
    && String.sub source 0 prefix_length = prefix
    && String.sub source (source_length - suffix_length) suffix_length = suffix
  then
    String.sub source prefix_length
      (source_length - prefix_length - suffix_length)
  else if String.length source = 0 then default_source
  else source

let location_from_message ~default_source message =
  let length = String.length message in
  let rec scan index =
    if index >= length then (default_source, None)
    else if message.[index] <> ':' then scan (index + 1)
    else
      let rec digits stop =
        if
          stop < length
          && Char.code message.[stop] >= Char.code '0'
          && Char.code message.[stop] <= Char.code '9'
        then digits (stop + 1)
        else stop
      in
      let stop = digits (index + 1) in
      if stop = index + 1 || stop >= length || message.[stop] <> ':' then
        scan (index + 1)
      else
        let source = String.sub message 0 index in
        let line =
          try
            Some
              (int_of_string
                 (String.sub message (index + 1) (stop - index - 1)))
          with Failure _ -> None
        in
        (normalize_source ~default_source source, line)
  in
  scan 0

let lua_error backend phase message =
  let source, line =
    location_from_message ~default_source:backend.source message
  in
  let source = if phase = "parse" then backend.source else source in
  error ?line phase source message

let string_at state index =
  let length = allocate size_t Unsigned.Size_t.zero in
  let value = to_string state index length in
  if is_null value then None
  else Some (string_from_ptr value ~length:(Unsigned.Size_t.to_int !@length))

let pop state count = if count > 0 then set_top state (get_top state - count)

let pure_list_table state index length =
  let index = abs_index state index in
  push_nil state;
  let rec loop () =
    if next state index = 0 then true
    else
      let valid_key =
        if value_type state (-2) <> lua_number || is_integer state (-2) = 0 then
          false
        else
          let accepted = allocate int 0 in
          let key = to_integer state (-2) accepted |> Int64.to_int in
          !@accepted <> 0 && key >= 1 && key <= length
      in
      if valid_key then (
        pop state 1;
        loop ())
      else (
        pop state 2;
        false)
  in
  loop ()

let rec value_at state index =
  let index = abs_index state index in
  match value_type state index with
  | value_type when value_type = lua_nil -> Ok Value.Nil
  | value_type when value_type = lua_boolean ->
      Ok (Value.Bool (to_boolean state index <> 0))
  | value_type when value_type = lua_number ->
      if is_integer state index <> 0 then
        let accepted = allocate int 0 in
        let value = to_integer state index accepted in
        if !@accepted = 0 then
          Error (error "conversion" "<lua>" "invalid integer")
        else
          try Ok (Value.Integer (Int64.to_int value))
          with Failure _ ->
            Error
              (error "conversion" "<lua>" "integer is outside OCaml int range")
      else
        let accepted = allocate int 0 in
        let value = to_number state index accepted in
        if !@accepted = 0 then
          Error (error "conversion" "<lua>" "invalid number")
        else Ok (Value.Float value)
  | value_type when value_type = lua_string -> (
      match string_at state index with
      | Some value -> Ok (Value.Text value)
      | None -> Error (error "conversion" "<lua>" "invalid string"))
  | value_type when value_type = lua_table -> table_value state index
  | _ -> Error (error "conversion" "<lua>" "unsupported Lua value")

and table_value state index =
  let index = abs_index state index in
  let length = raw_length state index |> Unsigned.Size_t.to_int in
  if length > 0 && pure_list_table state index length then
    let rec items values position =
      if position > length then Ok (Value.List (List.rev values))
      else (
        ignore (get_i state index (Int64.of_int position));
        match value_at state (-1) with
        | Error _ as error ->
            pop state 1;
            error
        | Ok value ->
            pop state 1;
            items (value :: values) (position + 1))
    in
    items [] 1
  else
    let fields values =
      push_nil state;
      let rec loop values =
        if next state index = 0 then Value.record (List.rev values)
        else
          match (string_at state (-2), value_at state (-1)) with
          | Some key, Ok value ->
              pop state 1;
              loop ((key, value) :: values)
          | None, _ ->
              pop state 2;
              Error (error "conversion" "<lua>" "table keys must be strings")
          | _, (Error _ as error) ->
              pop state 2;
              error
      in
      loop values
    in
    fields []

let rec push_value state = function
  | Value.Nil -> push_nil state
  | Value.Bool value -> push_boolean state (if value then 1 else 0)
  | Value.Integer value -> push_integer state (Int64.of_int value)
  | Value.Float value -> push_number state value
  | Value.Text value ->
      ignore
        (push_string state value (Unsigned.Size_t.of_int (String.length value)))
  | Value.List values ->
      create_table state (List.length values) 0;
      List.iteri
        (fun index value ->
          push_value state value;
          set_i state (-2) (Int64.of_int (index + 1)))
        values
  | Value.Record fields ->
      create_table state 0 (List.length fields);
      List.iter
        (fun (key, value) ->
          ignore
            (push_string state key (Unsigned.Size_t.of_int (String.length key)));
          push_value state value;
          set_table state (-3))
        fields

let required_text state table name =
  ignore (get_field state table name);
  let value = string_at state (-1) in
  pop state 1;
  match value with
  | Some value when String.length value > 0 -> Ok value
  | _ -> Error (error "registration" "<lua>" ("missing string field " ^ name))

let optional_text state table name =
  ignore (get_field state table name);
  let value =
    match value_type state (-1) with
    | value_type when value_type = lua_nil -> Ok None
    | value_type when value_type = lua_string -> Ok (string_at state (-1))
    | _ ->
        Error
          (error "registration" "<lua>" ("field " ^ name ^ " must be a string"))
  in
  pop state 1;
  value

let mode_transition_error message = error "registration" "<lua>" message

let mode_id fields =
  match Value.find (Value.Record fields) "id" with
  | Some (Value.Text value) when String.length value > 0 -> Ok value
  | _ ->
      Error
        (mode_transition_error "mode transition requires nonempty string id")

let mode_action fields =
  match Value.find (Value.Record fields) "action" with
  | Some (Value.Text value) -> Ok value
  | _ -> Error (mode_transition_error "mode transition requires string action")

let mode_transition_of_value = function
  | Value.Nil -> Ok None
  | Value.Text "" -> Ok (Some Clear_modes)
  | Value.Text id -> Ok (Some (Replace_mode id))
  | Value.Record fields -> (
      match mode_action fields with
      | Error _ as error -> error
      | Ok "replace" ->
          Result.map (fun id -> Some (Replace_mode id)) (mode_id fields)
      | Ok "push" -> Result.map (fun id -> Some (Push_mode id)) (mode_id fields)
      | Ok "pop" -> Ok (Some Pop_mode)
      | Ok "clear" -> Ok (Some Clear_modes)
      | Ok _ ->
          Error
            (mode_transition_error
               "mode transition action must be replace, push, pop, or clear"))
  | _ ->
      Error
        (mode_transition_error
           "field mode must be a string or a mode transition table")

let optional_mode_transition state table =
  ignore (get_field state table "mode");
  let result = value_at state (-1) |> Result.bind mode_transition_of_value in
  pop state 1;
  result

let optional_boolean state table name =
  ignore (get_field state table name);
  let value =
    match value_type state (-1) with
    | value_type when value_type = lua_nil -> Ok false
    | value_type when value_type = lua_boolean -> Ok (to_boolean state (-1) <> 0)
    | _ ->
        Error
          (error "registration" "<lua>"
             ("field " ^ name ^ " must be a boolean"))
  in
  pop state 1;
  value

let parameter_error message = error "registration" "<lua>" message

let parameter_text fields name =
  match Value.find (Value.Record fields) name with
  | Some (Value.Text value) when String.length value > 0 -> Ok value
  | _ -> Error (parameter_error ("parameter needs string field " ^ name))

let parameter_required fields =
  match Value.find (Value.Record fields) "required" with
  | None -> Ok true
  | Some (Value.Bool value) -> Ok value
  | Some _ ->
      Error (parameter_error "parameter field required must be a boolean")

let parameter_kind fields =
  match Value.find (Value.Record fields) "kind" with
  | None -> Ok Descriptor.Text
  | Some (Value.Text value) ->
      Descriptor.parameter_kind_of_string value
      |> Result.map_error (fun error ->
          parameter_error (Zenbu_kernel.Error.to_string error))
  | Some _ -> Error (parameter_error "parameter field kind must be a string")

let parameter_of_value = function
  | Value.Record fields ->
      let ( let* ) = Result.bind in
      let* name = parameter_text fields "name" in
      let* parameter_description = parameter_text fields "description" in
      let* required = parameter_required fields in
      let* kind = parameter_kind fields in
      Ok
        Descriptor.{ name; description = parameter_description; required; kind }
  | _ -> Error (parameter_error "each parameter must be a table")

let parameters_of_value = function
  | Value.List values ->
      let rec collect result = function
        | [] -> Ok (List.rev result)
        | value :: rest ->
            Result.bind (parameter_of_value value) (fun parameter ->
                collect (parameter :: result) rest)
      in
      collect [] values
  | _ -> Error (parameter_error "field parameters must be a list of tables")

let optional_parameters state table =
  ignore (get_field state table "parameters");
  let result =
    match value_type state (-1) with
    | value_type when value_type = lua_nil -> Ok []
    | value_type when value_type = lua_table ->
        Result.bind (value_at state (-1)) parameters_of_value
    | _ -> Error (parameter_error "field parameters must be a list of tables")
  in
  pop state 1;
  result

let callback_field state table name =
  ignore (get_field state table name);
  if value_type state (-1) <> lua_function then (
    pop state 1;
    Error (error "registration" "<lua>" ("missing function field " ^ name)))
  else Ok (reference state registry_index)

let descriptor state table =
  match
    ( required_text state table "id",
      optional_text state table "title",
      optional_text state table "description",
      optional_boolean state table "requires_syntax",
      optional_parameters state table )
  with
  | Ok id, Ok title, Ok description, Ok requires_syntax, Ok parameters ->
      Ok
        {
          id;
          title = Option.value title ~default:id;
          description = Option.value description ~default:id;
          requires_syntax;
          parameters;
        }
  | Error error, _, _, _, _
  | _, Error error, _, _, _
  | _, _, Error error, _, _
  | _, _, _, Error error, _
  | _, _, _, _, Error error ->
      Error error

let callback_error backend state error =
  backend.raised_error <- Some error;
  push_nil state;
  1

let add_registration backend registration =
  backend.registrations <- backend.registrations @ [ registration ]

let register_descriptor backend constructor state =
  let result =
    if value_type state 1 <> lua_table then
      Error (error "registration" backend.source "registration expects a table")
    else
      match (descriptor state 1, callback_field state 1 "run") with
      | Ok descriptor, Ok callback ->
          add_registration backend (constructor (descriptor, callback));
          Ok ()
      | Error error, _ | _, Error error -> Error error
  in
  match result with
  | Ok () -> 0
  | Error error -> callback_error backend state error

let register_mode backend state =
  let result =
    if value_type state 1 <> lua_table then
      Error (error "registration" backend.source "mode expects a table")
    else
      Result.map
        (fun definition -> add_registration backend (Mode definition))
        (descriptor state 1)
  in
  match result with
  | Ok () -> 0
  | Error error -> callback_error backend state error

let register_binding backend state =
  let result =
    if value_type state 1 <> lua_table then
      Error (error "registration" backend.source "binding expects a table")
    else
      match
        ( required_text state 1 "input",
          required_text state 1 "command",
          optional_text state 1 "scope",
          optional_mode_transition state 1 )
      with
      | Ok input, Ok command, Ok scope, Ok mode_transition ->
          add_registration backend
            (Binding { input; command; scope; mode_transition });
          Ok ()
      | Error error, _, _, _
      | _, Error error, _, _
      | _, _, Error error, _
      | _, _, _, Error error ->
          Error error
  in
  match result with
  | Ok () -> 0
  | Error error -> callback_error backend state error

let register_hook backend state =
  let result =
    if value_type state 1 <> lua_table then
      Error (error "registration" backend.source "hook expects a table")
    else
      match (required_text state 1 "event", callback_field state 1 "run") with
      | Ok event, Ok callback ->
          add_registration backend (Hook { event; callback });
          Ok ()
      | Error error, _ | _, Error error -> Error error
  in
  match result with
  | Ok () -> 0
  | Error error -> callback_error backend state error

let field value name = Value.find value name
let context_field request name = field request.Host.context name
let require request capability = Host.require request ~capability

let document_contents request =
  match context_field request "document" with
  | Some (Value.Record fields) -> (
      match List.assoc_opt "contents" fields with
      | Some (Value.Text contents) -> Ok contents
      | _ ->
          Error (error "execution" "<extension>" "document data is unavailable")
      )
  | _ -> Error (error "execution" "<extension>" "document data is unavailable")

let text_callback backend state =
  let result =
    match backend.active_request with
    | None ->
        Error (error "execution" backend.source "zenbu.text is unavailable")
    | Some request ->
        Result.bind (require request "document.read") (fun () ->
            Result.bind (document_contents request) (fun contents ->
                match (value_at state 1, value_at state 2) with
                | Ok (Value.Integer start), Ok (Value.Integer stop)
                  when start >= 0 && stop >= start
                       && stop <= String.length contents ->
                    Ok (String.sub contents start (stop - start))
                | _ ->
                    Error
                      (error "execution" backend.source
                         "zenbu.text expects an in-bounds start and stop offset")))
  in
  match result with
  | Error error -> callback_error backend state error
  | Ok value ->
      ignore
        (push_string state value (Unsigned.Size_t.of_int (String.length value)));
      1

let node_contains ~start_offset ~stop_offset = function
  | Value.Record fields -> (
      match (List.assoc_opt "start" fields, List.assoc_opt "stop" fields) with
      | Some (Value.Integer start), Some (Value.Integer stop) ->
          start <= start_offset && stop_offset <= stop
      | _ -> false)
  | _ -> false

let rec smallest_node ~start_offset ~stop_offset node =
  if not (node_contains ~start_offset ~stop_offset node) then None
  else
    match node with
    | Value.Record fields -> (
        match List.assoc_opt "children" fields with
        | Some (Value.List children) ->
            List.find_map (smallest_node ~start_offset ~stop_offset) children
            |> Option.value ~default:node |> Option.some
        | _ -> Some node)
    | _ -> None

let syntax_callback backend state =
  let result =
    match backend.active_request with
    | None ->
        Error (error "execution" backend.source "zenbu.syntax is unavailable")
    | Some request ->
        Result.bind (require request "syntax.read") (fun () ->
            match context_field request "syntax" with
            | Some Value.Nil | None -> Ok Value.Nil
            | Some (Value.Record fields as syntax) ->
                let default_node = List.assoc_opt "node" fields in
                let requested_node () =
                  match
                    ( value_at state 1,
                      value_at state 2,
                      List.assoc_opt "tree" fields )
                  with
                  | ( Ok (Value.Integer start_offset),
                      Ok (Value.Integer stop_offset),
                      Some tree )
                    when start_offset >= 0 && stop_offset >= start_offset ->
                      Ok
                        (Option.value ~default:Value.Nil
                           (smallest_node ~start_offset ~stop_offset tree))
                  | _ ->
                      Error
                        (error "execution" backend.source
                           "zenbu.syntax expects zero arguments or an \
                            in-bounds start and stop offset")
                in
                let node =
                  if get_top state = 0 then
                    Ok (Option.value ~default:Value.Nil default_node)
                  else requested_node ()
                in
                Result.map
                  (fun node ->
                    match syntax with
                    | Value.Record fields ->
                        Value.Record
                          (List.filter (fun (name, _) -> name <> "tree") fields
                          @ [ ("node", node) ])
                    | _ -> assert false)
                  node
            | Some _ ->
                Error
                  (error "execution" backend.source "invalid syntax context"))
  in
  match result with
  | Error error -> callback_error backend state error
  | Ok value ->
      push_value state value;
      1

let add_callback backend state name callback =
  let callback = Callback.of_fun callback in
  backend.callbacks <- callback :: backend.callbacks;
  let push =
    bind "lua_pushcclosure" (ptr void @-> Callback.t @-> int @-> returning void)
  in
  push state callback 0;
  set_field state (-2) name

let configure_module_path backend =
  let state = backend.state in
  let directory = Filename.dirname backend.source in
  ignore (get_global state "package");
  if value_type state (-1) = lua_table then (
    ignore (get_field state (-1) "path");
    let existing = string_at state (-1) in
    pop state 1;
    Option.iter
      (fun existing ->
        let local_modules = Filename.concat directory "?.lua" in
        ignore
          (push_string state
             (local_modules ^ ";" ^ existing)
             (Unsigned.Size_t.of_int
                (String.length local_modules + 1 + String.length existing)));
        set_field state (-2) "path")
      existing);
  pop state 1

let create ~source =
  try
    let state = new_state () in
    if is_null state then Error (error "load" source "cannot create Lua state")
    else
      let backend =
        {
          state;
          source;
          registrations = [];
          callbacks = [];
          active_request = None;
          raised_error = None;
          disposed = false;
        }
      in
      open_libs state;
      configure_module_path backend;
      create_table state 0 10;
      push_integer state 1L;
      set_field state (-2) "api_version";
      add_callback backend state "command"
        (register_descriptor backend (fun (descriptor, callback) ->
             Command (descriptor, callback)));
      add_callback backend state "selector"
        (register_descriptor backend (fun (descriptor, callback) ->
             Selector (descriptor, callback)));
      add_callback backend state "transform"
        (register_descriptor backend (fun (descriptor, callback) ->
             Transformation (descriptor, callback)));
      add_callback backend state "mode" (register_mode backend);
      add_callback backend state "bind" (register_binding backend);
      add_callback backend state "on" (register_hook backend);
      add_callback backend state "text" (text_callback backend);
      add_callback backend state "syntax" (syntax_callback backend);
      set_global state "zenbu";
      Ok backend
  with Dl.DL_error message -> Error (error "load" source message)

let lua_error_message backend =
  Option.value ~default:"Lua execution failed" (string_at backend.state (-1))

let evaluate backend source =
  if backend.disposed then
    Error (error "load" backend.source "Lua state is disposed")
  else
    let state = backend.state in
    set_top state 0;
    backend.raised_error <- None;
    let loaded =
      load_buffer state source
        (Unsigned.Size_t.of_int (String.length source))
        backend.source (from_voidp void null)
    in
    if loaded <> lua_ok then (
      let message = lua_error_message backend in
      set_top state 0;
      Error (lua_error backend "parse" message))
    else
      let called = protected_call state 0 0 0 0L (from_voidp void null) in
      if called = lua_ok then (
        set_top state 0;
        match backend.raised_error with
        | Some error -> Error error
        | None -> Ok ())
      else
        let message = lua_error_message backend in
        set_top state 0;
        Error
          (Option.value
             ~default:(lua_error backend "evaluation" message)
             backend.raised_error)

let call backend callback ~request =
  if backend.disposed then
    Error (error "execution" backend.source "Lua state is disposed")
  else
    let state = backend.state in
    set_top state 0;
    backend.active_request <- Some request;
    backend.raised_error <- None;
    Fun.protect
      ~finally:(fun () ->
        backend.active_request <- None;
        set_top state 0)
      (fun () ->
        ignore (raw_get_i state registry_index (Int64.of_int callback));
        if value_type state (-1) <> lua_function then
          Error
            (error "execution" backend.source "callback is no longer available")
        else (
          push_value state
            (Value.Record
               [
                 ("context", request.context); ("arguments", request.arguments);
               ]);
          let called = protected_call state 1 1 0 0L (from_voidp void null) in
          if called <> lua_ok then
            Error
              (Option.value
                 ~default:
                   (lua_error backend "execution" (lua_error_message backend))
                 backend.raised_error)
          else
            match backend.raised_error with
            | Some error -> Error error
            | None -> value_at state (-1)))

let registrations backend = backend.registrations

let dispose backend =
  if not backend.disposed then (
    backend.disposed <- true;
    List.iter
      (fun callback ->
        try Callback.free callback with Invalid_argument _ -> ())
      backend.callbacks;
    backend.callbacks <- [];
    backend.registrations <- [];
    backend.active_request <- None;
    backend.raised_error <- None;
    close backend.state)
