open Zenbu_model_api

type operator = Delete | Change | Yank
type find_direction = Forward | Backward
type visual_kind = Characterwise | Linewise
type find_motion = { target : string; direction : find_direction; till : bool }

type normal = {
  count : int option;
  slot : Clipboard.slot;
  last_find : find_motion option;
}

type state =
  | Normal of normal
  | Insert of normal
  | Replace of normal
  | Operator_pending of {
      operator : operator;
      normal : normal;
      operator_count : int;
      motion_count : int option;
    }
  | Text_object_pending of {
      operator : operator;
      normal : normal;
      count : int;
      around : bool;
    }
  | Register_prefix of normal
  | Macro_recording_prefix of normal
  | Macro_replay_prefix of normal
  | Go_pending of normal
  | Find_pending of { normal : normal; direction : find_direction; till : bool }
  | Operator_find_pending of {
      operator : operator;
      normal : normal;
      count : int;
      direction : find_direction;
      till : bool;
    }
  | Replace_pending of normal
  | Insert_register_prefix of normal
  | Visual of { normal : normal; kind : visual_kind; anchors : int list }

let static = function
  | Ok value -> value
  | Error _ -> failwith "invalid static Vim-style model declaration"

let provider =
  static
    (Zenbu_kernel.Provider.create ~id:"zenbu.models.vim"
       ~kind:Zenbu_kernel.Provider.Editing_model)

let descriptor =
  static
    (Editing_model.descriptor ~id:"zenbu.vim-style"
       ~title:"Vim-style editing model"
       ~description:
         "A modal editing stress-test with a growing Vim compatibility \
          surface, implemented only through zenbu.model_api."
       ~provider ())

let default_normal =
  { count = None; slot = Clipboard.unnamed; last_find = None }

let initialize _context = Normal default_normal
let reset _state _context = Normal default_normal
let reset_normal normal = { default_normal with last_find = normal.last_find }
let count_value = function None -> 1 | Some value -> value

let count_metadata count =
  match count with
  | None -> []
  | Some count -> [ ("count", string_of_int count) ]

let operator_name = function
  | Delete -> "DELETE"
  | Change -> "CHANGE"
  | Yank -> "YANK"

let status = function
  | Normal { count; slot; _ } ->
      static
        (Model_status.create ~id:"normal" ~label:"NORMAL"
           ~description:"Vim-compatible command state"
           ~metadata:
             (("clipboard-slot", Clipboard.slot_name slot)
             :: count_metadata count)
           ())
  | Insert _ ->
      static
        (Model_status.create ~id:"insert" ~label:"INSERT"
           ~description:"committed text is inserted through semantic intents"
           ~input_mode:Model_status.Text_entry ())
  | Replace _ ->
      static
        (Model_status.create ~id:"replace" ~label:"REPLACE"
           ~description:"committed text replaces successive UTF-8 scalars"
           ~input_mode:Model_status.Text_entry ())
  | Operator_pending { operator; operator_count; motion_count; normal } ->
      static
        (Model_status.create ~id:"operator-pending"
           ~label:(operator_name operator ^ "…")
           ~description:"awaiting a reusable selector or text object"
           ~pending_input:
             (String.lowercase_ascii (String.sub (operator_name operator) 0 1))
           ~metadata:
             [
               ( "count",
                 string_of_int
                   (operator_count
                   * match motion_count with None -> 1 | Some value -> value) );
               ("clipboard-slot", Clipboard.slot_name normal.slot);
             ]
           ())
  | Text_object_pending { operator; count; around; _ } ->
      static
        (Model_status.create ~id:"text-object-pending"
           ~label:(operator_name operator ^ if around then " A…" else " I…")
           ~description:"awaiting a text-object key"
           ~pending_input:(operator_name operator)
           ~metadata:[ ("count", string_of_int count) ]
           ())
  | Register_prefix normal ->
      static
        (Model_status.create ~id:"clipboard-slot-prefix" ~label:"SLOT…"
           ~description:"awaiting a clipboard slot name" ~pending_input:"\""
           ~metadata:(count_metadata normal.count)
           ())
  | Macro_recording_prefix normal ->
      static
        (Model_status.create ~id:"macro-recording-prefix" ~label:"RECORD…"
           ~description:"awaiting a named macro register" ~pending_input:"q"
           ~metadata:(count_metadata normal.count)
           ())
  | Macro_replay_prefix normal ->
      static
        (Model_status.create ~id:"macro-replay-prefix" ~label:"REPLAY…"
           ~description:"awaiting a named macro register" ~pending_input:"@"
           ~metadata:(count_metadata normal.count)
           ())
  | Go_pending normal ->
      static
        (Model_status.create ~id:"go-pending" ~label:"G…"
           ~description:"awaiting the second document-start key"
           ~pending_input:"g"
           ~metadata:(count_metadata normal.count)
           ())
  | Find_pending { direction; till; _ } ->
      let label =
        match (direction, till) with
        | Forward, false -> "FIND…"
        | Backward, false -> "FIND BACK…"
        | Forward, true -> "TILL…"
        | Backward, true -> "TILL BACK…"
      in
      static
        (Model_status.create ~id:"find-pending" ~label
           ~description:"awaiting one UTF-8 scalar find target" ())
  | Operator_find_pending { operator; direction; till; _ } ->
      let find =
        match (direction, till) with
        | Forward, false -> "FIND"
        | Backward, false -> "FIND BACK"
        | Forward, true -> "TILL"
        | Backward, true -> "TILL BACK"
      in
      static
        (Model_status.create ~id:"operator-find-pending"
           ~label:(operator_name operator ^ " " ^ find ^ "…")
           ~description:"awaiting an operator find target" ())
  | Replace_pending _ ->
      static
        (Model_status.create ~id:"replace-pending" ~label:"REPLACE…"
           ~description:"awaiting one UTF-8 scalar replacement" ())
  | Insert_register_prefix _ ->
      static
        (Model_status.create ~id:"insert-register-prefix" ~label:"INSERT SLOT…"
           ~description:"awaiting a clipboard slot to insert"
           ~pending_input:"Ctrl-r" ())
  | Visual { kind; _ } ->
      static
        (Model_status.create ~id:"visual"
           ~label:
             (match kind with
             | Characterwise -> "VISUAL"
             | Linewise -> "VISUAL LINE")
           ~description:"motions extend selections; d, c, and y act on them" ())

