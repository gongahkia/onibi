type ansi =
  | Black
  | Red
  | Green
  | Yellow
  | Blue
  | Magenta
  | Cyan
  | White
  | Light_black
  | Light_red
  | Light_green
  | Light_yellow
  | Light_blue
  | Light_magenta
  | Light_cyan
  | Light_white

type color = Default | Ansi of ansi | Rgb of int * int * int
type decoration = Bold | Italic | Underline

type attribute = {
  foreground : color;
  background : color;
  decorations : decoration list;
}

type t = { name : string; attributes : (Frame.style * attribute) list }

let styles =
  [
    Frame.Plain;
    Frame.Primary_selection;
    Frame.Secondary_selection;
    Frame.Status;
    Frame.Message;
    Frame.Dim;
    Frame.Search_match;
    Frame.Diagnostic_error;
    Frame.Diagnostic_warning;
    Frame.Diagnostic_information;
    Frame.Diagnostic_hint;
    Frame.Syntax_keyword;
    Frame.Syntax_string;
    Frame.Syntax_number;
    Frame.Syntax_comment;
    Frame.Syntax_type;
    Frame.Syntax_constructor;
    Frame.Decoration_inline;
    Frame.Decoration_virtual;
    Frame.Overlay;
  ]

let style_name = function
  | Frame.Plain -> "plain"
  | Frame.Primary_selection -> "primary_selection"
  | Frame.Secondary_selection -> "secondary_selection"
  | Frame.Status -> "status"
  | Frame.Message -> "message"
  | Frame.Dim -> "dim"
  | Frame.Search_match -> "search_match"
  | Frame.Diagnostic_error -> "diagnostic_error"
  | Frame.Diagnostic_warning -> "diagnostic_warning"
  | Frame.Diagnostic_information -> "diagnostic_information"
  | Frame.Diagnostic_hint -> "diagnostic_hint"
  | Frame.Syntax_keyword -> "syntax_keyword"
  | Frame.Syntax_string -> "syntax_string"
  | Frame.Syntax_number -> "syntax_number"
  | Frame.Syntax_comment -> "syntax_comment"
  | Frame.Syntax_type -> "syntax_type"
  | Frame.Syntax_constructor -> "syntax_constructor"
  | Frame.Decoration_inline -> "decoration_inline"
  | Frame.Decoration_virtual -> "decoration_virtual"
  | Frame.Overlay -> "overlay"

let plain = { foreground = Default; background = Default; decorations = [] }

let default_attribute = function
  | Frame.Plain -> plain
  | Frame.Primary_selection ->
      { foreground = Ansi White; background = Ansi Blue; decorations = [] }
  | Frame.Secondary_selection -> { plain with decorations = [ Underline ] }
  | Frame.Status ->
      {
        foreground = Ansi White;
        background = Ansi Light_black;
        decorations = [];
      }
  | Frame.Message | Frame.Search_match ->
      { foreground = Ansi Black; background = Ansi Yellow; decorations = [] }
  | Frame.Dim -> { plain with foreground = Ansi Light_black }
  | Frame.Diagnostic_error ->
      { plain with foreground = Ansi Red; decorations = [ Underline ] }
  | Frame.Diagnostic_warning ->
      { plain with foreground = Ansi Yellow; decorations = [ Underline ] }
  | Frame.Diagnostic_information ->
      { plain with foreground = Ansi Cyan; decorations = [ Underline ] }
  | Frame.Diagnostic_hint ->
      { plain with foreground = Ansi Light_black; decorations = [ Underline ] }
  | Frame.Syntax_keyword ->
      { plain with foreground = Ansi Cyan; decorations = [ Bold ] }
  | Frame.Syntax_string -> { plain with foreground = Ansi Green }
  | Frame.Syntax_number -> { plain with foreground = Ansi Magenta }
  | Frame.Syntax_comment ->
      { plain with foreground = Ansi Light_black; decorations = [ Italic ] }
  | Frame.Syntax_type ->
      { plain with foreground = Ansi Blue; decorations = [ Bold ] }
  | Frame.Syntax_constructor -> { plain with foreground = Ansi Yellow }
  | Frame.Decoration_inline ->
      { plain with foreground = Ansi Light_black; decorations = [ Italic ] }
  | Frame.Decoration_virtual ->
      { plain with foreground = Ansi Cyan; decorations = [ Italic ] }
  | Frame.Overlay ->
      {
        foreground = Ansi White;
        background = Ansi Light_black;
        decorations = [];
      }

