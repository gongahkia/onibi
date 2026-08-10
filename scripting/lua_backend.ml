open Ctypes
open Foreign
module Error = Zenbu_kernel.Error
module Value = Zenbu_model_api.Extension_value
module Context = Zenbu_model_api.Editor_context

type callback = int

type descriptor = {
  id : string;
  title : string;
  description : string;
  requires_syntax : bool;
}

type binding = { input : string; command : string; scope : string option }
type hook = { event : string; callback : callback }

type registration =
  | Command of descriptor * callback
  | Selector of descriptor * callback
  | Transformation of descriptor * callback
  | Binding of binding
  | Hook of hook

type state = unit ptr

module Callback =
  (val dynamic_funptr ~runtime_lock:true (ptr void @-> returning int))

let library = lazy (Dl.dlopen ~filename:"liblua-5.4.so" ~flags:[ Dl.RTLD_NOW ])
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

let raise_error = bind "lua_error" (ptr void @-> returning int)

type t = {
  state : state;
  source : string;
  mutable registrations : registration list;
  mutable callbacks : Callback.t list;
  mutable active_context : Context.t option;
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
        ((if String.length source = 0 then default_source else source), line)
  in
  scan 0

let lua_error backend phase message =
  let source, line =
    location_from_message ~default_source:backend.source message
  in
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
      optional_boolean state table "requires_syntax" )
  with
  | Ok id, Ok title, Ok description, Ok requires_syntax ->
      Ok
        {
          id;
          title = Option.value title ~default:id;
          description = Option.value description ~default:id;
          requires_syntax;
        }
  | Error error, _, _, _
  | _, Error error, _, _
  | _, _, Error error, _
  | _, _, _, Error error ->
      Error error

let registration_error _backend state error =
  ignore
    (push_string state (Error.to_string error)
       (Unsigned.Size_t.of_int (String.length (Error.to_string error))));
  raise_error state

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
  | Error error -> registration_error backend state error

let register_binding backend state =
  let result =
    if value_type state 1 <> lua_table then
      Error (error "registration" backend.source "binding expects a table")
    else
      match
        ( required_text state 1 "input",
          required_text state 1 "command",
          optional_text state 1 "scope" )
      with
      | Ok input, Ok command, Ok scope ->
          add_registration backend (Binding { input; command; scope });
          Ok ()
      | Error error, _, _ | _, Error error, _ | _, _, Error error -> Error error
  in
  match result with
  | Ok () -> 0
  | Error error -> registration_error backend state error

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
  | Error error -> registration_error backend state error

let context_value context =
  let selection_text selection =
    let contents = Context.contents context in
    let start = min selection.Context.anchor_offset selection.head_offset in
    let stop = max selection.anchor_offset selection.head_offset in
    String.sub contents start (stop - start)
  in
  let selections = Context.selections context in
  let syntax =
    match Context.syntax context with
    | None -> Value.Nil
    | Some syntax ->
        Value.Record
          [
            ( "language",
              Value.Text
                (Zenbu_syntax.Syntax.Snapshot.language syntax
                |> Zenbu_syntax.Syntax.Language.id) );
            ( "version",
              Value.Integer
                (Zenbu_syntax.Syntax.Snapshot.document_version syntax) );
            ( "has_error",
              Value.Bool (Zenbu_syntax.Syntax.Snapshot.has_error syntax) );
          ]
  in
  Value.Record
    [
      ( "document",
        Value.Record
          [
            ("id", Value.Text (Context.document_id context));
            ("version", Value.Integer (Context.document_version context));
            ("length", Value.Integer (Context.byte_length context));
          ] );
      ( "selections",
        Value.List
          (List.map
             (fun selection ->
               Value.Record
                 [
                   ("anchor", Value.Integer selection.Context.anchor_offset);
                   ("head", Value.Integer selection.head_offset);
                   ( "start",
                     Value.Integer
                       (min selection.anchor_offset selection.head_offset) );
                   ( "stop",
                     Value.Integer
                       (max selection.anchor_offset selection.head_offset) );
                   ("text", Value.Text (selection_text selection));
                 ])
             selections.Context.selections) );
      ("primary", Value.Integer selections.primary_index);
      ("syntax", syntax);
    ]

let text_callback backend state =
  let result =
    match backend.active_context with
    | None ->
        Error (error "execution" backend.source "zenbu.text is unavailable")
    | Some context -> (
        match (value_at state 1, value_at state 2) with
        | Ok (Value.Integer start), Ok (Value.Integer stop)
          when start >= 0 && stop >= start
               && stop <= Context.byte_length context ->
            let contents = Context.contents context in
            Ok (String.sub contents start (stop - start))
        | _ ->
            Error
              (error "execution" backend.source
                 "zenbu.text expects an in-bounds start and stop offset"))
  in
  match result with
  | Error error -> registration_error backend state error
  | Ok value ->
      ignore
        (push_string state value (Unsigned.Size_t.of_int (String.length value)));
      1

