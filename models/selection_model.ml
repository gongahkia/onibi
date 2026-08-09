open Zenbu_model_api

type selecting = { count : int option; slot : Clipboard.slot }

type state =
  | Select of selecting
  | Insert
  | Register_prefix of { count : int option }
  | Go_pending of { count : int option }

let static = function
  | Ok value -> value
  | Error _ -> failwith "invalid static selection-first model declaration"

let descriptor =
  static
    (Editing_model.descriptor ~id:"zenbu.selection-first"
       ~title:"Selection-first editing model"
       ~description:
         "A Kakoune/Helix-inspired select-then-transform model using shared \
          Zenbu semantics."
       ())

let default_select = { count = None; slot = Clipboard.unnamed }
let initialize _context = Select default_select
let reset _state _context = Select default_select

let count_metadata count =
  match count with
  | None -> []
  | Some count -> [ ("count", string_of_int count) ]

let status = function
  | Select { count; slot } ->
      static
        (Model_status.create ~id:"select" ~label:"SELECT"
           ~description:"selectors visibly update the active selection set"
           ~metadata:
             (("clipboard-slot", Clipboard.slot_name slot)
             :: count_metadata count)
           ())
  | Insert ->
      static
        (Model_status.create ~id:"insert" ~label:"INSERT"
           ~description:"committed text replaces active selections semantically"
           ~input_mode:Model_status.Text_entry ())
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

let selector key =
  match key with
  | "h" -> Some Model_intent.Previous_text_unit
  | "l" -> Some Model_intent.Next_text_unit
  | "j" -> Some Model_intent.Next_line
  | "k" -> Some Model_intent.Previous_line
  | "w" -> Some Model_intent.Next_word
  | "b" -> Some Model_intent.Previous_word
  | "e" -> Some Model_intent.Word_end
  | "W" -> Some Model_intent.Current_word
  | "0" -> Some Model_intent.Line_start
  | "$" -> Some Model_intent.Line_end
  | "^" -> Some Model_intent.First_nonblank
  | "L" -> Some Model_intent.Current_line
  | "*" -> Some Model_intent.All_occurrences
  | _ -> None

let retain_primary context =
  let selections = Editor_context.selections context in
  let primary =
    List.nth selections.Editor_context.selections selections.primary_index
  in
  match
    Model_intent.set_selections
      ~selections:
        [
          ( primary.Editor_context.anchor_offset,
            primary.Editor_context.head_offset );
        ]
      ~primary:0
  with
  | Ok intent -> [ Model_effect.Execute_intent intent ]
  | Error _ -> []

let has_nonempty_selection context =
  Editor_context.selections context |> fun selections ->
  List.exists
    (fun selection ->
      selection.Editor_context.anchor_offset
      <> selection.Editor_context.head_offset)
    selections.Editor_context.selections

let select_input selecting event context =
  match text_key event with
  | Some "\"" -> (Register_prefix { count = selecting.count }, [])
  | Some "g" -> (Go_pending { count = selecting.count }, [])
  | Some "d" | Some "x" ->
      ( Select default_select,
        if has_nonempty_selection context then
          [ apply Model_intent.Current_selections Model_intent.Delete ]
        else [] )
  | Some "c" ->
      ( Insert,
        if has_nonempty_selection context then
          [ apply Model_intent.Current_selections Model_intent.Delete ]
        else [] )
  | Some "y" ->
      ( Select default_select,
        [
          Model_effect.Copy_to_clipboard
            {
              slot = selecting.slot;
              selector = Model_intent.Current_selections;
              kind = Clipboard.Characterwise;
            };
        ] )
  | Some "p" ->
      ( Select default_select,
        [
          Model_effect.Paste_from_clipboard
            { slot = selecting.slot; placement = Clipboard.Replace };
        ] )
  | Some "P" ->
      ( Select default_select,
        [
          Model_effect.Paste_from_clipboard
            { slot = selecting.slot; placement = Clipboard.Before };
        ] )
  | Some "i" -> (Insert, [])
  | Some "u" -> (Select default_select, [ Model_effect.Undo ])
  | Some "." -> (Select default_select, [ Model_effect.Repeat_last_edit ])
  | Some "," -> (Select default_select, retain_primary context)
  | Some "G" ->
      ( Select default_select,
        [ apply Model_intent.Document_end Model_intent.Select ] )
  | Some value
    when String.length value = 1 && value.[0] >= '1' && value.[0] <= '9' ->
      ( Select
          {
            selecting with
            count =
              add_digit selecting.count (Char.code value.[0] - Char.code '0');
          },
        [] )
  | Some "0" when Option.is_some selecting.count ->
      (Select { selecting with count = add_digit selecting.count 0 }, [])
  | Some key -> (
      match selector key with
      | Some selector ->
          ( Select default_select,
            repeated
              (count_value selecting.count)
              (apply selector Model_intent.Select) )
      | None -> (Select default_select, []))
  | None when is_control event "r" ->
      (Select default_select, [ Model_effect.Redo ])
  | None when is_named event Input_event.Escape ->
      ( Select default_select,
        [ apply Model_intent.Current_selections Model_intent.Collapse_to_end ]
      )
  | None -> (Select selecting, [])

let handle_input state event context =
  match state with
  | Select selecting -> select_input selecting event context
  | Insert when is_named event Input_event.Escape -> (Select default_select, [])
  | Insert when is_named event Input_event.Backspace ->
      ( Insert,
        [
          apply Model_intent.Current_selections Model_intent.Collapse_to_end;
          apply Model_intent.Previous_text_unit Model_intent.Delete;
        ] )
  | Insert when is_named event Input_event.Enter ->
      (Insert, [ Model_effect.Execute_intent (Model_intent.insert_text "\n") ])
  | Insert -> (
      match Input_event.text event with
      | Some text ->
          ( Insert,
            [ Model_effect.Execute_intent (Model_intent.insert_text text) ] )
      | None -> (Insert, []))
  | Register_prefix { count } -> (
      match text_key event with
      | Some value
        when String.length value = 1 && value.[0] >= 'a' && value.[0] <= 'z' ->
          let slot = static (Clipboard.slot value) in
          (Select { count; slot }, [])
      | _ when is_named event Input_event.Escape -> (Select default_select, [])
      | _ -> (Register_prefix { count }, []))
  | Go_pending { count = _ } when is_text event "g" ->
      ( Select default_select,
        [ apply Model_intent.Document_start Model_intent.Select ] )
  | Go_pending _ when is_named event Input_event.Escape ->
      (Select default_select, [])
  | Go_pending pending -> (Go_pending pending, [])