let make name attributes = { name; attributes }

let default =
  make "default"
    (List.map (fun style -> (style, default_attribute style)) styles)

let dark =
  make "dark"
    [
      ( Frame.Plain,
        {
          foreground = Rgb (220, 222, 226);
          background = Rgb (20, 22, 27);
          decorations = [];
        } );
      ( Frame.Primary_selection,
        {
          foreground = Rgb (20, 22, 27);
          background = Rgb (97, 175, 239);
          decorations = [];
        } );
      ( Frame.Secondary_selection,
        {
          foreground = Rgb (220, 222, 226);
          background = Rgb (48, 52, 64);
          decorations = [];
        } );
      ( Frame.Status,
        {
          foreground = Rgb (220, 222, 226);
          background = Rgb (40, 44, 52);
          decorations = [];
        } );
      ( Frame.Message,
        {
          foreground = Rgb (20, 22, 27);
          background = Rgb (229, 192, 123);
          decorations = [];
        } );
      ( Frame.Dim,
        {
          foreground = Rgb (92, 99, 112);
          background = Rgb (20, 22, 27);
          decorations = [];
        } );
      ( Frame.Search_match,
        {
          foreground = Rgb (20, 22, 27);
          background = Rgb (229, 192, 123);
          decorations = [];
        } );
      ( Frame.Diagnostic_error,
        {
          foreground = Rgb (224, 108, 117);
          background = Rgb (20, 22, 27);
          decorations = [ Underline ];
        } );
      ( Frame.Diagnostic_warning,
        {
          foreground = Rgb (229, 192, 123);
          background = Rgb (20, 22, 27);
          decorations = [ Underline ];
        } );
      ( Frame.Diagnostic_information,
        {
          foreground = Rgb (97, 175, 239);
          background = Rgb (20, 22, 27);
          decorations = [ Underline ];
        } );
      ( Frame.Diagnostic_hint,
        {
          foreground = Rgb (92, 99, 112);
          background = Rgb (20, 22, 27);
          decorations = [ Underline ];
        } );
      ( Frame.Syntax_keyword,
        {
          foreground = Rgb (198, 120, 221);
          background = Rgb (20, 22, 27);
          decorations = [ Bold ];
        } );
      ( Frame.Syntax_string,
        {
          foreground = Rgb (152, 195, 121);
          background = Rgb (20, 22, 27);
          decorations = [];
        } );
      ( Frame.Syntax_number,
        {
          foreground = Rgb (209, 154, 102);
          background = Rgb (20, 22, 27);
          decorations = [];
        } );
      ( Frame.Syntax_comment,
        {
          foreground = Rgb (92, 99, 112);
          background = Rgb (20, 22, 27);
          decorations = [ Italic ];
        } );
      ( Frame.Syntax_type,
        {
          foreground = Rgb (97, 175, 239);
          background = Rgb (20, 22, 27);
          decorations = [ Bold ];
        } );
      ( Frame.Syntax_constructor,
        {
          foreground = Rgb (229, 192, 123);
          background = Rgb (20, 22, 27);
          decorations = [];
        } );
      ( Frame.Decoration_inline,
        {
          foreground = Rgb (92, 99, 112);
          background = Rgb (20, 22, 27);
          decorations = [ Italic ];
        } );
      ( Frame.Decoration_virtual,
        {
          foreground = Rgb (86, 182, 194);
          background = Rgb (20, 22, 27);
          decorations = [ Italic ];
        } );
      ( Frame.Overlay,
        {
          foreground = Rgb (220, 222, 226);
          background = Rgb (40, 44, 52);
          decorations = [];
        } );
    ]

