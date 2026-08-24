open Zenbu_model_api

type dimensions = { columns : int; rows : int }
type rendered = { frame : Frame.t; viewport : Viewport.t }
type syntax_class = Keyword | String | Number | Comment | Type | Constructor

type syntax_span = {
  start_offset : int;
  stop_offset : int;
  class_ : syntax_class;
}

type search_range = { start_offset : int; stop_offset : int }
type diagnostic_kind = Error | Warning | Information | Hint

type diagnostic_range = {
  start_offset : int;
  stop_offset : int;
  kind : diagnostic_kind;
}

type trailing = { text : string; style : Frame.style }

let spaces width = if width <= 0 then "" else String.make width ' '
let decimal_width value = String.length (string_of_int (max 1 value))

let selection_style selections primary_index (grapheme : Display.grapheme) =
  let rec loop index = function
    | [] -> None
    | selection :: rest ->
        let start_offset =
          min selection.Editor_context.anchor_offset selection.head_offset
        in
        let stop_offset =
          max selection.Editor_context.anchor_offset selection.head_offset
        in
        if
          start_offset < grapheme.Display.stop_offset
          && grapheme.start_offset < stop_offset
        then
          if index = primary_index then Some Frame.Primary_selection
          else Some Frame.Secondary_selection
        else loop (index + 1) rest
  in
  loop 0 selections.Editor_context.selections

let overlaps ~start_offset ~stop_offset (grapheme : Display.grapheme) =
  start_offset < grapheme.Display.stop_offset
  && grapheme.start_offset < stop_offset

let syntax_style spans grapheme =
  spans
  |> List.find_map (fun (span : syntax_span) ->
      if
        overlaps ~start_offset:span.start_offset ~stop_offset:span.stop_offset
          grapheme
      then
        Some
          (match span.class_ with
          | Keyword -> Frame.Syntax_keyword
          | String -> Frame.Syntax_string
          | Number -> Frame.Syntax_number
          | Comment -> Frame.Syntax_comment
          | Type -> Frame.Syntax_type
          | Constructor -> Frame.Syntax_constructor)
      else None)

let search_style ranges grapheme =
  if
    List.exists
      (fun (range : search_range) ->
        overlaps ~start_offset:range.start_offset ~stop_offset:range.stop_offset
          grapheme)
      ranges
  then Some Frame.Search_match
  else None

let diagnostic_style ranges grapheme =
  ranges
  |> List.find_map (fun (range : diagnostic_range) ->
      if
        overlaps ~start_offset:range.start_offset ~stop_offset:range.stop_offset
          grapheme
      then
        Some
          (match range.kind with
          | Error -> Frame.Diagnostic_error
          | Warning -> Frame.Diagnostic_warning
          | Information -> Frame.Diagnostic_information
          | Hint -> Frame.Diagnostic_hint)
      else None)

let grapheme_style ~syntax_spans ~search_ranges ~diagnostic_ranges ~selections
    ~primary_index grapheme =
  match selection_style selections primary_index grapheme with
  | Some style -> style
  | None -> (
      match search_style search_ranges grapheme with
      | Some style -> style
      | None -> (
          match diagnostic_style diagnostic_ranges grapheme with
          | Some style -> style
          | None ->
              Option.value ~default:Frame.Plain
                (syntax_style syntax_spans grapheme)))

