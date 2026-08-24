type model = Vim | Selection | Direct | Structural

type buffer = {
  id : int;
  path : string;
  name : string option;
  language : string option;
  model : model;
}

type pane_buffer = { pane : int; buffer : int }

type viewport = {
  pane : int;
  top_line : int;
  left_column : int;
  follow_cursor : bool;
}

type pane_display_options = { pane : int; options : Zenbu_view.View_options.t }
type selection = { anchor : int; head : int }

type view_position = {
  pane : int;
  buffer : int;
  selections : selection list;
  primary : int;
}

type t = {
  schema_version : int;
  buffers : buffer list;
  layout : Zenbu_view.Layout.persisted;
  focused_pane : int;
  pane_buffers : pane_buffer list;
  viewports : viewport list;
  pane_display_options : pane_display_options list;
  view_positions : view_position list;
}

let current_schema_version = 3
let error path message = Error (path ^ ": " ^ message)
let ( let* ) = Result.bind

let model_name = function
  | Vim -> "vim"
  | Selection -> "selection"
  | Direct -> "direct"
  | Structural -> "structural"

let model_of_name = function
  | "vim" -> Ok Vim
  | "selection" -> Ok Selection
  | "direct" -> Ok Direct
  | "structural" -> Ok Structural
  | value -> error "model" ("unsupported model " ^ Printf.sprintf "%S" value)

let unique path values =
  if List.length values = List.length (List.sort_uniq compare values) then Ok ()
  else error path "contains duplicates"

let fields path allowed = function
  | `Assoc fields -> (
      let* () = unique path (List.map fst fields) in
      let unknown =
        List.find_opt (fun (name, _) -> not (List.mem name allowed)) fields
      in
      match unknown with
      | None -> Ok fields
      | Some (name, _) -> error path ("unknown field " ^ name))
  | _ -> error path "expected an object"

let required path fields name decode =
  match List.assoc_opt name fields with
  | None -> error path ("missing field " ^ name)
  | Some value -> decode (path ^ "." ^ name) value

let optional path fields name decode =
  match List.assoc_opt name fields with
  | None | Some `Null -> Ok None
  | Some value -> decode (path ^ "." ^ name) value |> Result.map Option.some

let string path = function
  | `String value -> Ok value
  | _ -> error path "expected a string"

let nonempty_string path value =
  Result.bind (string path value) (fun value ->
      if String.length value = 0 then error path "must not be empty"
      else Ok value)

let integer path = function
  | `Int value -> Ok value
  | `Intlit value -> (
      match int_of_string_opt value with
      | Some value -> Ok value
      | None -> error path "is outside the supported integer range")
  | _ -> error path "expected an integer"

let nonnegative path value =
  Result.bind (integer path value) (fun value ->
      if value < 0 then error path "must not be negative" else Ok value)

let boolean path = function
  | `Bool value -> Ok value
  | _ -> error path "expected a boolean"

let array decode path = function
  | `List values ->
      let rec loop index values =
        match values with
        | [] -> Ok []
        | value :: rest ->
            let* value = decode (Printf.sprintf "%s[%d]" path index) value in
            let* rest = loop (index + 1) rest in
            Ok (value :: rest)
      in
      loop 0 values
  | _ -> error path "expected an array"

let orientation_json = function
  | Zenbu_view.Layout.Horizontal -> `String "horizontal"
  | Zenbu_view.Layout.Vertical -> `String "vertical"

let orientation path = function
  | `String "horizontal" -> Ok Zenbu_view.Layout.Horizontal
  | `String "vertical" -> Ok Zenbu_view.Layout.Vertical
  | `String value -> error path ("unsupported orientation " ^ value)
  | _ -> error path "expected an orientation string"

