type t =
  | Current_selections
  | Document
  | Next_text_unit
  | Previous_text_unit
  | Next_word
  | Previous_word
  | Word_end
  | Current_word
  | Around_word
  | Current_line
  | Line_start
  | Line_end
  | First_nonblank
  | Document_start
  | Document_end
  | Next_line
  | Previous_line
  | All_occurrences

let to_string = function
  | Current_selections -> "current-selections"
  | Document -> "document"
  | Next_text_unit -> "next-text-unit"
  | Previous_text_unit -> "previous-text-unit"
  | Next_word -> "next-word"
  | Previous_word -> "previous-word"
  | Word_end -> "word-end"
  | Current_word -> "current-word"
  | Around_word -> "around-word"
  | Current_line -> "current-line"
  | Line_start -> "line-start"
  | Line_end -> "line-end"
  | First_nonblank -> "first-nonblank"
  | Document_start -> "document-start"
  | Document_end -> "document-end"
  | Next_line -> "next-line"
  | Previous_line -> "previous-line"
  | All_occurrences -> "all-occurrences"

let of_string = function
  | "current-selections" -> Ok Current_selections
  | "document" -> Ok Document
  | "next-text-unit" -> Ok Next_text_unit
  | "previous-text-unit" -> Ok Previous_text_unit
  | "next-word" -> Ok Next_word
  | "previous-word" -> Ok Previous_word
  | "word-end" -> Ok Word_end
  | "current-word" -> Ok Current_word
  | "around-word" -> Ok Around_word
  | "current-line" -> Ok Current_line
  | "line-start" -> Ok Line_start
  | "line-end" -> Ok Line_end
  | "first-nonblank" -> Ok First_nonblank
  | "document-start" -> Ok Document_start
  | "document-end" -> Ok Document_end
  | "next-line" -> Ok Next_line
  | "previous-line" -> Ok Previous_line
  | "all-occurrences" -> Ok All_occurrences
  | value -> Error (Error.Malformed_replay ("unknown selector " ^ value))

let descriptors () =
  let provider =
    Provider.create ~id:"zenbu.kernel" ~kind:Provider.Builtin
    |> Result.get_ok
  in
  let declare value title description =
    Semantic_descriptor.create ~id:(to_string value) ~title ~description
      ~provider ~kind:Semantic_descriptor.Selector ()
    |> Result.get_ok
  in
  [
    declare Current_selections "Current selections"
      "Resolves the active selection set.";
    declare Document "Document" "Resolves the complete document.";
    declare Next_text_unit "Next text unit"
      "Resolves the next UTF-8 scalar from each selection head.";
    declare Previous_text_unit "Previous text unit"
      "Resolves the previous UTF-8 scalar from each selection head.";
    declare Next_word "Next word" "Resolves through the next word target.";
    declare Previous_word "Previous word"
      "Resolves through the previous word target.";
    declare Word_end "Word end" "Resolves to the end of the current word.";
    declare Current_word "Current word" "Resolves the current word.";
    declare Around_word "Around word" "Resolves the current word and boundary.";
    declare Current_line "Current line" "Resolves the current source line.";
    declare Line_start "Line start" "Resolves the start of the current line.";
    declare Line_end "Line end" "Resolves the end of the current line.";
    declare First_nonblank "First nonblank"
      "Resolves the first nonblank position on the current line.";
    declare Document_start "Document start" "Resolves the document start.";
    declare Document_end "Document end" "Resolves the document end.";
    declare Next_line "Next line" "Resolves the next source line target.";
    declare Previous_line "Previous line"
      "Resolves the previous source line target.";
    declare All_occurrences "All occurrences"
      "Resolves non-overlapping literal occurrences of the primary selection.";
  ]

let is_continuation text index = Char.code text.[index] land 0xC0 = 0x80

let next_boundary text offset =
  let length = String.length text in
  if offset >= length then
    Error
      (Error.Invalid_selector "next text unit is unavailable at document end")
  else
    let rec find index =
      if index = length || not (is_continuation text index) then index
      else find (index + 1)
    in
    Ok (find (offset + 1))

let previous_boundary text offset =
  if offset <= 0 then
    Error
      (Error.Invalid_selector
         "previous text unit is unavailable at document start")
  else
    let rec find index =
      if not (is_continuation text index) then index else find (index - 1)
    in
    Ok (find (offset - 1))

let selection_from_offsets snapshot ~anchor_offset ~head_offset =
  match Document_snapshot.anchor snapshot ~byte_offset:anchor_offset with
  | Error _ as error -> error
  | Ok anchor -> (
      match Document_snapshot.anchor snapshot ~byte_offset:head_offset with
      | Error _ as error -> error
      | Ok head -> Selection.make ~anchor ~head)