let node_value node =
  let open Zenbu_syntax.Syntax.Snapshot.Node in
  let compact node =
    Value.Record
      [
        ("kind", Value.Text (kind node |> Zenbu_syntax.Syntax.Kind.to_string));
        ("start", Value.Integer (start_offset node));
        ("stop", Value.Integer (stop_offset node));
        ("named", Value.Bool (is_named node));
        ("error", Value.Bool (has_error node));
        ("missing", Value.Bool (is_missing node));
      ]
  in
  Value.Record
    [
      ("kind", Value.Text (kind node |> Zenbu_syntax.Syntax.Kind.to_string));
      ("start", Value.Integer (start_offset node));
      ("stop", Value.Integer (stop_offset node));
      ("named", Value.Bool (is_named node));
      ("error", Value.Bool (has_error node));
      ("missing", Value.Bool (is_missing node));
      ( "parent",
        Option.value ~default:Value.Nil (Option.map compact (parent_named node))
      );
      ( "first_child",
        Option.value ~default:Value.Nil
          (Option.map compact (first_named_child node)) );
      ("children", Value.List (List.map compact (named_children node)));
      ( "next_sibling",
        Option.value ~default:Value.Nil
          (Option.map compact (next_named_sibling node)) );
      ( "previous_sibling",
        Option.value ~default:Value.Nil
          (Option.map compact (previous_named_sibling node)) );
    ]

let syntax_value context ~start_offset ~stop_offset =
  match Context.syntax context with
  | None -> Value.Nil
  | Some snapshot ->
      let node =
        Zenbu_syntax.Syntax.Snapshot.smallest_named_containing snapshot
          ~start_offset ~stop_offset
        |> Option.map node_value
        |> Option.value ~default:Value.Nil
      in
      Value.Record
        [
          ( "language",
            Value.Text
              (Zenbu_syntax.Syntax.Snapshot.language snapshot
              |> Zenbu_syntax.Syntax.Language.id) );
          ( "version",
            Value.Integer
              (Zenbu_syntax.Syntax.Snapshot.document_version snapshot) );
          ( "has_error",
            Value.Bool (Zenbu_syntax.Syntax.Snapshot.has_error snapshot) );
          ("node", node);
        ]

let syntax_callback backend state =
  let result =
    match backend.active_context with
    | None ->
        Error (error "execution" backend.source "zenbu.syntax is unavailable")
    | Some context ->
        let selections = Context.selections context in
        let primary = List.nth selections.selections selections.primary_index in
        let default () =
          Ok
            (syntax_value context
               ~start_offset:(min primary.anchor_offset primary.head_offset)
               ~stop_offset:(max primary.anchor_offset primary.head_offset))
        in
        let requested () =
          match (value_at state 1, value_at state 2) with
          | Ok (Value.Integer start_offset), Ok (Value.Integer stop_offset)
            when start_offset >= 0
                 && stop_offset >= start_offset
                 && stop_offset <= Context.byte_length context ->
              Ok (syntax_value context ~start_offset ~stop_offset)
          | _ ->
              Error
                (error "execution" backend.source
                   "zenbu.syntax expects zero arguments or an in-bounds start \
                    and stop offset")
        in
        if get_top state = 0 then default () else requested ()
  in
  match result with
  | Error error -> registration_error backend state error
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
          active_context = None;
          disposed = false;
        }
      in
      open_libs state;
      configure_module_path backend;
      create_table state 0 9;
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
        Ok ())
      else
        let message = lua_error_message backend in
        set_top state 0;
        Error (lua_error backend "evaluation" message)

let call backend callback ~context ~arguments =
  if backend.disposed then
    Error (error "execution" backend.source "Lua state is disposed")
  else
    let state = backend.state in
    set_top state 0;
    backend.active_context <- Some context;
    Fun.protect
      ~finally:(fun () ->
        backend.active_context <- None;
        set_top state 0)
      (fun () ->
        ignore (raw_get_i state registry_index (Int64.of_int callback));
        if value_type state (-1) <> lua_function then
          Error
            (error "execution" backend.source "callback is no longer available")
        else (
          push_value state
            (Value.Record
               [ ("context", context_value context); ("arguments", arguments) ]);
          let called = protected_call state 1 1 0 0L (from_voidp void null) in
          if called <> lua_ok then
            Error (lua_error backend "execution" (lua_error_message backend))
          else value_at state (-1)))

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
    backend.active_context <- None;
    close backend.state)
