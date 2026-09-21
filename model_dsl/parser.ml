type state = {
  source_name : string;
  source : string;
  tokens : Lexer.token array;
  mutable index : int;
}

exception Parse_error of Diagnostic.t

let diagnostic parser span message =
  Diagnostic.make ~severity:Diagnostic.Error ~message
    ~source_name:parser.source_name ~source:parser.source span

let current parser = parser.tokens.(min parser.index (Array.length parser.tokens - 1))

let advance parser =
  let token = current parser in
  parser.index <- parser.index + 1;
  token

let unexpected parser expected =
  let token = current parser in
  raise
    (Parse_error
       (diagnostic parser token.span
          ("expected " ^ expected ^ " before this token")))

let unexpected_at parser token expected =
  raise
    (Parse_error
       (diagnostic parser token.span
          ("expected " ^ expected ^ " before this token")))

let expect_ident parser expected =
  let token = advance parser in
  match token.kind with
  | Lexer.Ident value when String.equal value expected -> ()
  | _ -> unexpected_at parser token ("`" ^ expected ^ "`")

let expect_identifier parser description =
  let token = advance parser in
  match token.kind with
  | Lexer.Ident value -> (value, token.span)
  | _ -> unexpected_at parser token description

let expect_string parser description =
  let token = advance parser in
  match token.kind with
  | Lexer.String value -> (value, token.span)
  | _ -> unexpected_at parser token description

let expect_kind parser kind expected =
  let token = advance parser in
  if token.kind <> kind then unexpected_at parser token expected;
  token.span

let is_ident parser value =
  match (current parser).kind with
  | Lexer.Ident current -> String.equal current value
  | _ -> false

let join first last = Source_span.join first last

let parse_status parser start_span =
  expect_kind parser Lexer.Lbrace "`{` after `status`" |> ignore;
  let label = ref None in
  let input_mode = ref None in
  let rec loop () =
    match (current parser).kind with
    | Lexer.Rbrace ->
        let end_span = (advance parser).span in
        let label =
          match !label with
          | Some value -> value
          | None ->
              raise (Parse_error (diagnostic parser end_span "status requires a `label`"))
        in
        let input_mode =
          match !input_mode with
          | Some value -> value
          | None ->
              raise (Parse_error (diagnostic parser end_span "status requires an `input`"))
        in
        { Ast.label = label; input_mode; span = join start_span end_span }
    | Lexer.Ident "label" ->
        let keyword = advance parser in
        let value, _span = expect_string parser "a status label string" in
        if Option.is_some !label then
          raise (Parse_error (diagnostic parser keyword.span "duplicate status label"));
        label := Some value;
        loop ()
    | Lexer.Ident "input" ->
        let keyword = advance parser in
        let token = advance parser in
        let value =
          match token.kind with
          | Lexer.Ident "keys" -> Ast.Keys
          | Lexer.Ident "text" -> Ast.Text
          | _ ->
              raise
                (Parse_error
                   (diagnostic parser token.span
                      "status input must be `keys` or `text`"))
        in
        if Option.is_some !input_mode then
          raise (Parse_error (diagnostic parser keyword.span "duplicate status input"));
        input_mode := Some value;
        loop ()
    | Lexer.Eof -> unexpected parser "`}` closing status"
    | _ -> unexpected parser "`label` or `input` in status"
  in
  loop ()

let parse_effect parser =
  match (current parser).kind with
  | Lexer.Ident "apply" ->
      let start = (advance parser).span in
      expect_ident parser "selector";
      let selector, selector_span = expect_string parser "a selector ID" in
      expect_ident parser "transform";
      let transformation, transformation_span =
        expect_string parser "a transformation ID"
      in
      Ast.Apply
        {
          selector;
          selector_span;
          transformation;
          transformation_span;
          span = join start transformation_span;
        }
  | Lexer.Ident "insert" ->
      let start = (advance parser).span in
      expect_kind parser Lexer.Dollar "`$` before a capture name" |> ignore;
      let name, name_span = expect_identifier parser "a capture name" in
      Ast.Insert_capture { name; name_span; span = join start name_span }
  | Lexer.Ident name ->
      let token = current parser in
      raise
        (Parse_error
           (diagnostic parser token.span
              ("unsupported transition effect `" ^ name ^ "` in DSL v1")))
  | _ -> unexpected parser "a supported transition effect"

