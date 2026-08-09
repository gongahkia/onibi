type selection_state = { selections : Selection_spec.t list; primary : int }
type edit_spec = { start_offset : int; stop_offset : int; text : string }

type transaction_spec = {
  source : Transaction.source;
  intent : string option;
  description : string option;
  edits : edit_spec list;
  selection_change : selection_state option;
}

type action = Intent of Intent.t | Transaction of transaction_spec

type t = {
  document_id : string;
  contents : string;
  initial_selections : selection_state;
  actions : action list;
}

let ( let* ) result f = Result.bind result f

let create ~document_id ~contents ~initial_selections ~actions =
  if initial_selections.selections = [] then
    Error
      (Error.Invalid_selection_set
         "a replay must declare at least one initial selection")
  else
    let* id = Document_id.of_string document_id in
    let* _ = Text_buffer.of_utf8 contents in
    let* _ =
      Document.create ~id ~contents
        ~initial_selections:initial_selections.selections
        ~primary:initial_selections.primary ()
    in
    Ok { document_id; contents; initial_selections; actions }

let document_id value = value.document_id
let contents value = value.contents
let initial_selections value = value.initial_selections
let actions value = value.actions

let collect results =
  let rec loop values = function
    | [] -> Ok (List.rev values)
    | Ok value :: rest -> loop (value :: values) rest
    | Error error :: _ -> Error error
  in
  loop [] results

let selection_set_of_state snapshot state =
  let selections =
    List.map
      (fun spec ->
        let* anchor =
          Document_snapshot.anchor snapshot
            ~byte_offset:(Selection_spec.anchor_offset spec)
        in
        let* head =
          Document_snapshot.anchor snapshot
            ~byte_offset:(Selection_spec.head_offset spec)
        in
        Selection.make ~anchor ~head)
      state.selections
  in
  let* selections = collect selections in
  Selection_set.create ~primary:state.primary selections

let transaction_of_spec snapshot spec =
  let edits =
    List.map
      (fun edit ->
        let* range =
          Document_snapshot.range snapshot ~start_offset:edit.start_offset
            ~stop_offset:edit.stop_offset
        in
        Edit.replace range ~text:edit.text)
      spec.edits
  in
  let* edits = collect edits in
  let* selection_change =
    match spec.selection_change with
    | None -> Ok None
    | Some state ->
        let* selections = selection_set_of_state snapshot state in
        Ok (Some selections)
  in
  let metadata =
    Transaction.metadata ~source:spec.source ?intent:spec.intent
      ?description:spec.description ()
  in
  Transaction.create
    ~document_id:(Document_snapshot.document_id snapshot)
    ~source_version:(Document_snapshot.version snapshot)
    ~edits ?selection_change ~metadata ()

let run_action history = function
  | Intent intent ->
      History.apply_intent ~source:Transaction.Replay history intent
  | Transaction spec -> (
      match
        transaction_of_spec (Document.snapshot (History.current history)) spec
      with
      | Error _ as error -> error
      | Ok transaction -> History.commit history transaction)

let run replay =
  let* id = Document_id.of_string replay.document_id in
  let* document =
    Document.create ~id ~contents:replay.contents
      ~initial_selections:replay.initial_selections.selections
      ~primary:replay.initial_selections.primary ()
  in
  let rec apply history index = function
    | [] -> Ok history
    | action :: rest -> (
        match run_action history action with
        | Ok next -> apply next (index + 1) rest
        | Error cause -> Error (Error.Replay_diverged { step = index; cause }))
  in
  apply (History.create document) 0 replay.actions

let escape = String.escaped

let unescape value =
  try Ok (Scanf.unescaped value)
  with Failure _ | Invalid_argument _ ->
    Error (Error.Malformed_replay "invalid escaped string")

let option_to_string = function None -> "0" | Some value -> "1" ^ escape value

let option_of_string value =
  if String.equal value "0" then Ok None
  else if String.length value >= 1 && value.[0] = '1' then
    let* unescaped = unescape (String.sub value 1 (String.length value - 1)) in
    Ok (Some unescaped)
  else Error (Error.Malformed_replay "invalid optional string field")

let selection_specs_to_string selections =
  String.concat ","
    (List.map
       (fun spec ->
         Printf.sprintf "%d:%d"
           (Selection_spec.anchor_offset spec)
           (Selection_spec.head_offset spec))
       selections)

let selection_state_to_string state =
  Printf.sprintf "%d\t%s" state.primary
    (selection_specs_to_string state.selections)