let is_text event expected =
  match Input_event.key event with
  | Some (Input_event.Logical_text text) ->
      Input_event.modifiers event = [] && String.equal text expected
  | Some (Input_event.Named_key _) | None -> false

let text_key event =
  match Input_event.key event with
  | Some (Input_event.Logical_text text) when Input_event.modifiers event = []
    ->
      Some text
  | Some (Input_event.Named_key _) | Some (Input_event.Logical_text _) | None ->
      None

let is_named event named =
  match Input_event.key event with
  | Some (Input_event.Named_key value) -> value = named
  | Some (Input_event.Logical_text _) | None -> false

let is_control event text =
  match Input_event.key event with
  | Some (Input_event.Logical_text value) ->
      Input_event.modifiers event = [ Input_event.Control ]
      && String.equal value text
  | Some (Input_event.Named_key _) | None -> false

let add_digit count digit =
  match count with
  | None -> Some digit
  | Some value -> Some ((value * 10) + digit)

let repeated count value = List.init count (fun _ -> value)

let apply selector transformation =
  Semantic_commands.apply ~selector ~transformation

let motion_effect selector transformation count =
  repeated count (apply selector transformation)

let set_selections context selections =
  match
    Model_intent.set_selections ~selections
      ~primary:(Editor_context.selections context).primary_index
  with
  | Ok intent -> [ Model_effect.Execute_intent intent ]
  | Error _ -> []

let selection_heads context =
  Editor_context.selections context |> fun selections ->
  List.map
    (fun selection -> selection.Editor_context.head_offset)
    selections.Editor_context.selections

let set_carets context offsets =
  set_selections context (List.map (fun offset -> (offset, offset)) offsets)

let all_heads_at_end context =
  List.for_all
    (fun selection ->
      selection.Editor_context.head_offset = Editor_context.byte_length context)
    (Editor_context.selections context).Editor_context.selections

let operator_effects operator ~slot ~selector ~linewise count =
  match operator with
  | Delete | Change -> repeated count (apply selector Model_intent.Delete)
  | Yank ->
      [
        Model_effect.Copy_to_clipboard
          {
            slot;
            selector;
            kind =
              (if linewise then Clipboard.Linewise else Clipboard.Characterwise);
          };
      ]

let operator_next_state normal = function
  | Change -> Insert (reset_normal normal)
  | Delete | Yank -> Normal (reset_normal normal)

let run_operator normal operator ~count ~selector ~linewise =
  ( operator_next_state normal operator,
    operator_effects operator ~slot:normal.slot ~selector ~linewise count )

let is_continuation text index =
  index < String.length text && Char.code text.[index] land 0xc0 = 0x80

let next_boundary text offset =
  if offset >= String.length text then None
  else
    let rec loop index =
      if index >= String.length text || not (is_continuation text index) then
        index
      else loop (index + 1)
    in
    Some (loop (offset + 1))

let previous_boundary text offset =
  if offset <= 0 then None
  else
    let rec loop index =
      if not (is_continuation text index) then index else loop (index - 1)
    in
    Some (loop (offset - 1))

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

let rec line_content_stop text count offset =
  let stop = line_stop text offset in
  if count <= 1 || stop >= String.length text then stop
  else line_content_stop text (count - 1) (line_after text offset)

let run_change_line normal count context =
  let text = Editor_context.contents context in
  let selections =
    selection_heads context
    |> List.map (fun head ->
        (line_start text head, line_content_stop text count head))
  in
  ( Insert (reset_normal normal),
    [
      Model_effect.Apply_to_selections
        {
          selections;
          primary = (Editor_context.selections context).primary_index;
          selector_id = "vim.current-line-content";
          action = Model_effect.Transform Model_intent.Delete;
        };
    ] )

type word_class = Whitespace | Word | Punctuation

let word_class_at text offset =
  let code = Char.code text.[offset] in
  if code = Char.code ' ' || code = Char.code '\t' || code = Char.code '\n' then
    Whitespace
  else if
    code >= 0x80
    || (code >= Char.code 'a' && code <= Char.code 'z')
    || (code >= Char.code 'A' && code <= Char.code 'Z')
    || (code >= Char.code '0' && code <= Char.code '9')
    || code = Char.code '_'
  then Word
  else Punctuation

