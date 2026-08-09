open Zenbu_model_api

type operator = Delete | Change | Yank
type normal = { count : int option; slot : Clipboard.slot }

type state =
  | Normal of normal
  | Insert
  | Operator_pending of {
      operator : operator;
      operator_count : int;
      motion_count : int option;
      slot : Clipboard.slot;
    }
  | Text_object_pending of {
      operator : operator;
      count : int;
      slot : Clipboard.slot;
      around : bool;
    }
  | Register_prefix of { count : int option }
  | Go_pending of { count : int option }

let static = function
  | Ok value -> value
  | Error _ -> failwith "invalid static Vim-style model declaration"

let descriptor =
  static
    (Editing_model.descriptor ~id:"zenbu.vim-style"
       ~title:"Vim-style editing model"
       ~description:
         "A documented Vim-inspired modal subset implemented only through \
          zenbu.model_api."
       ())

let default_normal = { count = None; slot = Clipboard.unnamed }
let initialize _context = Normal default_normal
let reset _state _context = Normal default_normal

let count_metadata count =
  match count with
  | None -> []
  | Some count -> [ ("count", string_of_int count) ]

let status = function
  | Normal { count; slot } ->
      static
        (Model_status.create ~id:"normal" ~label:"NORMAL"
           ~description:"Vim-style command state"
           ~metadata:
             (("clipboard-slot", Clipboard.slot_name slot)
             :: count_metadata count)
           ())
  | Insert ->
      static
        (Model_status.create ~id:"insert" ~label:"INSERT"
           ~description:"committed text is inserted through semantic intents" ())
  | Operator_pending { operator; operator_count; motion_count; slot } ->
      let name =
        match operator with
        | Delete -> "DELETE"
        | Change -> "CHANGE"
        | Yank -> "YANK"
      in
      static
        (Model_status.create ~id:"operator-pending" ~label:(name ^ "…")
           ~description:"awaiting a reusable selector or text object"
           ~pending_input:(String.lowercase_ascii (String.sub name 0 1))
           ~metadata:
             [
               ( "count",
                 string_of_int
                   (operator_count
                   * match motion_count with None -> 1 | Some value -> value) );
               ("clipboard-slot", Clipboard.slot_name slot);
             ]
           ())
  | Text_object_pending { operator; count; around; _ } ->
      let name =
        match operator with
        | Delete -> "DELETE"
        | Change -> "CHANGE"
        | Yank -> "YANK"
      in
      static
        (Model_status.create ~id:"text-object-pending"
           ~label:(name ^ if around then " A…" else " I…")
           ~description:"awaiting a text-object key" ~pending_input:name
           ~metadata:[ ("count", string_of_int count) ]
           ())
  | Register_prefix { count } ->
      static
        (Model_status.create ~id:"clipboard-slot-prefix" ~label:"SLOT…"
           ~description:"awaiting a clipboard slot name" ~pending_input:"\""
           ~metadata:(count_metadata count) ())
  | Go_pending { count } ->
      static
        (Model_status.create ~id:"go-pending" ~label:"G…"
           ~description:"awaiting the second document-start key"
           ~pending_input:"g" ~metadata:(count_metadata count) ())

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

let count_value = function None -> 1 | Some value -> value

let add_digit count digit =
  match count with
  | None -> Some digit
  | Some value -> Some ((value * 10) + digit)

let repeated count value = List.init count (fun _ -> value)

let apply selector transformation =
  Semantic_commands.apply ~selector ~transformation

let motion_effect selector transformation count =
  repeated count (apply selector transformation)

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

let operator_effects operator ~slot ~selector ~linewise count =
  match operator with
  | Delete -> repeated count (apply selector Model_intent.Delete)
  | Change -> repeated count (apply selector Model_intent.Delete)
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

let operator_next_state operator =
  match operator with
  | Change -> Insert
  | Delete | Yank -> Normal default_normal

let run_operator operator ~count ~slot ~selector ~linewise =
  ( operator_next_state operator,
    operator_effects operator ~slot ~selector ~linewise count )

let all_heads_at_end context =
  let selections = Editor_context.selections context in
  List.for_all
    (fun selection ->
      selection.Editor_context.head_offset = Editor_context.byte_length context)
    selections.Editor_context.selections

