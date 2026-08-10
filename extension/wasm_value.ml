module Value = Zenbu_model_api.Extension_value

let max_nodes = 4_096
let max_path_segments = 64
let max_total_string_bytes = 1_048_576

let response_limit detail =
  Error ("component response exceeds Zenbu limit: " ^ detail)

type segment = Field of string | Item of int
type kind = Nil | Boolean | Integer | Floating | Text | List | Record

type node = {
  path : segment list;
  kind : kind;
  boolean : bool;
  integer : int;
  floating : float;
  text : string;
}

let segment_value = function
  | Field name ->
      Value.Record
        [
          ("kind", Value.Text "field");
          ("name", Value.Text name);
          ("index", Value.Integer 0);
        ]
  | Item index ->
      Value.Record
        [
          ("kind", Value.Text "item");
          ("name", Value.Text "");
          ("index", Value.Integer index);
        ]

let kind_name = function
  | Nil -> "nil"
  | Boolean -> "boolean"
  | Integer -> "integer"
  | Floating -> "floating"
  | Text -> "text"
  | List -> "items"
  | Record -> "fields"

let node_value node =
  Value.Record
    [
      ("path", Value.List (List.map segment_value node.path));
      ("kind", Value.Text (kind_name node.kind));
      ("boolean", Value.Bool node.boolean);
      ("integer", Value.Integer node.integer);
      ("floating", Value.Float node.floating);
      ("text", Value.Text node.text);
    ]

let encode value =
  let rec descend path = function
    | Value.Nil ->
        [
          {
            path;
            kind = Nil;
            boolean = false;
            integer = 0;
            floating = 0.;
            text = "";
          };
        ]
    | Value.Bool boolean ->
        [
          {
            path;
            kind = Boolean;
            boolean;
            integer = 0;
            floating = 0.;
            text = "";
          };
        ]
    | Value.Integer integer ->
        [
          {
            path;
            kind = Integer;
            boolean = false;
            integer;
            floating = 0.;
            text = "";
          };
        ]
    | Value.Float floating ->
        [
          {
            path;
            kind = Floating;
            boolean = false;
            integer = 0;
            floating;
            text = "";
          };
        ]
    | Value.Text text ->
        [
          {
            path;
            kind = Text;
            boolean = false;
            integer = 0;
            floating = 0.;
            text;
          };
        ]
    | Value.List values ->
        {
          path;
          kind = List;
          boolean = false;
          integer = 0;
          floating = 0.;
          text = "";
        }
        :: (List.mapi
              (fun index value -> descend (path @ [ Item index ]) value)
              values
           |> List.concat)
    | Value.Record fields ->
        {
          path;
          kind = Record;
          boolean = false;
          integer = 0;
          floating = 0.;
          text = "";
        }
        :: List.concat_map
             (fun (name, value) -> descend (path @ [ Field name ]) value)
             fields
  in
  Value.List (List.map node_value (descend [] value))

let field name fields =
  match List.assoc_opt name fields with
  | Some value -> Ok value
  | None -> Error ("WIT value node is missing " ^ name)

let text_field name fields =
  Result.bind (field name fields) (function
    | Value.Text value -> Ok value
    | _ -> Error ("WIT value node field " ^ name ^ " must be text"))

let bool_field name fields =
  Result.bind (field name fields) (function
    | Value.Bool value -> Ok value
    | _ -> Error ("WIT value node field " ^ name ^ " must be bool"))

let integer_field name fields =
  Result.bind (field name fields) (function
    | Value.Integer value -> Ok value
    | _ -> Error ("WIT value node field " ^ name ^ " must be integer"))

let float_field name fields =
  Result.bind (field name fields) (function
    | Value.Float value -> Ok value
    | _ -> Error ("WIT value node field " ^ name ^ " must be float"))

let kind_of_string = function
  | "nil" -> Ok Nil
  | "boolean" -> Ok Boolean
  | "integer" -> Ok Integer
  | "floating" -> Ok Floating
  | "text" -> Ok Text
  | "items" -> Ok List
  | "fields" -> Ok Record
  | value -> Error ("unknown WIT value kind " ^ value)

let segment_of_value = function
  | Value.Record fields ->
      Result.bind (text_field "kind" fields) (function
        | "field" ->
            text_field "name" fields |> Result.map (fun name -> Field name)
        | "item" ->
            Result.bind (integer_field "index" fields) (fun index ->
                if index < 0 then
                  Error "WIT item path index must be nonnegative"
                else Ok (Item index))
        | value -> Error ("unknown WIT path segment kind " ^ value))
  | _ -> Error "WIT path segment must be a record"

