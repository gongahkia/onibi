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

val lex :
  source_name:string -> source:string -> (token list, Diagnostic.t list) result
