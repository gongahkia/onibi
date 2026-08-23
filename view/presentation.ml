type line_numbers = Hidden | Absolute | Relative
type status_line = Detailed | Minimal | Hidden_status
type buffer_line = Visible | Hidden_buffer_line

type t = {
  name : string;
  line_numbers : line_numbers;
  status_line : status_line;
  buffer_line : buffer_line;
}

let make name line_numbers status_line buffer_line =
  { name; line_numbers; status_line; buffer_line }

let default = make "default" Hidden Detailed Hidden_buffer_line
let numbered = make "numbered" Absolute Detailed Hidden_buffer_line
let relative = make "relative" Relative Detailed Hidden_buffer_line
let minimal = make "minimal" Hidden Minimal Hidden_buffer_line
let bare = make "bare" Hidden Hidden_status Hidden_buffer_line
let buffered = make "buffered" Hidden Detailed Visible
let builtins () = [ default; numbered; relative; minimal; bare; buffered ]
let name value = value.name
let line_numbers value = value.line_numbers
let status_line value = value.status_line
let buffer_line value = value.buffer_line

let find_builtin value =
  builtins ()
  |> List.find_opt (fun profile -> String.equal (name profile) value)

let table = function
  | Otoml.TomlTable values | Otoml.TomlInlineTable values -> Some values
  | _ -> None

let field fields name = List.assoc_opt name fields
let error path message = Error ("presentation " ^ path ^ ": " ^ message)
let ( let* ) = Result.bind

let parse_name root path =
  match field root "name" with
  | None -> Ok (Filename.basename path)
  | Some (Otoml.TomlString value) when String.length value > 0 -> Ok value
  | Some (Otoml.TomlString _) -> error "name" "must be a nonempty string"
  | Some _ -> error "name" "expected a string"

let parse_line_numbers root =
  match field root "line_numbers" with
  | None -> Ok (line_numbers default)
  | Some (Otoml.TomlString "none") -> Ok Hidden
  | Some (Otoml.TomlString "absolute") -> Ok Absolute
  | Some (Otoml.TomlString "relative") -> Ok Relative
  | Some (Otoml.TomlString _) ->
      error "line_numbers" "expected none, absolute, or relative"
  | Some _ -> error "line_numbers" "expected a string"

let parse_status_line root =
  match field root "status_line" with
  | None -> Ok (status_line default)
  | Some (Otoml.TomlString "detailed") -> Ok Detailed
  | Some (Otoml.TomlString "minimal") -> Ok Minimal
  | Some (Otoml.TomlString "hidden") -> Ok Hidden_status
  | Some (Otoml.TomlString _) ->
      error "status_line" "expected detailed, minimal, or hidden"
  | Some _ -> error "status_line" "expected a string"

let parse_buffer_line root =
  match field root "buffer_line" with
  | None -> Ok (buffer_line default)
  | Some (Otoml.TomlString "visible") -> Ok Visible
  | Some (Otoml.TomlString "hidden") -> Ok Hidden_buffer_line
  | Some (Otoml.TomlString _) ->
      error "buffer_line" "expected visible or hidden"
  | Some _ -> error "buffer_line" "expected a string"

let validate_root root =
  match
    List.find_opt
      (fun (key, _) ->
        not
          (String.equal key "name"
          || String.equal key "line_numbers"
          || String.equal key "status_line"
          || String.equal key "buffer_line"))
      root
  with
  | None -> Ok ()
  | Some (key, _) -> error key "unknown presentation field"

let load path =
  let* document =
    Otoml.Parser.from_file_result path
    |> Result.map_error (fun message -> "presentation " ^ path ^ ": " ^ message)
  in
  match table document with
  | None -> error path "root must be a table"
  | Some root ->
      let* () = validate_root root in
      let* name = parse_name root path in
      let* line_numbers = parse_line_numbers root in
      let* status_line = parse_status_line root in
      let* buffer_line = parse_buffer_line root in
      Ok (make name line_numbers status_line buffer_line)