let node_of_value = function
  | Value.Record fields ->
      let ( let* ) = Result.bind in
      let* path_value = field "path" fields in
      let* path =
        match path_value with
        | Value.List values ->
            let rec collect result = function
              | [] -> Ok (List.rev result)
              | value :: rest ->
                  let* value = segment_of_value value in
                  collect (value :: result) rest
            in
            collect [] values
        | _ -> Error "WIT value node path must be a list"
      in
      let* kind = Result.bind (text_field "kind" fields) kind_of_string in
      let* boolean = bool_field "boolean" fields in
      let* integer = integer_field "integer" fields in
      let* floating = float_field "floating" fields in
      let* text = text_field "text" fields in
      Ok { path; kind; boolean; integer; floating; text }
  | _ -> Error "WIT value node must be a record"

let key_of_path path =
  List.fold_left
    (fun key -> function
      | Field name ->
          key ^ "f:" ^ string_of_int (String.length name) ^ ":" ^ name ^ "/"
      | Item index -> key ^ "i:" ^ string_of_int index ^ "/")
    "" path

let direct_children nodes path =
  nodes
  |> List.filter_map (fun node ->
      match List.rev node.path with
      | last :: reversed_parent when List.rev reversed_parent = path ->
          Some (last, node)
      | _ -> None)

let decode value =
  let ( let* ) = Result.bind in
  let* nodes =
    match value with
    | Value.List values ->
        let rec collect result = function
          | [] -> Ok (List.rev result)
          | value :: rest ->
              let* value = node_of_value value in
              collect (value :: result) rest
        in
        collect [] values
    | _ -> Error "WIT value must be a list of nodes"
  in
  if List.length nodes > max_nodes then
    response_limit
      (Printf.sprintf "at most %d value nodes are accepted" max_nodes)
  else
    let total_string_bytes, overlong_path =
      List.fold_left
        (fun (total, overlong_path) node ->
          let path_bytes =
            List.fold_left
              (fun bytes -> function
                | Field name -> bytes + String.length name
                | Item _ -> bytes)
              0 node.path
          in
          ( total + String.length node.text + path_bytes,
            overlong_path || List.length node.path > max_path_segments ))
        (0, false) nodes
    in
    if overlong_path then
      response_limit
        (Printf.sprintf "path depth must not exceed %d segments"
           max_path_segments)
    else if total_string_bytes > max_total_string_bytes then
      response_limit
        (Printf.sprintf "string data must not exceed %d bytes"
           max_total_string_bytes)
    else
      let table = Hashtbl.create (List.length nodes) in
      let duplicate =
        List.find_opt
          (fun node ->
            let key = key_of_path node.path in
            if Hashtbl.mem table key then true
            else (
              Hashtbl.add table key node;
              false))
          nodes
      in
      match duplicate with
      | Some _ -> Error "WIT value has duplicate paths"
      | None -> (
          match Hashtbl.find_opt table "" with
          | None -> Error "WIT value has no root node"
          | Some _ ->
              let visited = Hashtbl.create (List.length nodes) in
              let rec build path =
                match Hashtbl.find_opt table (key_of_path path) with
                | None -> Error "WIT value path is not rooted"
                | Some node -> (
                    Hashtbl.replace visited (key_of_path path) ();
                    match node.kind with
                    | Nil -> Ok Value.Nil
                    | Boolean -> Ok (Value.Bool node.boolean)
                    | Integer -> Ok (Value.Integer node.integer)
                    | Floating -> Ok (Value.Float node.floating)
                    | Text -> Ok (Value.Text node.text)
                    | List ->
                        let children = direct_children nodes path in
                        let rec ordered index values =
                          match List.assoc_opt (Item index) children with
                          | Some _ ->
                              Result.bind
                                (build (path @ [ Item index ]))
                                (fun value ->
                                  ordered (index + 1) (value :: values))
                          | None ->
                              if
                                List.exists
                                  (function
                                    | Item candidate, _ -> candidate > index
                                    | Field _, _ -> true)
                                  children
                              then Error "WIT list paths must be contiguous items"
                              else Ok (Value.List (List.rev values))
                        in
                        ordered 0 []
                    | Record ->
                        let children = direct_children nodes path in
                        let rec fields result = function
                          | [] -> Ok (Value.Record (List.rev result))
                          | (Field name, _) :: rest ->
                              Result.bind
                                (build (path @ [ Field name ]))
                                (fun value ->
                                  fields ((name, value) :: result) rest)
                          | (Item _, _) :: _ ->
                              Error "WIT record paths must use field segments"
                        in
                        fields [] children)
              in
              Result.bind (build []) (fun decoded ->
                  if Hashtbl.length visited = List.length nodes then Ok decoded
                  else Error "WIT value contains unreachable paths"))