let rec skip_forward text offset predicate =
  if offset >= String.length text || not (predicate (word_class_at text offset))
  then offset
  else
    match next_boundary text offset with
    | None -> String.length text
    | Some next -> skip_forward text next predicate

let rec skip_backward text offset predicate =
  match previous_boundary text offset with
  | None -> 0
  | Some previous ->
      if predicate (word_class_at text previous) then
        skip_backward text previous predicate
      else offset

let next_word text offset =
  if offset >= String.length text then None
  else
    let initial = word_class_at text offset in
    let after_initial =
      skip_forward text offset (fun value -> value = initial)
    in
    Some (skip_forward text after_initial (fun value -> value = Whitespace))

let previous_word text offset =
  if offset <= 0 then None
  else
    let after_space =
      skip_backward text offset (fun value -> value = Whitespace)
    in
    if after_space = 0 then Some 0
    else
      match previous_boundary text after_space with
      | None -> Some 0
      | Some previous ->
          let class_ = word_class_at text previous in
          Some (skip_backward text after_space (fun value -> value = class_))

let word_end text offset =
  let start = skip_forward text offset (fun value -> value = Whitespace) in
  if start >= String.length text then None
  else
    let class_ = word_class_at text start in
    Some (skip_forward text start (fun value -> value = class_))

let scalar_column text ~start ~offset =
  let rec loop index column =
    if index >= offset then column
    else
      match next_boundary text index with
      | None -> column
      | Some next -> loop next (column + 1)
  in
  loop start 0

let offset_at_scalar_column text ~start ~stop column =
  let rec loop index remaining =
    if index >= stop || remaining = 0 then index
    else
      match next_boundary text index with
      | None -> index
      | Some next -> loop next (remaining - 1)
  in
  loop start column

let move_once text key offset =
  match key with
  | "h" -> previous_boundary text offset
  | "l" -> next_boundary text offset
  | "w" -> next_word text offset
  | "b" -> previous_word text offset
  | "e" -> word_end text offset
  | "0" -> Some (line_start text offset)
  | "^" ->
      let start = line_start text offset in
      let stop = line_stop text offset in
      let first = skip_forward text start (fun value -> value = Whitespace) in
      Some (if first >= stop then start else first)
  | "$" -> Some (line_stop text offset)
  | "G" -> Some (String.length text)
  | "j" ->
      let next_start = line_after text offset in
      if next_start >= String.length text then None
      else
        let column =
          scalar_column text ~start:(line_start text offset) ~offset
        in
        Some
          (offset_at_scalar_column text ~start:next_start
             ~stop:(line_stop text next_start)
             column)
  | "k" ->
      let start = line_start text offset in
      if start = 0 then None
      else
        let previous_start = line_start text (start - 1) in
        let column = scalar_column text ~start ~offset in
        Some
          (offset_at_scalar_column text ~start:previous_start
             ~stop:(line_stop text previous_start)
             column)
  | _ -> None

let rec move_count text key count offset =
  if count <= 0 then Some offset
  else
    match move_once text key offset with
    | None -> None
    | Some next -> move_count text key (count - 1) next

let motion key =
  match key with
  | "h" -> Some (Model_intent.Previous_text_unit, Model_intent.Collapse_to_start)
  | "l" -> Some (Model_intent.Next_text_unit, Model_intent.Collapse_to_end)
  | "j" -> Some (Model_intent.Next_line, Model_intent.Collapse_to_end)
  | "k" -> Some (Model_intent.Previous_line, Model_intent.Collapse_to_start)
  | "w" -> Some (Model_intent.Next_word, Model_intent.Collapse_to_end)
  | "b" -> Some (Model_intent.Previous_word, Model_intent.Collapse_to_start)
  | "e" -> Some (Model_intent.Word_end, Model_intent.Collapse_to_end)
  | "0" -> Some (Model_intent.Line_start, Model_intent.Collapse_to_start)
  | "$" -> Some (Model_intent.Line_end, Model_intent.Collapse_to_end)
  | "^" -> Some (Model_intent.First_nonblank, Model_intent.Collapse_to_end)
  | _ -> None

let is_single_scalar text =
  match next_boundary text 0 with
  | Some stop -> stop = String.length text
  | None -> false

let find_offset text ~direction ~target offset =
  let target_length = String.length target in
  let matches index =
    index + target_length <= String.length text
    && String.sub text index target_length = target
  in
  let rec forward index =
    if index >= String.length text then None
    else if matches index then Some index
    else
      match next_boundary text index with
      | None -> None
      | Some next -> forward next
  in
  let rec backward index =
    if matches index then Some index
    else
      match previous_boundary text index with
      | None -> None
      | Some previous -> backward previous
  in
  match direction with
  | Forward -> Option.bind (next_boundary text offset) forward
  | Backward -> Option.bind (previous_boundary text offset) backward

let find_destination text find offset =
  Option.bind
    (find_offset text ~direction:find.direction ~target:find.target offset)
    (fun target ->
      if not find.till then Some target
      else
        match find.direction with
        | Forward -> previous_boundary text target
        | Backward -> next_boundary text target)