let row_for_line ?(trailing = []) ?(content_style = None) ~columns ~left_column
    ~syntax_spans ~search_ranges ~diagnostic_ranges ~selections ~primary_index
    line =
  let right_column = left_column + columns in
  let padding used cells =
    let padding = columns - used in
    List.rev
      (if padding > 0 then Frame.cell ~width:padding (spaces padding) :: cells
       else cells)
  in
  let trailing_graphemes =
    trailing
    |> List.concat_map (fun trailing ->
        let graphemes =
          Display.layout trailing.text
            {
              Display.number = 0;
              start_offset = 0;
              stop_offset = String.length trailing.text;
              end_offset = String.length trailing.text;
            }
          |> fun marker -> marker.Display.graphemes
        in
        List.map (fun grapheme -> (trailing.style, grapheme)) graphemes)
  in
  let rec marker_loop used cells = function
    | [] -> padding used cells
    | (style, grapheme) :: rest ->
        let column = line.Display.width + grapheme.Display.column in
        let grapheme_end = column + grapheme.width in
        if grapheme_end <= left_column then marker_loop used cells rest
        else if column >= right_column then padding used cells
        else
          let visible_start = max left_column column in
          let visible_end = min right_column grapheme_end in
          let width = visible_end - visible_start in
          let fully_visible =
            visible_start = column && visible_end = grapheme_end
          in
          let text = if fully_visible then grapheme.text else spaces width in
          let cell = Frame.cell ~style ~width text in
          marker_loop (used + width) (cell :: cells) rest
  in
  let rec loop used cells = function
    | [] -> marker_loop used cells trailing_graphemes
    | grapheme :: rest ->
        let grapheme_end = grapheme.Display.column + grapheme.width in
        if grapheme_end <= left_column then loop used cells rest
        else if grapheme.column >= right_column then padding used cells
        else
          let visible_start = max left_column grapheme.column in
          let visible_end = min right_column grapheme_end in
          let width = visible_end - visible_start in
          let fully_visible =
            visible_start = grapheme.column && visible_end = grapheme_end
          in
          let text = if fully_visible then grapheme.text else spaces width in
          let cell =
            Frame.cell
              ~style:
                (Option.value
                   ~default:
                     (grapheme_style ~syntax_spans ~search_ranges
                        ~diagnostic_ranges ~selections ~primary_index grapheme)
                   content_style)
              ~width text
          in
          loop (used + width) (cell :: cells) rest
  in
  loop 0 [] line.Display.graphemes

let clipped_text text columns =
  if columns <= 0 then ""
  else
    let line =
      Display.layout text
        {
          Display.number = 0;
          start_offset = 0;
          stop_offset = String.length text;
          end_offset = String.length text;
        }
    in
    let cells =
      row_for_line ~columns ~left_column:0 ~syntax_spans:[] ~search_ranges:[]
        ~diagnostic_ranges:[]
        ~selections:{ Editor_context.selections = []; primary_index = 0 }
        ~primary_index:0 line
    in
    String.concat "" (List.map (fun cell -> cell.Frame.text) cells)

let status_row ~columns ~presentation ~status ~filename ~dirty ~line ~column
    ~selection_count ~diagnostic_summary ~message =
  let dirty_marker = if dirty then " [+]" else "" in
  let pending =
    match Model_status.pending_input status with
    | None -> ""
    | Some value -> " pending:" ^ value
  in
  let base =
    match Presentation.status_line presentation with
    | Presentation.Detailed ->
        Printf.sprintf "%s  %s%s  %d:%d  %d selection%s%s%s"
          (Model_status.label status)
          filename dirty_marker (line + 1) (column + 1) selection_count
          (if selection_count = 1 then "" else "s")
          pending
          (Option.map (fun summary -> "  " ^ summary) diagnostic_summary
          |> Option.value ~default:"")
    | Presentation.Minimal ->
        Printf.sprintf "%s  %s%s  %d:%d"
          (Model_status.label status)
          filename dirty_marker (line + 1) (column + 1)
    | Presentation.Hidden_status -> ""
  in
  let message = Option.value ~default:"" message in
  let text =
    if String.length message = 0 then base else base ^ " — " ^ message
  in
  let style =
    if String.length message = 0 then Frame.Status else Frame.Message
  in
  [
    Frame.cell ~style
      ~width:(Display.text_width (clipped_text text columns))
      (clipped_text text columns);
  ]

let tiny_frame dimensions =
  if dimensions.rows <= 0 || dimensions.columns <= 0 then
    {
      frame =
        Frame.create ~width:dimensions.columns ~height:dimensions.rows ~rows:[]
          ~cursor:None;
      viewport = Viewport.origin;
    }
  else
    let text = clipped_text "Zenbu: terminal too small" dimensions.columns in
    {
      frame =
        Frame.create ~width:dimensions.columns ~height:dimensions.rows
          ~rows:
            [
              [
                Frame.cell ~style:Frame.Message ~width:(Display.text_width text)
                  text;
              ];
            ]
          ~cursor:None;
      viewport = Viewport.origin;
    }

let text_frame ?(style = Frame.Message) dimensions lines =
  let rows = max 0 dimensions.rows in
  let rec take remaining values =
    match (remaining, values) with
    | 0, _ | _, [] -> []
    | remaining, value :: rest -> value :: take (remaining - 1) rest
  in
  let lines = take rows lines in
  let row text =
    let text = clipped_text text dimensions.columns in
    [ Frame.cell ~style ~width:(Display.text_width text) text ]
  in
  {
    frame =
      Frame.create ~width:dimensions.columns ~height:dimensions.rows
        ~rows:(List.map row lines) ~cursor:None;
    viewport = Viewport.origin;
  }

let has_status_line presentation =
  Presentation.status_line presentation <> Presentation.Hidden_status