let collect selections =
  let rec loop values = function
    | [] -> Ok (List.rev values)
    | Ok value :: rest -> loop (value :: values) rest
    | Error error :: _ -> Error error
  in
  loop [] selections

let selection_set snapshot selections =
  match collect selections with
  | Error _ as error -> error
  | Ok selections ->
      Selection_set.create
        ~primary:
          (Selection_set.primary_index (Document_snapshot.selections snapshot))
        selections

let selection_heads snapshot f =
  Document_snapshot.selections snapshot
  |> Selection_set.to_list
  |> List.map (fun selection ->
      let head = Anchor.byte_offset (Selection.head selection) in
      match f head with
      | Error _ as error -> error
      | Ok (anchor_offset, head_offset) ->
          selection_from_offsets snapshot ~anchor_offset ~head_offset)
  |> selection_set snapshot

let resolve_text_unit snapshot direction =
  let text = Document_snapshot.contents snapshot in
  selection_heads snapshot (fun head ->
      match direction with
      | `Next -> (
          match next_boundary text head with
          | Error _ as error -> error
          | Ok stop -> Ok (head, stop))
      | `Previous -> (
          match previous_boundary text head with
          | Error _ as error -> error
          | Ok start -> Ok (head, start)))

type word_class = Whitespace | Word | Punctuation

let word_class_at text offset =
  let code = Char.code text.[offset] in
  if
    code = Char.code ' '
    || code = Char.code '\t'
    || code = Char.code '\n'
    || code = Char.code '\r'
  then Whitespace
  else if
    code >= 0x80
    || (code >= Char.code 'a' && code <= Char.code 'z')
    || (code >= Char.code 'A' && code <= Char.code 'Z')
    || (code >= Char.code '0' && code <= Char.code '9')
    || code = Char.code '_'
  then Word
  else Punctuation

let skip_forward_while text offset predicate =
  let rec loop index =
    if index >= String.length text || not (predicate (word_class_at text index))
    then index
    else
      match next_boundary text index with
      | Ok next -> loop next
      | Error _ -> String.length text
  in
  loop offset

let skip_backward_while text offset predicate =
  let rec loop index =
    if index <= 0 then 0
    else
      match previous_boundary text index with
      | Error _ -> 0
      | Ok previous ->
          if predicate (word_class_at text previous) then loop previous
          else index
  in
  loop offset

let next_word_stop text offset =
  if offset >= String.length text then
    Error (Error.Invalid_selector "next word is unavailable at document end")
  else
    let initial_class = word_class_at text offset in
    let after_unit =
      skip_forward_while text offset (fun word_class ->
          word_class = initial_class)
    in
    let after_space =
      skip_forward_while text after_unit (fun word_class ->
          word_class = Whitespace)
    in
    Ok after_space

let previous_word_start text offset =
  if offset <= 0 then
    Error
      (Error.Invalid_selector "previous word is unavailable at document start")
  else
    let after_space =
      skip_backward_while text offset (fun word_class ->
          word_class = Whitespace)
    in
    if after_space = 0 then Ok 0
    else
      match previous_boundary text after_space with
      | Error _ as error -> error
      | Ok previous ->
          let word_class = word_class_at text previous in
          Ok
            (skip_backward_while text after_space (fun candidate ->
                 candidate = word_class))

let word_bounds text offset =
  if offset >= String.length text then
    Error (Error.Invalid_selector "word is unavailable at document end")
  else
    let word_class = word_class_at text offset in
    if word_class = Whitespace then
      Error (Error.Invalid_selector "word is unavailable on whitespace")
    else
      let start =
        skip_backward_while text offset (fun candidate ->
            candidate = word_class)
      in
      let stop =
        skip_forward_while text offset (fun candidate -> candidate = word_class)
      in
      Ok (start, stop)

let word_end text offset =
  let start =
    skip_forward_while text offset (fun word_class -> word_class = Whitespace)
  in
  if start >= String.length text then
    Error (Error.Invalid_selector "word end is unavailable at document end")
  else
    let word_class = word_class_at text start in
    Ok (skip_forward_while text start (fun candidate -> candidate = word_class))

let resolve_word snapshot selector =
  let text = Document_snapshot.contents snapshot in
  selection_heads snapshot (fun head ->
      match selector with
      | Next_word -> (
          match next_word_stop text head with
          | Error _ as error -> error
          | Ok stop -> Ok (head, stop))
      | Previous_word -> (
          match previous_word_start text head with
          | Error _ as error -> error
          | Ok start -> Ok (head, start))
      | Word_end -> (
          match word_end text head with
          | Error _ as error -> error
          | Ok stop -> Ok (head, stop))
      | Current_word -> word_bounds text head
      | Around_word -> (
          match word_bounds text head with
          | Error _ as error -> error
          | Ok (start, stop) ->
              Ok
                ( start,
                  skip_forward_while text stop (fun word_class ->
                      word_class = Whitespace) ))
      | _ -> assert false)