let rec find_count text find count offset =
  if count <= 0 then Some offset
  else
    Option.bind
      (find_destination text find offset)
      (find_count text find (count - 1))

let find_effects context find count =
  let text = Editor_context.contents context in
  let offsets =
    selection_heads context |> List.map (find_count text find count)
  in
  if List.exists Option.is_none offsets then []
  else set_carets context (List.map Option.get offsets)

let rec find_target_count text find count offset =
  if count <= 0 then Some offset
  else
    Option.bind
      (find_offset text ~direction:find.direction ~target:find.target offset)
      (fun target ->
        if count = 1 then Some target
        else find_target_count text find (count - 1) target)

let operator_find_endpoint text find count offset =
  Option.bind (find_target_count text find count offset) (fun target ->
      match (find.direction, find.till) with
      | Forward, false -> next_boundary text target
      | Forward, true -> Some target
      | Backward, false -> Some target
      | Backward, true -> next_boundary text target)

let run_find_operator normal operator count find context =
  let text = Editor_context.contents context in
  let selections =
    selection_heads context
    |> List.map (fun head ->
        Option.map
          (fun stop -> (head, stop))
          (operator_find_endpoint text find count head))
  in
  let slot = normal.slot in
  let next_normal = { (reset_normal normal) with last_find = Some find } in
  if List.exists Option.is_none selections then
    (operator_next_state next_normal operator, [])
  else
    let action =
      match operator with
      | Delete | Change -> Model_effect.Transform Model_intent.Delete
      | Yank -> Model_effect.Copy { slot; kind = Clipboard.Characterwise }
    in
    ( operator_next_state next_normal operator,
      [
        Model_effect.Apply_to_selections
          {
            selections = List.map Option.get selections;
            primary = (Editor_context.selections context).primary_index;
            selector_id = "vim.find-motion";
            action;
          };
      ] )

let visual_effects kind anchors context key count =
  let text = Editor_context.contents context in
  let heads = selection_heads context in
  let offsets = List.map (move_count text key count) heads in
  if
    List.length anchors <> List.length offsets
    || List.exists Option.is_none offsets
  then []
  else
    let selections =
      List.map2
        (fun anchor head ->
          let head = Option.get head in
          match kind with
          | Characterwise -> (anchor, head)
          | Linewise ->
              let low = min anchor head in
              let high = max anchor head in
              (line_start text low, line_after text high))
        anchors offsets
    in
    set_selections context selections

let visual_line_effects context =
  let text = Editor_context.contents context in
  selection_heads context
  |> List.map (fun head -> (line_start text head, line_after text head))
  |> set_selections context

let visual_motion_key = function
  | "h" | "j" | "k" | "l" | "w" | "b" | "e" | "0" | "^" | "$" | "G" -> true
  | _ -> false