let gutter_width presentation source_lines columns =
  match Presentation.line_numbers presentation with
  | Presentation.Hidden -> 0
  | Presentation.Absolute | Presentation.Relative ->
      min columns (decimal_width (List.length source_lines) + 1)

let gutter_text ~width ~number_width number =
  if width <= 0 then ""
  else
    let text = Printf.sprintf "%*d " number_width number in
    if String.length text <= width then
      spaces (width - String.length text) ^ text
    else clipped_text text width

let gutter_row ~presentation ~width ~number_width ~primary_line source_line =
  match Presentation.line_numbers presentation with
  | Presentation.Hidden -> []
  | Presentation.Absolute ->
      [
        Frame.cell ~style:Frame.Dim ~width
          (gutter_text ~width ~number_width (source_line.Display.number + 1));
      ]
  | Presentation.Relative ->
      [
        Frame.cell ~style:Frame.Dim ~width
          (gutter_text ~width ~number_width
             (abs (source_line.Display.number - primary_line)));
      ]

let blank_gutter width =
  if width = 0 then []
  else [ Frame.cell ~style:Frame.Dim ~width (spaces width) ]

let decoration_text decoration =
  let provider = Decoration.provider_id decoration in
  match Decoration.item decoration with
  | Decoration.Inline { text; _ } -> "  [" ^ provider ^ ": " ^ text ^ "]"
  | Decoration.Virtual_line { text; _ } -> "[" ^ provider ^ "] " ^ text