let line_start text offset =
  let rec loop index =
    if index <= 0 || text.[index - 1] = '\n' then index else loop (index - 1)
  in
  loop offset

let line_stop text offset =
  let rec loop index =
    if index >= String.length text || text.[index] = '\n' then index
    else loop (index + 1)
  in
  loop offset

let line_after text offset =
  let stop = line_stop text offset in
  if stop < String.length text then stop + 1 else stop

let scalar_column text ~start ~offset =
  let rec loop index column =
    if index >= offset then column
    else
      match next_boundary text index with
      | Ok next -> loop next (column + 1)
      | Error _ -> column
  in
  loop start 0

let offset_at_scalar_column text ~start ~stop column =
  let rec loop index remaining =
    if index >= stop || remaining = 0 then index
    else
      match next_boundary text index with
      | Ok next -> loop next (remaining - 1)
      | Error _ -> index
  in
  loop start column

let resolve_line snapshot selector =
  let text = Document_snapshot.contents snapshot in
  selection_heads snapshot (fun head ->
      let start = line_start text head in
      let stop = line_stop text head in
      match selector with
      | Current_line -> Ok (start, line_after text head)
      | Line_start -> Ok (head, start)
      | Line_end -> Ok (head, stop)
      | First_nonblank ->
          let first =
            skip_forward_while text start (fun word_class ->
                word_class = Whitespace)
          in
          Ok (head, if first >= stop then start else first)
      | Document_start -> Ok (head, 0)
      | Document_end -> Ok (head, String.length text)
      | Next_line ->
          let next_start = line_after text head in
          if next_start >= String.length text then
            Error
              (Error.Invalid_selector "next line is unavailable at document end")
          else
            let column = scalar_column text ~start ~offset:head in
            let next_stop = line_stop text next_start in
            Ok
              ( head,
                offset_at_scalar_column text ~start:next_start ~stop:next_stop
                  column )
      | Previous_line ->
          if start = 0 then
            Error
              (Error.Invalid_selector
                 "previous line is unavailable at document start")
          else
            let previous_start = line_start text (start - 1) in
            let previous_stop = line_stop text previous_start in
            let column = scalar_column text ~start ~offset:head in
            Ok
              ( head,
                offset_at_scalar_column text ~start:previous_start
                  ~stop:previous_stop column )
      | _ -> assert false)

let resolve_all_occurrences snapshot =
  let text = Document_snapshot.contents snapshot in
  let primary = Selection_set.primary (Document_snapshot.selections snapshot) in
  let range = Selection.range primary in
  let start = Anchor.byte_offset (Range.start range) in
  let stop = Anchor.byte_offset (Range.stop range) in
  if start = stop then
    Error
      (Error.Invalid_selector
         "all occurrences requires a non-empty primary selection")
  else
    let needle = String.sub text start (stop - start) in
    let rec find offset values primary_index =
      try
        let occurrence = String.index_from text offset needle.[0] in
        if
          occurrence + String.length needle <= String.length text
          && String.sub text occurrence (String.length needle) = needle
        then
          match
            selection_from_offsets snapshot ~anchor_offset:occurrence
              ~head_offset:(occurrence + String.length needle)
          with
          | Error _ as error -> error
          | Ok selection ->
              let primary_index =
                if occurrence = start then List.length values else primary_index
              in
              find
                (occurrence + String.length needle)
                (selection :: values) primary_index
        else find (occurrence + 1) values primary_index
      with Not_found -> (
        match List.rev values with
        | [] ->
            Error (Error.Invalid_selector "selected text has no occurrences")
        | selections -> Selection_set.create ~primary:primary_index selections)
    in
    find 0 [] 0

let resolve snapshot = function
  | Current_selections -> Ok (Document_snapshot.selections snapshot)
  | Document -> (
      match
        selection_from_offsets snapshot ~anchor_offset:0
          ~head_offset:(Document_snapshot.byte_length snapshot)
      with
      | Error _ as error -> error
      | Ok selection -> Selection_set.create ~primary:0 [ selection ])
  | Next_text_unit -> resolve_text_unit snapshot `Next
  | Previous_text_unit -> resolve_text_unit snapshot `Previous
  | (Next_word | Previous_word | Word_end | Current_word | Around_word) as
    selector ->
      resolve_word snapshot selector
  | ( Current_line | Line_start | Line_end | First_nonblank | Document_start
    | Document_end | Next_line | Previous_line ) as selector ->
      resolve_line snapshot selector
  | All_occurrences -> resolve_all_occurrences snapshot