let normal_input normal event context =
  match text_key event with
  | Some "\"" -> (Register_prefix normal, [])
  | Some "g" -> (Go_pending normal, [])
  | Some "d" ->
      ( Operator_pending
          {
            operator = Delete;
            normal;
            operator_count = count_value normal.count;
            motion_count = None;
          },
        [] )
  | Some "c" ->
      ( Operator_pending
          {
            operator = Change;
            normal;
            operator_count = count_value normal.count;
            motion_count = None;
          },
        [] )
  | Some "y" ->
      ( Operator_pending
          {
            operator = Yank;
            normal;
            operator_count = count_value normal.count;
            motion_count = None;
          },
        [] )
  | Some "i" -> (Insert (reset_normal normal), [])
  | Some "a" ->
      if all_heads_at_end context then (Insert (reset_normal normal), [])
      else
        ( Insert (reset_normal normal),
          motion_effect Model_intent.Next_text_unit Model_intent.Collapse_to_end
            1 )
  | Some "I" ->
      ( Insert (reset_normal normal),
        [ apply Model_intent.First_nonblank Model_intent.Collapse_to_end ] )
  | Some "A" ->
      ( Insert (reset_normal normal),
        [ apply Model_intent.Line_end Model_intent.Collapse_to_end ] )
  | Some "o" ->
      ( Insert (reset_normal normal),
        [
          apply Model_intent.Line_end Model_intent.Collapse_to_end;
          Model_effect.Execute_intent (Model_intent.insert_text "\n");
        ] )
  | Some "O" ->
      ( Insert (reset_normal normal),
        [
          apply Model_intent.Line_start Model_intent.Collapse_to_start;
          Model_effect.Execute_intent (Model_intent.insert_text "\n");
          apply Model_intent.Previous_text_unit Model_intent.Collapse_to_start;
        ] )
  | Some "R" -> (Replace (reset_normal normal), [])
  | Some "r" -> (Replace_pending normal, [])
  | Some "v" ->
      ( Visual
          {
            normal = reset_normal normal;
            kind = Characterwise;
            anchors = selection_heads context;
          },
        [] )
  | Some "V" ->
      ( Visual
          {
            normal = reset_normal normal;
            kind = Linewise;
            anchors = selection_heads context;
          },
        visual_line_effects context )
  | Some "f" -> (Find_pending { normal; direction = Forward; till = false }, [])
  | Some "F" -> (Find_pending { normal; direction = Backward; till = false }, [])
  | Some "t" -> (Find_pending { normal; direction = Forward; till = true }, [])
  | Some "T" -> (Find_pending { normal; direction = Backward; till = true }, [])
  | Some ";" -> (
      match normal.last_find with
      | None -> (Normal (reset_normal normal), [])
      | Some find ->
          ( Normal (reset_normal normal),
            find_effects context find (count_value normal.count) ))
  | Some "," -> (
      match normal.last_find with
      | None -> (Normal (reset_normal normal), [])
      | Some find ->
          let direction =
            match find.direction with
            | Forward -> Backward
            | Backward -> Forward
          in
          ( Normal (reset_normal normal),
            find_effects context { find with direction }
              (count_value normal.count) ))
  | Some "x" ->
      ( Normal (reset_normal normal),
        repeated (count_value normal.count)
          (apply Model_intent.Next_text_unit Model_intent.Delete) )
  | Some "X" ->
      ( Normal (reset_normal normal),
        repeated (count_value normal.count)
          (apply Model_intent.Previous_text_unit Model_intent.Delete) )
  | Some "s" ->
      ( Insert (reset_normal normal),
        repeated (count_value normal.count)
          (apply Model_intent.Next_text_unit Model_intent.Delete) )
  | Some "D" ->
      run_operator normal Delete ~count:(count_value normal.count)
        ~selector:Model_intent.Line_end ~linewise:false
  | Some "C" ->
      run_operator normal Change ~count:(count_value normal.count)
        ~selector:Model_intent.Line_end ~linewise:false
  | Some "S" -> run_change_line normal (count_value normal.count) context
  | Some "p" ->
      ( Normal (reset_normal normal),
        [
          Model_effect.Paste_from_clipboard
            { slot = normal.slot; placement = Clipboard.After };
        ] )
  | Some "P" ->
      ( Normal (reset_normal normal),
        [
          Model_effect.Paste_from_clipboard
            { slot = normal.slot; placement = Clipboard.Before };
        ] )
  | Some "q" -> (
      match Editor_context.macro_recording_register context with
      | Some register ->
          ( Normal (reset_normal normal),
            [
              Model_effect.Request_macro
                (Model_effect.Toggle_macro_recording register);
            ] )
      | None ->
          ( Macro_recording_prefix normal,
            [ Model_effect.Request_macro Model_effect.Reserve_macro_input ] ))
  | Some "@" ->
      ( Macro_replay_prefix normal,
        [ Model_effect.Request_macro Model_effect.Reserve_macro_input ] )
  | Some "u" -> (Normal (reset_normal normal), [ Model_effect.Undo ])
  | Some "." -> (Normal (reset_normal normal), [ Model_effect.Repeat_last_edit ])
  | Some "/" ->
      ( Normal (reset_normal normal),
        [ Model_effect.Request_search Model_effect.Forward ] )
  | Some "?" ->
      ( Normal (reset_normal normal),
        [ Model_effect.Request_search Model_effect.Backward ] )
  | Some "n" ->
      ( Normal (reset_normal normal),
        [ Model_effect.Repeat_search Model_effect.Forward ] )
  | Some "N" ->
      ( Normal (reset_normal normal),
        [ Model_effect.Repeat_search Model_effect.Backward ] )
  | Some "G" ->
      ( Normal (reset_normal normal),
        motion_effect Model_intent.Document_end Model_intent.Collapse_to_end 1
      )
  | Some value
    when String.length value = 1 && value.[0] >= '1' && value.[0] <= '9' ->
      ( Normal
          {
            normal with
            count = add_digit normal.count (Char.code value.[0] - Char.code '0');
          },
        [] )
  | Some "0" when Option.is_some normal.count ->
      (Normal { normal with count = add_digit normal.count 0 }, [])
  | Some key -> (
      match motion key with
      | Some (selector, transformation) ->
          ( Normal (reset_normal normal),
            motion_effect selector transformation (count_value normal.count) )
      | None -> (Normal (reset_normal normal), []))
  | None when is_control event "r" ->
      (Normal (reset_normal normal), [ Model_effect.Redo ])
  | None when is_named event Input_event.Escape ->
      (Normal (reset_normal normal), [])
  | None -> (Normal normal, [])

let pending_input operator normal operator_count motion_count event context =
  let count = operator_count * count_value motion_count in
  match text_key event with
  | Some "i" ->
      (Text_object_pending { operator; normal; count; around = false }, [])
  | Some "a" ->
      (Text_object_pending { operator; normal; count; around = true }, [])
  | Some "f" ->
      ( Operator_find_pending
          { operator; normal; count; direction = Forward; till = false },
        [] )
  | Some "F" ->
      ( Operator_find_pending
          { operator; normal; count; direction = Backward; till = false },
        [] )
  | Some "t" ->
      ( Operator_find_pending
          { operator; normal; count; direction = Forward; till = true },
        [] )
  | Some "T" ->
      ( Operator_find_pending
          { operator; normal; count; direction = Backward; till = true },
        [] )
  | Some "d" when operator = Delete ->
      run_operator normal operator ~count ~selector:Model_intent.Current_line
        ~linewise:true
  | Some "c" when operator = Change -> run_change_line normal count context
  | Some "y" when operator = Yank ->
      run_operator normal operator ~count ~selector:Model_intent.Current_line
        ~linewise:true
  | Some value
    when String.length value = 1 && value.[0] >= '1' && value.[0] <= '9' ->
      ( Operator_pending
          {
            operator;
            normal;
            operator_count;
            motion_count =
              add_digit motion_count (Char.code value.[0] - Char.code '0');
          },
        [] )
  | Some "0" when Option.is_some motion_count ->
      ( Operator_pending
          {
            operator;
            normal;
            operator_count;
            motion_count = add_digit motion_count 0;
          },
        [] )
  | Some key -> (
      match motion key with
      | Some (selector, _) ->
          run_operator normal operator ~count ~selector ~linewise:false
      | None -> (Normal (reset_normal normal), []))
  | None when is_named event Input_event.Escape ->
      (Normal (reset_normal normal), [])
  | None ->
      (Operator_pending { operator; normal; operator_count; motion_count }, [])