let rec layout_json = function
  | Zenbu_view.Layout.Pane pane ->
      `Assoc [ ("kind", `String "pane"); ("pane", `Int pane) ]
  | Zenbu_view.Layout.Persisted_split { orientation; ratio; first; second } ->
      `Assoc
        [
          ("kind", `String "split");
          ("orientation", orientation_json orientation);
          ("ratio", `Int ratio);
          ("first", layout_json first);
          ("second", layout_json second);
        ]

let rec layout path value =
  let* initial_fields =
    fields path
      [ "kind"; "pane"; "orientation"; "ratio"; "first"; "second" ]
      value
  in
  let* kind = required path initial_fields "kind" string in
  match kind with
  | "pane" ->
      let* node_fields = fields path [ "kind"; "pane" ] value in
      required path node_fields "pane" nonnegative
      |> Result.map (fun pane -> Zenbu_view.Layout.Pane pane)
  | "split" ->
      let* node_fields =
        fields path [ "kind"; "orientation"; "ratio"; "first"; "second" ] value
      in
      let* orientation = required path node_fields "orientation" orientation in
      let* ratio = required path node_fields "ratio" integer in
      let* first = required path node_fields "first" layout in
      let* second = required path node_fields "second" layout in
      Ok
        (Zenbu_view.Layout.Persisted_split { orientation; ratio; first; second })
  | value -> error (path ^ ".kind") ("unsupported layout kind " ^ value)

let option_json encode = function None -> `Null | Some value -> encode value

let buffer_json (buffer : buffer) =
  `Assoc
    [
      ("id", `Int buffer.id);
      ("path", `String buffer.path);
      ("name", option_json (fun value -> `String value) buffer.name);
      ("language", option_json (fun value -> `String value) buffer.language);
      ("model", `String (model_name buffer.model));
    ]

let buffer path value =
  let* fields =
    fields path [ "id"; "path"; "name"; "language"; "model" ] value
  in
  let* id = required path fields "id" nonnegative in
  let* path_value = required path fields "path" nonempty_string in
  let* name = optional path fields "name" nonempty_string in
  let* language = optional path fields "language" nonempty_string in
  let* model =
    Result.bind (required path fields "model" string) model_of_name
  in
  Ok { id; path = path_value; name; language; model }

let pane_buffer_json (value : pane_buffer) =
  `Assoc [ ("pane", `Int value.pane); ("buffer", `Int value.buffer) ]

let pane_buffer path value =
  let* fields = fields path [ "pane"; "buffer" ] value in
  let* pane = required path fields "pane" nonnegative in
  let* buffer = required path fields "buffer" nonnegative in
  Ok { pane; buffer }

let viewport_json (value : viewport) =
  `Assoc
    [
      ("pane", `Int value.pane);
      ("top_line", `Int value.top_line);
      ("left_column", `Int value.left_column);
      ("follow_cursor", `Bool value.follow_cursor);
    ]

let viewport path value =
  let* fields =
    fields path [ "pane"; "top_line"; "left_column"; "follow_cursor" ] value
  in
  let* pane = required path fields "pane" nonnegative in
  let* top_line = required path fields "top_line" nonnegative in
  let* left_column = required path fields "left_column" nonnegative in
  let* follow_cursor = required path fields "follow_cursor" boolean in
  Ok { pane; top_line; left_column; follow_cursor }

let pane_display_options_json (value : pane_display_options) =
  `Assoc
    [
      ("pane", `Int value.pane);
      ( "scroll_margin",
        `Int (Zenbu_view.View_options.scroll_margin value.options) );
      ( "presentation",
        option_json
          (fun value -> `String value)
          (Zenbu_view.View_options.presentation value.options) );
    ]

let pane_display_options path value =
  let* fields = fields path [ "pane"; "scroll_margin"; "presentation" ] value in
  let* pane = required path fields "pane" nonnegative in
  let* scroll_margin = required path fields "scroll_margin" integer in
  let* presentation = optional path fields "presentation" nonempty_string in
  Zenbu_view.View_options.create ~scroll_margin ~presentation
  |> Result.map_error (fun reason -> path ^ ": " ^ reason)
  |> Result.map (fun options -> { pane; options })

let selection_json (value : selection) =
  `Assoc [ ("anchor", `Int value.anchor); ("head", `Int value.head) ]

let selection path value =
  let* fields = fields path [ "anchor"; "head" ] value in
  let* anchor = required path fields "anchor" nonnegative in
  let* head = required path fields "head" nonnegative in
  Ok { anchor; head }

let view_position_json (value : view_position) =
  `Assoc
    [
      ("pane", `Int value.pane);
      ("buffer", `Int value.buffer);
      ("selections", `List (List.map selection_json value.selections));
      ("primary", `Int value.primary);
    ]

let view_position path value =
  let* fields =
    fields path [ "pane"; "buffer"; "selections"; "primary" ] value
  in
  let* pane = required path fields "pane" nonnegative in
  let* buffer = required path fields "buffer" nonnegative in
  let* selections = required path fields "selections" (array selection) in
  let* primary = required path fields "primary" nonnegative in
  if selections = [] then error (path ^ ".selections") "must not be empty"
  else if primary >= List.length selections then
    error (path ^ ".primary") "does not identify a selection"
  else Ok { pane; buffer; selections; primary }

let ids values = List.sort Int.compare values

let duplicate_pairs values =
  List.length values <> List.length (List.sort_uniq compare values)

let validate (value : t) =
  let* () =
    if
      value.schema_version = 1 || value.schema_version = 2
      || value.schema_version = current_schema_version
    then Ok ()
    else
      error "schema_version"
        ("unsupported version " ^ string_of_int value.schema_version)
  in
  let* () =
    unique "buffers" (List.map (fun buffer -> buffer.id) value.buffers)
  in
  let* () =
    unique "buffers" (List.map (fun buffer -> buffer.path) value.buffers)
  in
  let* () =
    if List.exists (fun buffer -> buffer.id = max_int) value.buffers then
      error "buffers"
        "contains an identifier that cannot allocate a next buffer"
    else Ok ()
  in
  let* layout =
    Zenbu_view.Layout.of_persisted value.layout
    |> Result.map_error (fun message -> "layout: " ^ message)
  in
  let pane_ids = Zenbu_view.Layout.panes layout |> ids in
  let* () =
    if List.mem max_int pane_ids then
      error "layout"
        "contains a pane identifier that cannot allocate a next pane"
    else Ok ()
  in
  let buffer_ids = List.map (fun buffer -> buffer.id) value.buffers |> ids in
  let pane_buffer_ids =
    List.map (fun (value : pane_buffer) -> value.pane) value.pane_buffers |> ids
  in
  let* () =
    if pane_ids = pane_buffer_ids then Ok ()
    else error "pane_buffers" "must assign every layout pane exactly once"
  in
  let* () =
    if
      List.for_all
        (fun (value : pane_buffer) -> List.mem value.buffer buffer_ids)
        value.pane_buffers
    then Ok ()
    else error "pane_buffers" "references an unknown buffer"
  in
  let* () =
    if List.mem value.focused_pane pane_ids then Ok ()
    else error "focused_pane" "does not identify a layout pane"
  in
  let viewport_ids =
    List.map (fun (value : viewport) -> value.pane) value.viewports |> ids
  in
  let* () =
    if pane_ids = viewport_ids then Ok ()
    else error "viewports" "must describe every layout pane exactly once"
  in
  let pane_display_option_ids =
    List.map
      (fun (value : pane_display_options) -> value.pane)
      value.pane_display_options
    |> ids
  in
  let* () =
    if pane_ids = pane_display_option_ids then Ok ()
    else
      error "pane_display_options"
        "must describe every layout pane exactly once"
  in
  let position_pairs =
    List.map
      (fun (value : view_position) -> (value.pane, value.buffer))
      value.view_positions
  in
  let* () =
    if duplicate_pairs position_pairs then
      error "view_positions" "contains duplicates"
    else Ok ()
  in
  if
    List.for_all
      (fun (value : view_position) ->
        List.mem value.pane pane_ids && List.mem value.buffer buffer_ids)
      value.view_positions
  then Ok value
  else error "view_positions" "references an unknown pane or buffer"

let json value =
  `Assoc
    [
      ("schema_version", `Int current_schema_version);
      ("buffers", `List (List.map buffer_json value.buffers));
      ("layout", layout_json value.layout);
      ("focused_pane", `Int value.focused_pane);
      ("pane_buffers", `List (List.map pane_buffer_json value.pane_buffers));
      ("viewports", `List (List.map viewport_json value.viewports));
      ( "pane_display_options",
        `List (List.map pane_display_options_json value.pane_display_options) );
      ( "view_positions",
        `List (List.map view_position_json value.view_positions) );
    ]

let encode value = Yojson.Safe.pretty_to_string (json value) ^ "\n"

let decode contents =
  let parsed =
    try Ok (Yojson.Safe.from_string contents)
    with Yojson.Json_error message -> error "layout" message
  in
  let* value = parsed in
  let* initial_fields =
    fields "layout"
      [
        "schema_version";
        "buffers";
        "layout";
        "focused_pane";
        "pane_buffers";
        "viewports";
        "pane_display_options";
        "view_positions";
      ]
      value
  in
  let* schema_version =
    required "layout" initial_fields "schema_version" integer
  in
  let* () =
    if
      schema_version = 1 || schema_version = 2
      || schema_version = current_schema_version
    then Ok ()
    else
      error "schema_version"
        ("unsupported version " ^ string_of_int schema_version)
  in
  let allowed =
    match schema_version with
    | 1 ->
        [
          "schema_version";
          "buffers";
          "layout";
          "focused_pane";
          "pane_buffers";
          "view_positions";
        ]
    | 2 ->
        [
          "schema_version";
          "buffers";
          "layout";
          "focused_pane";
          "pane_buffers";
          "viewports";
          "view_positions";
        ]
    | value when value = current_schema_version ->
        [
          "schema_version";
          "buffers";
          "layout";
          "focused_pane";
          "pane_buffers";
          "viewports";
          "pane_display_options";
          "view_positions";
        ]
    | _ -> assert false
  in
  let* fields = fields "layout" allowed value in
  let* buffers = required "layout" fields "buffers" (array buffer) in
  let* layout = required "layout" fields "layout" layout in
  let* focused_pane = required "layout" fields "focused_pane" nonnegative in
  let* pane_buffers =
    required "layout" fields "pane_buffers" (array pane_buffer)
  in
  let* viewports =
    match schema_version with
    | 1 ->
        let* restored = Zenbu_view.Layout.of_persisted layout in
        Ok
          (Zenbu_view.Layout.panes restored
          |> List.map (fun pane ->
              { pane; top_line = 0; left_column = 0; follow_cursor = true }))
    | _ -> required "layout" fields "viewports" (array viewport)
  in
  let* pane_display_options =
    match schema_version with
    | 1 | 2 ->
        let* restored = Zenbu_view.Layout.of_persisted layout in
        Ok
          (Zenbu_view.Layout.panes restored
          |> List.map (fun pane ->
              { pane; options = Zenbu_view.View_options.default }))
    | _ ->
        required "layout" fields "pane_display_options"
          (array pane_display_options)
  in
  let* view_positions =
    required "layout" fields "view_positions" (array view_position)
  in
  validate
    {
      schema_version;
      buffers;
      layout;
      focused_pane;
      pane_buffers;
      viewports;
      pane_display_options;
      view_positions;
    }