let int_of_replay value =
  try Ok (int_of_string value)
  with Failure _ ->
    Error (Error.Malformed_replay ("expected integer, got " ^ value))

let split_exactly expected parts context =
  if List.length parts = expected then Ok parts
  else Error (Error.Malformed_replay context)

let parse_selection_specs value =
  if String.length value = 0 then
    Error (Error.Malformed_replay "selection list is empty")
  else
    let parse_one item =
      let* parts =
        split_exactly 2
          (String.split_on_char ':' item)
          "invalid selection position"
      in
      match parts with
      | [ anchor; head ] ->
          let* anchor_offset = int_of_replay anchor in
          let* head_offset = int_of_replay head in
          Selection_spec.make ~anchor_offset ~head_offset
      | _ -> Error (Error.Malformed_replay "invalid selection position")
    in
    collect (List.map parse_one (String.split_on_char ',' value))

let parse_selection_state value =
  let* parts =
    split_exactly 2 (String.split_on_char '\t' value) "invalid selection state"
  in
  match parts with
  | [ primary; selections ] ->
      let* primary = int_of_replay primary in
      let* selections = parse_selection_specs selections in
      Ok { selections; primary }
  | _ -> Error (Error.Malformed_replay "invalid selection state")

let after_prefix prefix line =
  let prefix_length = String.length prefix in
  if
    String.length line >= prefix_length
    && String.sub line 0 prefix_length = prefix
  then Some (String.sub line prefix_length (String.length line - prefix_length))
  else None

let require_field field = function
  | line :: rest -> (
      match after_prefix (field ^ "=") line with
      | Some value -> Ok (value, rest)
      | None -> Error (Error.Malformed_replay ("expected " ^ field ^ " field")))
  | [] -> Error (Error.Malformed_replay ("missing " ^ field ^ " field"))

let parse_edit value =
  let* parts =
    split_exactly 2 (String.split_on_char '\t' value) "invalid edit"
  in
  match parts with
  | [ range; text ] -> (
      let* positions =
        split_exactly 2 (String.split_on_char ':' range) "invalid edit range"
      in
      match positions with
      | [ start_offset; stop_offset ] ->
          let* start_offset = int_of_replay start_offset in
          let* stop_offset = int_of_replay stop_offset in
          let* text = unescape text in
          Ok { start_offset; stop_offset; text }
      | _ -> Error (Error.Malformed_replay "invalid edit range"))
  | _ -> Error (Error.Malformed_replay "invalid edit")

let parse_transaction lines =
  let* source_text, lines = require_field "source" lines in
  let* source = Transaction.source_of_string source_text in
  let* intent_text, lines = require_field "intent" lines in
  let* intent = option_of_string intent_text in
  let* description_text, lines = require_field "description" lines in
  let* description = option_of_string description_text in
  let* selection_text, lines = require_field "selection-change" lines in
  let* selection_change =
    if String.equal selection_text "-" then Ok None
    else
      let* state = parse_selection_state selection_text in
      Ok (Some state)
  in
  let rec edits values = function
    | [] -> Error (Error.Malformed_replay "unterminated transaction")
    | "end-transaction" :: rest ->
        Ok
          ( Transaction
              {
                source;
                intent;
                description;
                edits = List.rev values;
                selection_change;
              },
            rest )
    | line :: rest -> (
        match after_prefix "edit=" line with
        | None ->
            Error (Error.Malformed_replay "expected edit or end-transaction")
        | Some value ->
            let* edit = parse_edit value in
            edits (edit :: values) rest)
  in
  edits [] lines