let text_object_input operator normal count around event =
  match text_key event with
  | Some "w" ->
      run_operator normal operator ~count
        ~selector:
          (if around then Model_intent.Around_word
           else Model_intent.Current_word)
        ~linewise:false
  | _ when is_named event Input_event.Escape ->
      (Normal (reset_normal normal), [])
  | _ -> (Text_object_pending { operator; normal; count; around }, [])

let find_input normal direction till event context =
  match text_key event with
  | Some target when is_single_scalar target ->
      let find = { target; direction; till } in
      let next = Normal { (reset_normal normal) with last_find = Some find } in
      (next, find_effects context find (count_value normal.count))
  | _ when is_named event Input_event.Escape ->
      (Normal (reset_normal normal), [])
  | _ -> (Find_pending { normal; direction; till }, [])

let operator_find_input operator normal count direction till event context =
  match text_key event with
  | Some target when is_single_scalar target ->
      run_find_operator normal operator count
        { target; direction; till }
        context
  | _ when is_named event Input_event.Escape ->
      (Normal (reset_normal normal), [])
  | _ -> (Operator_find_pending { operator; normal; count; direction; till }, [])

let replace_input normal event _context =
  match text_key event with
  | Some text when is_single_scalar text ->
      ( Normal (reset_normal normal),
        repeated (count_value normal.count)
          (apply Model_intent.Next_text_unit (Model_intent.Replace_text text))
      )
  | _ when is_named event Input_event.Escape ->
      (Normal (reset_normal normal), [])
  | _ -> (Replace_pending normal, [])

let replace_text_input normal event context =
  match Input_event.text event with
  | None -> (Replace normal, [])
  | Some text when all_heads_at_end context ->
      ( Replace normal,
        [ Model_effect.Execute_intent (Model_intent.insert_text text) ] )
  | Some text ->
      ( Replace normal,
        [ apply Model_intent.Next_text_unit (Model_intent.Replace_text text) ]
      )

let insert_input normal event =
  if is_named event Input_event.Escape then (Normal (reset_normal normal), [])
  else if is_named event Input_event.Backspace then
    ( Insert normal,
      [ apply Model_intent.Previous_text_unit Model_intent.Delete ] )
  else if is_named event Input_event.Enter then
    ( Insert normal,
      [ Model_effect.Execute_intent (Model_intent.insert_text "\n") ] )
  else if is_control event "w" then
    (Insert normal, [ apply Model_intent.Previous_word Model_intent.Delete ])
  else if is_control event "u" then
    (Insert normal, [ apply Model_intent.Line_start Model_intent.Delete ])
  else if is_control event "r" then (Insert_register_prefix normal, [])
  else
    match Input_event.text event with
    | Some text ->
        ( Insert normal,
          [ Model_effect.Execute_intent (Model_intent.insert_text text) ] )
    | None -> (Insert normal, [])

let visual_input normal kind anchors event context =
  let reset = Normal (reset_normal normal) in
  match text_key event with
  | Some "d" ->
      (reset, [ apply Model_intent.Current_selections Model_intent.Delete ])
  | Some "c" ->
      ( Insert (reset_normal normal),
        [ apply Model_intent.Current_selections Model_intent.Delete ] )
  | Some "y" ->
      ( reset,
        [
          Model_effect.Copy_to_clipboard
            {
              slot = normal.slot;
              selector = Model_intent.Current_selections;
              kind =
                (match kind with
                | Characterwise -> Clipboard.Characterwise
                | Linewise -> Clipboard.Linewise);
            };
        ] )
  | Some ("v" | "V") -> (reset, set_carets context (selection_heads context))
  | Some key ->
      if visual_motion_key key then
        ( Visual { normal; kind; anchors },
          visual_effects kind anchors context key 1 )
      else (Visual { normal; kind; anchors }, [])
  | None when is_named event Input_event.Escape ->
      (reset, set_carets context (selection_heads context))
  | None -> (Visual { normal; kind; anchors }, [])

