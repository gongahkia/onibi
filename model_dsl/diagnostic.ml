type severity = Error | Warning

type t = {
  severity : severity;
  message : string;
  source_name : string;
  span : Source_span.t;
  line : int;
  column : int;
}

let line_column source offset =
  let limit = min (max 0 offset) (String.length source) in
  let rec loop index line column =
    if index >= limit then (line, column)
    else
      match source.[index] with
      | '\n' -> loop (index + 1) (line + 1) 1
      | '\r' when index + 1 < limit && source.[index + 1] = '\n' ->
          loop (index + 2) (line + 1) 1
      | '\r' -> loop (index + 1) (line + 1) 1
      | value ->
          let continuation = Char.code value land 0xc0 = 0x80 in
          loop (index + 1) line (if continuation then column else column + 1)
  in
  loop 0 1 1

let make ~severity ~message ~source_name ~source span =
  let line, column = line_column source (Source_span.start_offset span) in
  { severity; message; source_name; span; line; column }

let severity value = value.severity
let message value = value.message
let source_name value = value.source_name
let span value = value.span
let line value = value.line
let column value = value.column

let severity_name = function Error -> "error" | Warning -> "warning"

let format value =
  Printf.sprintf "%s:%d:%d: %s: %s" value.source_name value.line
    value.column (severity_name value.severity) value.message