let parse_transition parser =
  let start = (advance parser).span in
  let pattern, pattern_span = expect_string parser "an input pattern string" in
  let capture =
    if is_ident parser "as" then (
      advance parser |> ignore;
      Some (expect_identifier parser "a capture name"))
    else None
  in
  expect_kind parser Lexer.Arrow "`->` after an input pattern" |> ignore;
  let target, target_span = expect_identifier parser "a target state" in
  let effects, end_span =
    match (current parser).kind with
    | Lexer.Lbrace ->
        advance parser |> ignore;
        let rec effects values =
          match (current parser).kind with
          | Lexer.Rbrace ->
              let end_span = (advance parser).span in
              (List.rev values, end_span)
          | Lexer.Eof -> unexpected parser "`}` closing transition effects"
          | _ -> effects (parse_effect parser :: values)
        in
        effects []
    | _ -> ([], target_span)
  in
  {
    Ast.pattern = pattern;
    pattern_span;
    capture;
    target;
    target_span;
    effects;
    span = join start end_span;
  }

let parse_state parser =
  let start = (advance parser).span in
  let name, name_span = expect_identifier parser "a state name" in
  expect_kind parser Lexer.Lbrace "`{` after a state name" |> ignore;
  let rec declarations statuses transitions =
    match (current parser).kind with
    | Lexer.Rbrace ->
        let end_span = (advance parser).span in
        {
          Ast.name = name;
          name_span;
          statuses = List.rev statuses;
          transitions = List.rev transitions;
          span = join start end_span;
        }
    | Lexer.Ident "status" ->
        let status_start = (advance parser).span in
        declarations (parse_status parser status_start :: statuses) transitions
    | Lexer.Ident "on" ->
        declarations statuses (parse_transition parser :: transitions)
    | Lexer.Eof -> unexpected parser "`}` closing state"
    | _ -> unexpected parser "`status` or `on` in state"
  in
  declarations [] []

let parse ~source_name ~source tokens =
  let required_prefix = "zenbu-model " in
  if
    String.length source < String.length required_prefix
    || not (String.starts_with ~prefix:required_prefix source)
  then
    Error
      [
        Diagnostic.make ~severity:Diagnostic.Error
          ~message:"a .zenmodel file must start with `zenbu-model 1`"
          ~source_name ~source
          (Source_span.make ~start_offset:0
             ~stop_offset:(min 1 (String.length source)));
      ]
  else
    let parser = { source_name; source; tokens = Array.of_list tokens; index = 0 } in
    try
      expect_ident parser "zenbu-model";
    let version_token = advance parser in
    let version =
      match version_token.kind with
      | Lexer.Integer version -> version
      | _ -> unexpected parser "a language version number"
    in
    expect_ident parser "model";
    let id, id_span = expect_string parser "a model ID string" in
    expect_kind parser Lexer.Lbrace "`{` after model ID" |> ignore;
    let rec declarations titles initials states =
      match (current parser).kind with
      | Lexer.Rbrace ->
          let end_span = (advance parser).span in
          (match (current parser).kind with
          | Lexer.Eof ->
              Ok
                {
                  Ast.version = version;
                  version_span = version_token.span;
                  model =
                    {
                      id;
                      id_span;
                      titles = List.rev titles;
                      initials = List.rev initials;
                      states = List.rev states;
                      span = join id_span end_span;
                    };
                }
          | _ -> unexpected parser "end of file after the model declaration")
      | Lexer.Ident "title" ->
          advance parser |> ignore;
          let value = expect_string parser "a title string" in
          declarations (value :: titles) initials states
      | Lexer.Ident "initial" ->
          advance parser |> ignore;
          let value = expect_identifier parser "an initial state name" in
          declarations titles (value :: initials) states
      | Lexer.Ident "state" ->
          declarations titles initials (parse_state parser :: states)
      | Lexer.Eof -> unexpected parser "`}` closing model"
      | _ -> unexpected parser "`title`, `initial`, or `state` in model"
    in
      declarations [] [] []
    with Parse_error diagnostic -> Error [ diagnostic ]
