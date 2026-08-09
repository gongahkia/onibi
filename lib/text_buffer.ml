type t = string

let byte text index = Char.code text.[index]
let is_continuation byte = byte land 0xC0 = 0x80

let valid_utf8 text =
  let length = String.length text in
  let has index = index < length in
  let continuation index = has index && is_continuation (byte text index) in
  let rec loop index =
    if index = length then true
    else
      match byte text index with
      | value when value <= 0x7F -> loop (index + 1)
      | value when value >= 0xC2 && value <= 0xDF ->
          continuation (index + 1) && loop (index + 2)
      | 0xE0 ->
          has (index + 2)
          && byte text (index + 1) >= 0xA0
          && byte text (index + 1) <= 0xBF
          && continuation (index + 2)
          && loop (index + 3)
      | 0xED ->
          has (index + 2)
          && byte text (index + 1) >= 0x80
          && byte text (index + 1) <= 0x9F
          && continuation (index + 2)
          && loop (index + 3)
      | value
        when (value >= 0xE1 && value <= 0xEC) || (value >= 0xEE && value <= 0xEF)
        ->
          continuation (index + 1)
          && continuation (index + 2)
          && loop (index + 3)
      | 0xF0 ->
          has (index + 3)
          && byte text (index + 1) >= 0x90
          && byte text (index + 1) <= 0xBF
          && continuation (index + 2)
          && continuation (index + 3)
          && loop (index + 4)
      | value when value >= 0xF1 && value <= 0xF3 ->
          continuation (index + 1)
          && continuation (index + 2)
          && continuation (index + 3)
          && loop (index + 4)
      | 0xF4 ->
          has (index + 3)
          && byte text (index + 1) >= 0x80
          && byte text (index + 1) <= 0x8F
          && continuation (index + 2)
          && continuation (index + 3)
          && loop (index + 4)
      | _ -> false
  in
  loop 0

let of_utf8 text =
  if valid_utf8 text then Ok text else Error (Error.Invalid_utf8 "text")

let empty = ""
let contents text = text
let byte_length = String.length

let is_code_point_boundary text offset =
  offset >= 0
  && offset <= String.length text
  && (offset = String.length text || not (is_continuation (byte text offset)))

let replace text ~start ~stop ~with_ =
  let length = String.length text in
  if start < 0 || stop < start || stop > length then
    Error
      (Error.Invalid_anchor
         {
           offset = start;
           byte_length = length;
           reason = "replacement range is out of bounds";
         })
  else if
    not (is_code_point_boundary text start && is_code_point_boundary text stop)
  then
    Error
      (Error.Invalid_anchor
         {
           offset = start;
           byte_length = length;
           reason = "replacement range splits a UTF-8 code point";
         })
  else if not (valid_utf8 with_) then
    Error (Error.Invalid_utf8 "replacement text")
  else
    Ok
      (String.sub text 0 start ^ with_
      ^ String.sub text stop (String.length text - stop))