let normal_input normal event context =
  match text_key event with
  | Some "\"" -> (Register_prefix { count = normal.count }, [])
  | Some "g" -> (Go_pending { count = normal.count }, [])
  | Some "d" ->
      ( Operator_pending
          {
            operator = Delete;
            operator_count = count_value normal.count;
            motion_count = None;
            slot = normal.slot;
          },
        [] )
  | Some "c" ->
      ( Operator_pending
          {
            operator = Change;
            operator_count = count_value normal.count;
            motion_count = None;
            slot = normal.slot;
          },
        [] )
  | Some "y" ->
      ( Operator_pending
          {
            operator = Yank;
            operator_count = count_value normal.count;
            motion_count = None;
            slot = normal.slot;
          },
        [] )
  | Some "i" -> (Insert, [])
  | Some "a" ->
      if all_heads_at_end context then (Insert, [])
      else
        ( Insert,
          motion_effect Model_intent.Next_text_unit Model_intent.Collapse_to_end
            1 )
  | Some "x" ->
      ( Normal default_normal,
        repeated (count_value normal.count)
          (apply Model_intent.Next_text_unit Model_intent.Delete) )
  | Some "X" ->
      ( Normal default_normal,
        repeated (count_value normal.count)
          (apply Model_intent.Previous_text_unit Model_intent.Delete) )
  | Some "s" ->
      ( Insert,
        repeated (count_value normal.count)
          (apply Model_intent.Next_text_unit Model_intent.Delete) )
  | Some "p" ->
      ( Normal default_normal,
        [
          Model_effect.Paste_from_clipboard
            { slot = normal.slot; placement = Clipboard.After };
        ] )
  | Some "P" ->
      ( Normal default_normal,
        [
          Model_effect.Paste_from_clipboard
            { slot = normal.slot; placement = Clipboard.Before };
        ] )
  | Some "u" -> (Normal default_normal, [ Model_effect.Undo ])
  | Some "." -> (Normal default_normal, [ Model_effect.Repeat_last_edit ])
  | Some "G" ->
      ( Normal default_normal,
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
          ( Normal default_normal,
            motion_effect selector transformation (count_value normal.count) )
      | None -> (Normal default_normal, []))
  | None when is_control event "r" ->
      (Normal default_normal, [ Model_effect.Redo ])
  | None when is_named event Input_event.Escape -> (Normal default_normal, [])
  | None -> (Normal normal, [])

let pending_input operator operator_count motion_count slot event =
  let count = operator_count * count_value motion_count in
  match text_key event with
  | Some "i" ->
      (Text_object_pending { operator; count; slot; around = false }, [])
  | Some "a" ->
      (Text_object_pending { operator; count; slot; around = true }, [])
  | Some "d" when operator = Delete ->
      run_operator operator ~count ~slot ~selector:Model_intent.Current_line
        ~linewise:true
  | Some "c" when operator = Change ->
      run_operator operator ~count ~slot ~selector:Model_intent.Current_line
        ~linewise:true
  | Some "y" when operator = Yank ->
      run_operator operator ~count ~slot ~selector:Model_intent.Current_line
        ~linewise:true
  | Some value
    when String.length value = 1 && value.[0] >= '1' && value.[0] <= '9' ->
      ( Operator_pending
          {
            operator;
            operator_count;
            motion_count =
              add_digit motion_count (Char.code value.[0] - Char.code '0');
            slot;
          },
        [] )
  | Some "0" when Option.is_some motion_count ->
      ( Operator_pending
          {
            operator;
            operator_count;
            motion_count = add_digit motion_count 0;
            slot;
          },
        [] )
  | Some key -> (
      match motion key with
      | Some (selector, _) ->
          run_operator operator ~count ~slot ~selector ~linewise:false
      | None -> (Normal default_normal, []))
  | None when is_named event Input_event.Escape -> (Normal default_normal, [])
  | None ->
      (Operator_pending { operator; operator_count; motion_count; slot }, [])

let text_object_input operator count slot around event =
  match text_key event with
  | Some "w" ->
      run_operator operator ~count ~slot
        ~selector:
          (if around then Model_intent.Around_word
           else Model_intent.Current_word)
        ~linewise:false
  | _ when is_named event Input_event.Escape -> (Normal default_normal, [])
  | _ -> (Text_object_pending { operator; count; slot; around }, [])

let handle_input state event context =
  match state with
  | Normal normal -> normal_input normal event context
  | Insert when is_named event Input_event.Escape -> (Normal default_normal, [])
  | Insert when is_named event Input_event.Backspace ->
      (Insert, [ apply Model_intent.Previous_text_unit Model_intent.Delete ])
  | Insert when is_named event Input_event.Enter ->
      (Insert, [ Model_effect.Execute_intent (Model_intent.insert_text "\n") ])
  | Insert -> (
      match Input_event.text event with
      | Some text ->
          ( Insert,
            [ Model_effect.Execute_intent (Model_intent.insert_text text) ] )
      | None -> (Insert, []))
  | Operator_pending { operator; operator_count; motion_count; slot } ->
      pending_input operator operator_count motion_count slot event
  | Text_object_pending { operator; count; slot; around } ->
      text_object_input operator count slot around event
  | Register_prefix { count } -> (
      match text_key event with
      | Some value
        when String.length value = 1 && value.[0] >= 'a' && value.[0] <= 'z' ->
          let slot = static (Clipboard.slot value) in
          (Normal { count; slot }, [])
      | _ when is_named event Input_event.Escape -> (Normal default_normal, [])
      | _ -> (Register_prefix { count }, []))
  | Go_pending { count = _ } when is_text event "g" ->
      ( Normal default_normal,
        motion_effect Model_intent.Document_start Model_intent.Collapse_to_start
          1 )
  | Go_pending _ when is_named event Input_event.Escape ->
      (Normal default_normal, [])
  | Go_pending pending -> (Go_pending pending, [])