let light =
  make "light"
    [
      ( Frame.Plain,
        {
          foreground = Rgb (56, 58, 66);
          background = Rgb (250, 250, 250);
          decorations = [];
        } );
      ( Frame.Primary_selection,
        {
          foreground = Rgb (250, 250, 250);
          background = Rgb (64, 120, 192);
          decorations = [];
        } );
      ( Frame.Secondary_selection,
        {
          foreground = Rgb (56, 58, 66);
          background = Rgb (220, 228, 240);
          decorations = [];
        } );
      ( Frame.Status,
        {
          foreground = Rgb (250, 250, 250);
          background = Rgb (56, 58, 66);
          decorations = [];
        } );
      ( Frame.Message,
        {
          foreground = Rgb (56, 58, 66);
          background = Rgb (238, 198, 100);
          decorations = [];
        } );
      ( Frame.Dim,
        {
          foreground = Rgb (120, 125, 135);
          background = Rgb (250, 250, 250);
          decorations = [];
        } );
      ( Frame.Search_match,
        {
          foreground = Rgb (56, 58, 66);
          background = Rgb (238, 198, 100);
          decorations = [];
        } );
      ( Frame.Diagnostic_error,
        {
          foreground = Rgb (192, 57, 43);
          background = Rgb (250, 250, 250);
          decorations = [ Underline ];
        } );
      ( Frame.Diagnostic_warning,
        {
          foreground = Rgb (165, 105, 0);
          background = Rgb (250, 250, 250);
          decorations = [ Underline ];
        } );
      ( Frame.Diagnostic_information,
        {
          foreground = Rgb (41, 98, 168);
          background = Rgb (250, 250, 250);
          decorations = [ Underline ];
        } );
      ( Frame.Diagnostic_hint,
        {
          foreground = Rgb (120, 125, 135);
          background = Rgb (250, 250, 250);
          decorations = [ Underline ];
        } );
      ( Frame.Syntax_keyword,
        {
          foreground = Rgb (120, 44, 146);
          background = Rgb (250, 250, 250);
          decorations = [ Bold ];
        } );
      ( Frame.Syntax_string,
        {
          foreground = Rgb (35, 126, 65);
          background = Rgb (250, 250, 250);
          decorations = [];
        } );
      ( Frame.Syntax_number,
        {
          foreground = Rgb (162, 75, 20);
          background = Rgb (250, 250, 250);
          decorations = [];
        } );
      ( Frame.Syntax_comment,
        {
          foreground = Rgb (120, 125, 135);
          background = Rgb (250, 250, 250);
          decorations = [ Italic ];
        } );
      ( Frame.Syntax_type,
        {
          foreground = Rgb (41, 98, 168);
          background = Rgb (250, 250, 250);
          decorations = [ Bold ];
        } );
      ( Frame.Syntax_constructor,
        {
          foreground = Rgb (165, 105, 0);
          background = Rgb (250, 250, 250);
          decorations = [];
        } );
      ( Frame.Decoration_inline,
        {
          foreground = Rgb (120, 125, 135);
          background = Rgb (250, 250, 250);
          decorations = [ Italic ];
        } );
      ( Frame.Decoration_virtual,
        {
          foreground = Rgb (28, 126, 126);
          background = Rgb (250, 250, 250);
          decorations = [ Italic ];
        } );
      ( Frame.Overlay,
        {
          foreground = Rgb (250, 250, 250);
          background = Rgb (56, 58, 66);
          decorations = [];
        } );
    ]

let builtins () = [ default; dark; light ]
let name theme = theme.name

let find_builtin value =
  builtins () |> List.find_opt (fun theme -> String.equal (name theme) value)

let attribute theme style =
  List.assoc_opt style theme.attributes
  |> Option.value ~default:(default_attribute style)

let table = function
  | Otoml.TomlTable values | Otoml.TomlInlineTable values -> Some values
  | _ -> None

let field fields name = List.assoc_opt name fields
let error path message = Error ("theme " ^ path ^ ": " ^ message)
let ( let* ) = Result.bind