let render_with_inspector ~inspector ?(presentation = Presentation.default)
    ?overlay ?source_lines ?(syntax_spans = []) ?(search_ranges = [])
    ?(diagnostic_ranges = []) ?(fold_ranges = []) ?(decorations = [])
    ?diagnostic_summary ~context ~status ~filename ~dirty ~message ~viewport
    ~dimensions () =
  if
    dimensions.columns < 1
    || dimensions.rows < if has_status_line presentation then 2 else 1
  then tiny_frame dimensions
  else
    match inspector with
    | Some lines -> text_frame dimensions lines
    | None -> (
        match overlay with
        | Some lines -> text_frame ~style:Frame.Overlay dimensions lines
        | None ->
            let contents = Editor_context.contents context in
            let source_lines =
              match source_lines with
              | Some source_lines -> source_lines
              | None -> Display.source_lines contents
            in
            let selections = Editor_context.selections context in
            let primary =
              List.nth selections.selections selections.primary_index
            in
            let primary_source_line =
              Display.source_line_at source_lines primary.head_offset
            in
            let primary_line = Display.layout contents primary_source_line in
            let primary_column =
              Display.column_at primary_line primary.head_offset
            in
            let projected_lines, _ =
              Projection.project ~contents
                ~document_id:(Editor_context.document_id context)
                ~document_version:(Editor_context.document_version context)
                ~folds:fold_ranges ~decorations source_lines
            in
            let primary_projected_index =
              Projection.index_for_offset projected_lines source_lines
                primary.head_offset
            in
            let primary_projected_line =
              List.nth projected_lines primary_projected_index
            in
            let cursor_source_line =
              Projection.row_anchor_line primary_projected_line
            in
            let cursor_line = Display.layout contents cursor_source_line in
            let cursor_column =
              if cursor_source_line.number = primary_source_line.number then
                primary_column
              else cursor_line.width
            in
            let gutter_columns =
              gutter_width presentation source_lines dimensions.columns
            in
            let content_columns = dimensions.columns - gutter_columns in
            let content_rows =
              dimensions.rows - if has_status_line presentation then 1 else 0
            in
            let maximum_top_line =
              max 0 (List.length projected_lines - content_rows)
            in
            let viewport =
              Viewport.
                {
                  viewport with
                  top_line = min maximum_top_line (max 0 viewport.top_line);
                }
            in
            let viewport =
              Viewport.reconcile viewport ~line:primary_projected_index
                ~column:cursor_column ~width:content_columns
                ~height:(content_rows + 1)
            in
            let first = viewport.top_line in
            let last = first + content_rows - 1 in
            let visible_projected_lines =
              projected_lines
              |> List.mapi (fun index line -> (index, line))
              |> List.filter (fun (index, _) -> index >= first && index <= last)
            in
            let visible_start, visible_stop =
              match visible_projected_lines with
              | [] -> (0, 0)
              | (_, first_line) :: rest ->
                  let last_line =
                    List.fold_left (fun _ (_, line) -> line) first_line rest
                  in
                  Projection.row_anchor_line first_line |> fun line ->
                  ( line.start_offset,
                    Projection.row_anchor_line last_line |> fun line ->
                    line.end_offset )
            in
            let intersects start_offset stop_offset =
              start_offset < visible_stop && visible_start < stop_offset
            in
            let visible_syntax_spans =
              List.filter
                (fun (span : syntax_span) ->
                  intersects span.start_offset span.stop_offset)
                syntax_spans
            in
            let visible_search_ranges =
              List.filter
                (fun (range : search_range) ->
                  intersects range.start_offset range.stop_offset)
                search_ranges
            in
            let visible_diagnostic_ranges =
              List.filter
                (fun (range : diagnostic_range) ->
                  intersects range.start_offset range.stop_offset)
                diagnostic_ranges
            in
            let number_width = decimal_width (List.length source_lines) in
            let visible_rows =
              visible_projected_lines
              |> List.map (fun (_, projected_line) ->
                  match projected_line with
                  | Projection.Source source ->
                      let source_line = Projection.source_line source in
                      let line = Display.layout contents source_line in
                      let fold_marker =
                        match Projection.fold source with
                        | None -> []
                        | Some fold ->
                            let hidden =
                              Fold.stop_line fold - Fold.start_line fold
                            in
                            [
                              {
                                text =
                                  Printf.sprintf "  … %d line%s folded" hidden
                                    (if hidden = 1 then "" else "s");
                                style = Frame.Dim;
                              };
                            ]
                      in
                      let inline =
                        Projection.inline source
                        |> List.map (fun decoration ->
                            {
                              text = decoration_text decoration;
                              style = Frame.Decoration_inline;
                            })
                      in
                      gutter_row ~presentation ~width:gutter_columns
                        ~number_width ~primary_line:primary_line.number
                        source_line
                      @ row_for_line ~trailing:(fold_marker @ inline)
                          ~columns:content_columns
                          ~left_column:viewport.left_column
                          ~syntax_spans:visible_syntax_spans
                          ~search_ranges:visible_search_ranges
                          ~diagnostic_ranges:visible_diagnostic_ranges
                          ~selections ~primary_index:selections.primary_index
                          line
                  | Projection.Virtual virtual_row ->
                      let text =
                        decoration_text
                          (Projection.virtual_decoration virtual_row)
                      in
                      let line =
                        Display.layout text
                          {
                            Display.number = 0;
                            start_offset = 0;
                            stop_offset = String.length text;
                            end_offset = String.length text;
                          }
                      in
                      blank_gutter gutter_columns
                      @ row_for_line
                          ~content_style:(Some Frame.Decoration_virtual)
                          ~columns:content_columns
                          ~left_column:viewport.left_column ~syntax_spans:[]
                          ~search_ranges:[] ~diagnostic_ranges:[]
                          ~selections:
                            {
                              Editor_context.selections = [];
                              primary_index = 0;
                            }
                          ~primary_index:0 line)
            in
            let missing_rows = content_rows - List.length visible_rows in
            let blank_row =
              blank_gutter gutter_columns
              @ [ Frame.cell ~width:content_columns (spaces content_columns) ]
            in
            let rows =
              visible_rows
              @ List.init missing_rows (fun _ -> blank_row)
              @
              if has_status_line presentation then
                [
                  status_row ~columns:dimensions.columns ~presentation ~status
                    ~filename ~dirty ~line:primary_line.number
                    ~column:primary_column
                    ~selection_count:(List.length selections.selections)
                    ~diagnostic_summary ~message;
                ]
              else []
            in
            let cursor =
              let row = primary_projected_index - viewport.top_line in
              let column =
                gutter_columns + cursor_column - viewport.left_column
              in
              if
                row < 0 || row >= content_rows || column < 0
                || column >= dimensions.columns
              then None
              else Some { Frame.column; row }
            in
            {
              frame =
                Frame.create ~width:dimensions.columns ~height:dimensions.rows
                  ~rows ~cursor;
              viewport;
            })

let render_with_presentation ~presentation ~context ~status ~filename ~dirty
    ~message ~viewport ~dimensions =
  render_with_inspector ~inspector:None ~presentation ~syntax_spans:[]
    ~search_ranges:[] ~diagnostic_ranges:[] ~context ~status ~filename ~dirty
    ~message ~viewport ~dimensions ()

let render ~context ~status ~filename ~dirty ~message ~viewport ~dimensions =
  render_with_presentation ~presentation:Presentation.default ~context ~status
    ~filename ~dirty ~message ~viewport ~dimensions