let handle_input state event context =
  match state with
  | Normal normal -> normal_input normal event context
  | Insert normal -> insert_input normal event
  | Replace normal when is_named event Input_event.Escape ->
      (Normal (reset_normal normal), [])
  | Replace normal when is_named event Input_event.Backspace ->
      ( Replace normal,
        [ apply Model_intent.Previous_text_unit Model_intent.Delete ] )
  | Replace normal -> replace_text_input normal event context
  | Operator_pending { operator; normal; operator_count; motion_count } ->
      pending_input operator normal operator_count motion_count event context
  | Text_object_pending { operator; normal; count; around } ->
      text_object_input operator normal count around event
  | Register_prefix normal -> (
      match text_key event with
      | Some value
        when String.length value = 1 && value.[0] >= 'a' && value.[0] <= 'z' ->
          let slot = static (Clipboard.slot value) in
          (Normal { normal with slot }, [])
      | _ when is_named event Input_event.Escape ->
          (Normal (reset_normal normal), [])
      | _ -> (Register_prefix normal, []))
  | Macro_recording_prefix normal -> (
      match text_key event with
      | Some register when is_single_scalar register ->
          ( Normal (reset_normal normal),
            [
              Model_effect.Request_macro
                (Model_effect.Toggle_macro_recording register);
            ] )
      | _ when is_named event Input_event.Escape ->
          (Normal (reset_normal normal), [])
      | _ -> (Macro_recording_prefix normal, []))
  | Macro_replay_prefix normal -> (
      match text_key event with
      | Some register when is_single_scalar register ->
          ( Normal (reset_normal normal),
            [
              Model_effect.Request_macro
                (Model_effect.Replay_macro
                   { register; count = count_value normal.count });
            ] )
      | _ when is_named event Input_event.Escape ->
          (Normal (reset_normal normal), [])
      | _ -> (Macro_replay_prefix normal, []))
  | Go_pending normal when is_text event "g" ->
      ( Normal (reset_normal normal),
        motion_effect Model_intent.Document_start Model_intent.Collapse_to_start
          1 )
  | Go_pending normal when is_named event Input_event.Escape ->
      (Normal (reset_normal normal), [])
  | Go_pending normal -> (Go_pending normal, [])
  | Find_pending { normal; direction; till } ->
      find_input normal direction till event context
  | Operator_find_pending { operator; normal; count; direction; till } ->
      operator_find_input operator normal count direction till event context
  | Replace_pending normal -> replace_input normal event context
  | Insert_register_prefix normal -> (
      match text_key event with
      | Some value
        when String.length value = 1 && value.[0] >= 'a' && value.[0] <= 'z' ->
          let slot = static (Clipboard.slot value) in
          ( Insert normal,
            [
              Model_effect.Paste_from_clipboard
                { slot; placement = Clipboard.Replace };
            ] )
      | _ when is_named event Input_event.Escape -> (Insert normal, [])
      | _ -> (Insert_register_prefix normal, []))
  | Visual { normal; kind; anchors } ->
      visual_input normal kind anchors event context

let input_rule ?next_status ?selector_id ?transformation_id id pattern kind
    summary =
  static
    (Input_rule.create ~id ~pattern ~kind ~summary ?next_status ?selector_id
       ?transformation_id ())