let parse_actions lines =
  let rec loop values = function
    | [] -> Ok (List.rev values)
    | "" :: rest -> loop values rest
    | "action=intent-delete" :: rest ->
        loop (Intent Intent.Delete_selected_ranges :: values) rest
    | "action=transaction" :: rest ->
        let* action, remaining = parse_transaction rest in
        loop (action :: values) remaining
    | line :: rest -> (
        match after_prefix "action=intent-apply\t" line with
        | Some value -> (
            let* parts =
              if List.length (String.split_on_char '\t' value) >= 2 then
                Ok (String.split_on_char '\t' value)
              else Error (Error.Malformed_replay "invalid apply intent")
            in
            match parts with
            | [ selector; "select" ] ->
                let* selector = Selector.of_string selector in
                loop
                  (Intent
                     (Intent.Apply
                        { selector; transformation = Transformation.Select })
                  :: values)
                  rest
            | [ selector; "delete" ] ->
                let* selector = Selector.of_string selector in
                loop
                  (Intent
                     (Intent.Apply
                        { selector; transformation = Transformation.Delete })
                  :: values)
                  rest
            | [ selector; "replace"; text ] ->
                let* selector = Selector.of_string selector in
                let* text = unescape text in
                loop
                  (Intent
                     (Intent.Apply
                        {
                          selector;
                          transformation = Transformation.Replace_text text;
                        })
                  :: values)
                  rest
            | _ -> Error (Error.Malformed_replay "invalid apply intent"))
        | None -> (
            match after_prefix "action=intent-insert\t" line with
            | Some text ->
                let* text = unescape text in
                loop (Intent (Intent.Insert_text text) :: values) rest
            | None -> (
                match after_prefix "action=intent-replace\t" line with
                | Some text ->
                    let* text = unescape text in
                    loop
                      (Intent (Intent.Replace_selected_ranges text) :: values)
                      rest
                | None -> (
                    match after_prefix "action=intent-set\t" line with
                    | Some state ->
                        let* { selections; primary } =
                          parse_selection_state state
                        in
                        loop
                          (Intent
                             (Intent.Set_selections { selections; primary })
                          :: values)
                          rest
                    | None ->
                        Error
                          (Error.Malformed_replay ("unknown action " ^ line)))))
        )
  in
  loop [] lines

let of_string text =
  match String.split_on_char '\n' text with
  | "zenbu-replay-v1" :: document :: contents :: selections :: primary
    :: actions ->
      let parse_header field line =
        match after_prefix (field ^ "=") line with
        | Some value -> Ok value
        | None ->
            Error (Error.Malformed_replay ("expected " ^ field ^ " header"))
      in
      let* document_id = parse_header "document" document in
      let* document_id = unescape document_id in
      let* contents = parse_header "text" contents in
      let* contents = unescape contents in
      let* selections = parse_header "selections" selections in
      let* selections = parse_selection_specs selections in
      let* primary = parse_header "primary" primary in
      let* primary = int_of_replay primary in
      let* actions = parse_actions actions in
      create ~document_id ~contents ~initial_selections:{ selections; primary }
        ~actions
  | _ -> Error (Error.Malformed_replay "missing zenbu-replay-v1 header")

let add_line buffer line =
  Buffer.add_string buffer line;
  Buffer.add_char buffer '\n'

let add_action buffer = function
  | Intent (Intent.Insert_text text) ->
      add_line buffer ("action=intent-insert\t" ^ escape text)
  | Intent Intent.Delete_selected_ranges ->
      add_line buffer "action=intent-delete"
  | Intent (Intent.Replace_selected_ranges text) ->
      add_line buffer ("action=intent-replace\t" ^ escape text)
  | Intent (Intent.Set_selections { selections; primary }) ->
      add_line buffer
        ("action=intent-set\t"
        ^ selection_state_to_string { selections; primary })
  | Intent (Intent.Apply { selector; transformation }) ->
      let body =
        match transformation with
        | Transformation.Select -> Selector.to_string selector ^ "\tselect"
        | Transformation.Delete -> Selector.to_string selector ^ "\tdelete"
        | Transformation.Replace_text text ->
            Selector.to_string selector ^ "\treplace\t" ^ escape text
      in
      add_line buffer ("action=intent-apply\t" ^ body)
  | Transaction spec ->
      add_line buffer "action=transaction";
      add_line buffer ("source=" ^ Transaction.source_to_string spec.source);
      add_line buffer ("intent=" ^ option_to_string spec.intent);
      add_line buffer ("description=" ^ option_to_string spec.description);
      add_line buffer
        ("selection-change="
        ^
        match spec.selection_change with
        | None -> "-"
        | Some state -> selection_state_to_string state);
      List.iter
        (fun edit ->
          add_line buffer
            (Printf.sprintf "edit=%d:%d\t%s" edit.start_offset edit.stop_offset
               (escape edit.text)))
        spec.edits;
      add_line buffer "end-transaction"

let to_string replay =
  let buffer = Buffer.create 256 in
  add_line buffer "zenbu-replay-v1";
  add_line buffer ("document=" ^ escape replay.document_id);
  add_line buffer ("text=" ^ escape replay.contents);
  add_line buffer
    ("selections="
    ^ selection_specs_to_string replay.initial_selections.selections);
  add_line buffer ("primary=" ^ string_of_int replay.initial_selections.primary);
  List.iter (add_action buffer) replay.actions;
  Buffer.contents buffer
