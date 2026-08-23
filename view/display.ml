type grapheme = {
  start_offset : int;
  stop_offset : int;
  text : string;
  column : int;
  width : int;
}

type line = {
  number : int;
  start_offset : int;
  stop_offset : int;
  end_offset : int;
  graphemes : grapheme list;
  width : int;
}

type source_line = {
  number : int;
  start_offset : int;
  stop_offset : int;
  end_offset : int;
}

let tab_width = 4

let utf8_segments value =
  let segmenter = Uuseg.create `Grapheme_cluster in
  let flush buffer segments =
    let segment = Buffer.contents buffer in
    Buffer.clear buffer;
    if String.length segment = 0 then segments else segment :: segments
  in
  let rec drain buffer segments input =
    match Uuseg.add segmenter input with
    | `Uchar uchar ->
        Buffer.add_utf_8_uchar buffer uchar;
        drain buffer segments `Await
    | `Boundary -> drain buffer (flush buffer segments) `Await
    | `Await | `End -> segments
  in
  let rec add_chars buffer segments index =
    if index = String.length value then
      List.rev (flush buffer (drain buffer segments `End))
    else
      let decoded = String.get_utf_8_uchar value index in
      let segments =
        drain buffer segments (`Uchar (Uchar.utf_decode_uchar decoded))
      in
      add_chars buffer segments (index + Uchar.utf_decode_length decoded)
  in
  add_chars (Buffer.create 16) [] 0

let is_control uchar =
  let value = Uchar.to_int uchar in
  value < 0x20 || value = 0x7f

let control_representation uchar =
  let value = Uchar.to_int uchar in
  if value = 0x7f then "^?"
  else String.make 1 '^' ^ String.make 1 (Char.chr (value + Char.code '@'))

let grapheme_width text =
  let rec loop index width =
    if index = String.length text then max 1 width
    else
      let decoded = String.get_utf_8_uchar text index in
      let uchar = Uchar.utf_decode_uchar decoded in
      let hint = Uucp.Break.tty_width_hint uchar in
      loop (index + Uchar.utf_decode_length decoded) (max width (max 0 hint))
  in
  loop 0 0

let layout_source_line ~number ~start_offset source =
  let rec loop offset column graphemes = function
    | [] ->
        {
          number;
          start_offset;
          stop_offset = start_offset + String.length source;
          end_offset = start_offset + String.length source;
          graphemes = List.rev graphemes;
          width = column;
        }
    | segment :: rest ->
        let segment_length = String.length segment in
        let decoded = String.get_utf_8_uchar segment 0 in
        let uchar = Uchar.utf_decode_uchar decoded in
        let text, width =
          if Uchar.equal uchar (Uchar.of_int 0x09) && segment_length = 1 then
            (" ", tab_width - (column mod tab_width))
          else if is_control uchar && segment_length = 1 then
            (control_representation uchar, 2)
          else (segment, grapheme_width segment)
        in
        let grapheme =
          {
            start_offset = start_offset + offset;
            stop_offset = start_offset + offset + segment_length;
            text;
            column;
            width;
          }
        in
        loop (offset + segment_length) (column + width) (grapheme :: graphemes)
          rest
  in
  loop 0 0 [] (utf8_segments source)

let source_lines contents =
  let length = String.length contents in
  let rec loop number start_offset lines =
    match String.index_from_opt contents start_offset '\n' with
    | Some newline ->
        let line =
          {
            number;
            start_offset;
            stop_offset = newline;
            end_offset = newline + 1;
          }
        in
        loop (number + 1) (newline + 1) (line :: lines)
    | None ->
        List.rev
          ({ number; start_offset; stop_offset = length; end_offset = length }
          :: lines)
  in
  loop 0 0 []

let layout contents source_line =
  let source =
    String.sub contents source_line.start_offset
      (source_line.stop_offset - source_line.start_offset)
  in
  let line =
    layout_source_line ~number:source_line.number
      ~start_offset:source_line.start_offset source
  in
  { line with end_offset = source_line.end_offset }

let lines contents = List.map (layout contents) (source_lines contents)

let source_line_at source_lines offset =
  let rec loop = function
    | [] -> invalid_arg "Display.source_line_at requires at least one line"
    | [ line ] -> line
    | line :: rest -> if offset <= line.stop_offset then line else loop rest
  in
  loop source_lines

let column_at (line : line) offset =
  let offset = min (max offset line.start_offset) line.stop_offset in
  let rec loop = function
    | [] -> line.width
    | (grapheme : grapheme) :: rest ->
        if offset <= grapheme.start_offset then grapheme.column
        else if offset < grapheme.stop_offset then grapheme.column
        else loop rest
  in
  loop line.graphemes

let offset_at_column (line : line) column =
  let column = max 0 column in
  let rec loop = function
    | [] -> line.stop_offset
    | (grapheme : grapheme) :: rest ->
        if column <= grapheme.column then grapheme.start_offset
        else if column < grapheme.column + grapheme.width then
          grapheme.start_offset
        else loop rest
  in
  loop line.graphemes

let locate lines offset =
  let rec loop = function
    | [] -> invalid_arg "Display.locate requires at least one line"
    | [ line ] -> (line, column_at line offset)
    | line :: rest ->
        if offset <= line.stop_offset then (line, column_at line offset)
        else loop rest
  in
  loop lines

let visible_graphemes (line : line) ~left_column ~width =
  if width <= 0 then []
  else
    let right_column = left_column + width in
    List.filter
      (fun grapheme ->
        grapheme.column + grapheme.width > left_column
        && grapheme.column < right_column)
      line.graphemes

let text_width value =
  (layout_source_line ~number:0 ~start_offset:0 value).width