let input_rules = function
  | Normal _ ->
      [
        input_rule "vim.normal.motion"
          (Input_rule.Text_range "h, j, k, l, w, b, e, 0, ^, $, g g, or G")
          Input_rule.Binding "move through UTF-8-safe text and line targets";
        input_rule "vim.normal.find"
          (Input_rule.Text_range "f, F, t, T, ;, or ,") Input_rule.Binding
          "find and repeat a UTF-8 scalar motion";
        input_rule "vim.normal.delete" (Input_rule.Exact "d") Input_rule.Prefix
          "begin a delete operator" ~next_status:"operator-pending";
        input_rule "vim.normal.change" (Input_rule.Exact "c") Input_rule.Prefix
          "begin a change operator" ~next_status:"operator-pending";
        input_rule "vim.normal.yank" (Input_rule.Exact "y") Input_rule.Prefix
          "begin a copy operator" ~next_status:"operator-pending";
        input_rule "vim.normal.insert"
          (Input_rule.Text_range "i, a, I, A, o, or O") Input_rule.Binding
          "enter insert mode at a Vim-compatible insertion boundary"
          ~next_status:"insert";
        input_rule "vim.normal.replace" (Input_rule.Text_range "r or R")
          Input_rule.Binding "replace one scalar or enter replace mode";
        input_rule "vim.normal.visual" (Input_rule.Text_range "v or V")
          Input_rule.Binding "enter characterwise or linewise visual selection"
          ~next_status:"visual";
        input_rule "vim.normal.character-delete"
          (Input_rule.Text_range "x, X, s, D, C, or S") Input_rule.Binding
          "delete or change a neighboring scalar or line target";
        input_rule "vim.normal.paste" (Input_rule.Text_range "p or P")
          Input_rule.Binding "paste the selected clipboard entry";
        input_rule "vim.normal.search" (Input_rule.Text_range "/, ?, n, or N")
          Input_rule.Binding "request or repeat model-neutral literal search";
        input_rule "vim.normal.history" (Input_rule.Text_range "u or Ctrl-r")
          Input_rule.Binding "move through shared history";
        input_rule "vim.normal.repeat" (Input_rule.Exact ".") Input_rule.Binding
          "repeat the latest semantic edit";
        input_rule "vim.normal.escape" (Input_rule.Named "Escape")
          Input_rule.Binding "cancel pending input or return to normal state";
        input_rule "vim.normal.count" (Input_rule.Text_range "1-9")
          Input_rule.Prefix "begin or extend a count";
        input_rule "vim.normal.slot" (Input_rule.Exact "\"") Input_rule.Prefix
          "choose a clipboard slot" ~next_status:"clipboard-slot-prefix";
        input_rule "vim.normal.macro-record" (Input_rule.Exact "q")
          Input_rule.Prefix "start or stop recording a named macro"
          ~next_status:"macro-recording-prefix";
        input_rule "vim.normal.macro-replay" (Input_rule.Exact "@")
          Input_rule.Prefix "replay a named macro"
          ~next_status:"macro-replay-prefix";
      ]
  | Insert _ ->
      [
        input_rule "vim.insert.text" Input_rule.Text_input Input_rule.Catch_all
          "insert committed text" ~transformation_id:"replace-text";
        input_rule "vim.insert.escape" (Input_rule.Named "Escape")
          Input_rule.Binding "return to normal state" ~next_status:"normal";
        input_rule "vim.insert.backspace" (Input_rule.Named "Backspace")
          Input_rule.Binding "delete the previous text unit";
        input_rule "vim.insert.word-delete" (Input_rule.Exact "Ctrl-w")
          Input_rule.Binding "delete through the previous word";
        input_rule "vim.insert.line-delete" (Input_rule.Exact "Ctrl-u")
          Input_rule.Binding "delete to the start of the current line";
        input_rule "vim.insert.register" (Input_rule.Exact "Ctrl-r")
          Input_rule.Prefix "insert a selected clipboard slot"
          ~next_status:"insert-register-prefix";
      ]
  | Replace _ ->
      [
        input_rule "vim.replace.text" Input_rule.Text_input Input_rule.Catch_all
          "replace the next text unit with committed text";
        input_rule "vim.replace.escape" (Input_rule.Named "Escape")
          Input_rule.Binding "return to normal state" ~next_status:"normal";
      ]
  | Operator_pending _ ->
      [
        input_rule "vim.operator.word" (Input_rule.Exact "w") Input_rule.Binding
          "apply the pending operator through the next word"
          ~selector_id:"next-word";
        input_rule "vim.operator.motion" (Input_rule.Text_range "motion")
          Input_rule.Binding "apply the pending operator through a motion";
        input_rule "vim.operator.inner" (Input_rule.Exact "i") Input_rule.Prefix
          "begin an inner text object" ~next_status:"text-object-pending";
        input_rule "vim.operator.around" (Input_rule.Exact "a")
          Input_rule.Prefix "begin an around text object"
          ~next_status:"text-object-pending";
        input_rule "vim.operator.cancel" (Input_rule.Named "Escape")
          Input_rule.Binding "cancel the pending operator" ~next_status:"normal";
      ]
  | Text_object_pending _ ->
      [
        input_rule "vim.text-object.word" (Input_rule.Exact "w")
          Input_rule.Binding "apply the pending operator to a word";
        input_rule "vim.text-object.cancel" (Input_rule.Named "Escape")
          Input_rule.Binding "cancel the pending operator" ~next_status:"normal";
      ]
  | Register_prefix _ | Insert_register_prefix _ ->
      [
        input_rule "vim.slot.letter" (Input_rule.Text_range "a-z")
          Input_rule.Binding "select a clipboard slot";
        input_rule "vim.slot.cancel" (Input_rule.Named "Escape")
          Input_rule.Binding "cancel clipboard slot selection";
      ]
  | Macro_recording_prefix _ | Macro_replay_prefix _ ->
      [
        input_rule "vim.macro.register"
          (Input_rule.Text_range "one UTF-8 scalar") Input_rule.Binding
          "select a named macro register";
        input_rule "vim.macro.cancel" (Input_rule.Named "Escape")
          Input_rule.Binding "cancel macro register selection"
          ~next_status:"normal";
      ]
  | Go_pending _ ->
      [
        input_rule "vim.go.complete" (Input_rule.Exact "g") Input_rule.Binding
          "move to document start" ~next_status:"normal"
          ~selector_id:"document-start" ~transformation_id:"collapse-to-start";
        input_rule "vim.go.cancel" (Input_rule.Named "Escape")
          Input_rule.Binding "cancel document-start input" ~next_status:"normal";
      ]
  | Find_pending _ | Operator_find_pending _ | Replace_pending _ ->
      [
        input_rule "vim.pending.text" (Input_rule.Text_range "one UTF-8 scalar")
          Input_rule.Binding "provide one UTF-8 scalar";
        input_rule "vim.pending.cancel" (Input_rule.Named "Escape")
          Input_rule.Binding "cancel the pending command" ~next_status:"normal";
      ]
  | Visual _ ->
      [
        input_rule "vim.visual.motion" (Input_rule.Text_range "motion")
          Input_rule.Binding "extend the visual selection";
        input_rule "vim.visual.operator" (Input_rule.Text_range "d, c, or y")
          Input_rule.Binding "apply an operator to the visual selection";
        input_rule "vim.visual.exit" (Input_rule.Text_range "v, V, or Escape")
          Input_rule.Binding "leave visual selection mode" ~next_status:"normal";
      ]