let hexadecimal_digit = function
  | '0' .. '9' as value -> Some (Char.code value - Char.code '0')
  | 'a' .. 'f' as value -> Some (10 + Char.code value - Char.code 'a')
  | 'A' .. 'F' as value -> Some (10 + Char.code value - Char.code 'A')
  | _ -> None

let byte_of_hex text offset =
  match
    (hexadecimal_digit text.[offset], hexadecimal_digit text.[offset + 1])
  with
  | Some high, Some low -> Ok ((high * 16) + low)
  | None, _ | _, None -> Error "expected hexadecimal RGB components"

let color_of_string value =
  if String.equal value "default" then Ok Default
  else if String.length value = 7 && value.[0] = '#' then
    let* red = byte_of_hex value 1 in
    let* green = byte_of_hex value 3 in
    let* blue = byte_of_hex value 5 in
    Ok (Rgb (red, green, blue))
  else Error "expected default or #RRGGBB"

let parse_color ~path fields key fallback =
  match field fields key with
  | None -> Ok fallback
  | Some (Otoml.TomlString value) ->
      color_of_string value
      |> Result.map_error (fun reason -> path ^ "." ^ key ^ ": " ^ reason)
  | Some _ -> error (path ^ "." ^ key) "expected a string"

let replace_decoration decoration enabled decorations =
  let decorations =
    List.filter (fun value -> value <> decoration) decorations
  in
  if enabled then decorations @ [ decoration ] else decorations

let parse_decoration ~path fields key decoration decorations =
  match field fields key with
  | None -> Ok decorations
  | Some (Otoml.TomlBoolean enabled) ->
      Ok (replace_decoration decoration enabled decorations)
  | Some _ -> error (path ^ "." ^ key) "expected a boolean"

let allowed_fields =
  [ "foreground"; "background"; "bold"; "italic"; "underline" ]

let validate_fields path fields =
  match
    List.find_opt (fun (key, _) -> not (List.mem key allowed_fields)) fields
  with
  | None -> Ok ()
  | Some (key, _) -> error (path ^ "." ^ key) "unknown style field"

let parse_attribute style fields =
  let path = style_name style in
  let base = attribute default style in
  let* () = validate_fields path fields in
  let* foreground = parse_color ~path fields "foreground" base.foreground in
  let* background = parse_color ~path fields "background" base.background in
  let* decorations =
    parse_decoration ~path fields "bold" Bold base.decorations
  in
  let* decorations =
    parse_decoration ~path fields "italic" Italic decorations
  in
  let* decorations =
    parse_decoration ~path fields "underline" Underline decorations
  in
  Ok { foreground; background; decorations }

let parse_style root style =
  let style_name = style_name style in
  match field root style_name with
  | None -> Ok (style, attribute default style)
  | Some value -> (
      match table value with
      | Some fields ->
          parse_attribute style fields
          |> Result.map (fun value -> (style, value))
      | None -> error style_name "expected a table")

let parse_name root path =
  match field root "name" with
  | None -> Ok (Filename.basename path)
  | Some (Otoml.TomlString value) when String.length value > 0 -> Ok value
  | Some (Otoml.TomlString _) -> error "name" "must be a nonempty string"
  | Some _ -> error "name" "expected a string"

let validate_root root =
  let valid key =
    String.equal key "name"
    || List.exists (fun style -> String.equal key (style_name style)) styles
  in
  match List.find_opt (fun (key, _) -> not (valid key)) root with
  | None -> Ok ()
  | Some (key, _) -> error key "unknown theme field"

let rec parse_styles root values = function
  | [] -> Ok (List.rev values)
  | style :: rest ->
      let* value = parse_style root style in
      parse_styles root (value :: values) rest

let load path =
  let* document =
    Otoml.Parser.from_file_result path
    |> Result.map_error (fun message -> "theme " ^ path ^ ": " ^ message)
  in
  match table document with
  | None -> error path "root must be a table"
  | Some root ->
      let* () = validate_root root in
      let* name = parse_name root path in
      let* attributes = parse_styles root [] styles in
      Ok (make name attributes)
