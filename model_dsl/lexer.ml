open Zenbu_kernel

type kind =
  | Ident of string
  | String of string
  | Integer of int
  | Lbrace
  | Rbrace
  | Arrow
  | Dollar
  | Eof

type token = { kind : kind; span : Source_span.t }

let diagnostic ~source_name ~source ~start_offset ~stop_offset message =
  Diagnostic.make ~severity:Diagnostic.Error ~message ~source_name ~source
    (Source_span.make ~start_offset ~stop_offset)

let is_space = function ' ' | '\t' | '\r' | '\n' -> true | _ -> false

let is_ident_start = function
  | 'a' .. 'z' | 'A' .. 'Z' | '_' -> true
  | _ -> false

let is_ident_continue = function
  | 'a' .. 'z' | 'A' .. 'Z' | '0' .. '9' | '_' | '-' | '.' -> true
  | _ -> false

let lex ~source_name ~source =
  match Text_buffer.of_utf8 source with
  | Error _ ->
      Error
        [
          diagnostic ~source_name ~source ~start_offset:0
            ~stop_offset:(String.length source) "source is not valid UTF-8";
        ]
  | Ok _ ->
      let length = String.length source in
      let token kind start_offset stop_offset =
        { kind; span = Source_span.make ~start_offset ~stop_offset }
      in
      let rec skip_comment index =
        if index >= length || source.[index] = '\n' || source.[index] = '\r'
        then index
        else skip_comment (index + 1)
      in
      let rec string_value start index buffer =
        if index >= length then
          Error
            (diagnostic ~source_name ~source ~start_offset ~stop_offset:length
               "unterminated string literal")
        else
          match source.[index] with
          | '"' -> Ok (Buffer.contents buffer, index + 1)
          | '\n' | '\r' ->
              Error
                (diagnostic ~source_name ~source ~start_offset
                   ~stop_offset:(index + 1)
                   "string literals may not contain an unescaped newline")
          | '\\' when index + 1 >= length ->
              Error
                (diagnostic ~source_name ~source ~start_offset
                   ~stop_offset:(index + 1) "unterminated escape sequence")
          | '\\' -> (
              let escaped =
                match source.[index + 1] with
                | 'n' -> Ok '\n'
                | 'r' -> Ok '\r'
                | 't' -> Ok '\t'
                | '\\' -> Ok '\\'
                | '"' -> Ok '"'
                | value ->
                    Error
                      (diagnostic ~source_name ~source ~start_offset:index
                         ~stop_offset:(index + 2)
                         ("unsupported escape sequence \\" ^ String.make 1 value))
              in
              match escaped with
              | Error _ as error -> error
              | Ok value ->
                  Buffer.add_char buffer value;
                  string_value start (index + 2) buffer)
          | value ->
              Buffer.add_char buffer value;
              string_value start (index + 1) buffer
      in
      let rec identifier_end index =
        if index < length && is_ident_continue source.[index] then
          identifier_end (index + 1)
        else index
      in
      let rec integer_end index =
        if index < length then
          match source.[index] with
          | '0' .. '9' -> integer_end (index + 1)
          | _ -> index
        else index
      in
      let rec loop index tokens =
        if index >= length then Ok (List.rev (token Eof index index :: tokens))
        else if is_space source.[index] then loop (index + 1) tokens
        else
          match source.[index] with
          | '#' -> loop (skip_comment (index + 1)) tokens
          | '{' -> loop (index + 1) (token Lbrace index (index + 1) :: tokens)
          | '}' -> loop (index + 1) (token Rbrace index (index + 1) :: tokens)
          | '$' -> loop (index + 1) (token Dollar index (index + 1) :: tokens)
          | '-' when index + 1 < length && source.[index + 1] = '>' ->
              loop (index + 2) (token Arrow index (index + 2) :: tokens)
          | '"' -> (
              match string_value index (index + 1) (Buffer.create 16) with
              | Error error -> Error [ error ]
              | Ok (value, stop_offset) ->
                  loop stop_offset
                    (token (String value) index stop_offset :: tokens))
          | '0' .. '9' -> (
              let stop_offset = integer_end index in
              let text = String.sub source index (stop_offset - index) in
              match int_of_string_opt text with
              | Some value ->
                  loop stop_offset
                    (token (Integer value) index stop_offset :: tokens)
              | None ->
                  Error
                    [
                      diagnostic ~source_name ~source ~start_offset:index
                        ~stop_offset
                        "integer literal is outside the supported range";
                    ])
          | value when is_ident_start value ->
              let stop_offset = identifier_end (index + 1) in
              let value = String.sub source index (stop_offset - index) in
              loop stop_offset (token (Ident value) index stop_offset :: tokens)
          | value ->
              Error
                [
                  diagnostic ~source_name ~source ~start_offset:index
                    ~stop_offset:(index + 1)
                    ("unexpected character " ^ String.make 1 value);
                ]
      in
      loop 0 []
