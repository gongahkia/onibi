open Zenbu_model_api

type dimensions = { columns : int; rows : int }
type rendered = { frame : Frame.t; viewport : Viewport.t }

let spaces width = if width <= 0 then "" else String.make width ' '

let selection_style selections primary_index (grapheme : Display.grapheme) =
  let rec loop index = function
    | [] -> Frame.Plain
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
          if index = primary_index then Frame.Primary_selection
          else Frame.Secondary_selection
        else loop (index + 1) rest
  in
  loop 0 selections.Editor_context.selections

let row_for_line ~columns ~left_column ~selections ~primary_index line =
  let right_column = left_column + columns in
  let rec loop used cells = function
    | [] ->
        let padding = columns - used in
        List.rev
          (if padding > 0 then
             Frame.cell ~width:padding (spaces padding) :: cells
           else cells)
    | grapheme :: rest ->
        let grapheme_end = grapheme.Display.column + grapheme.width in
        if grapheme_end <= left_column then loop used cells rest
        else if grapheme.column >= right_column then
          let padding = columns - used in
          List.rev
            (if padding > 0 then
               Frame.cell ~width:padding (spaces padding) :: cells
             else cells)
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
              ~style:(selection_style selections primary_index grapheme)
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
      row_for_line ~columns ~left_column:0
        ~selections:{ Editor_context.selections = []; primary_index = 0 }
        ~primary_index:0 line
    in
    String.concat "" (List.map (fun cell -> cell.Frame.text) cells)

let status_row ~columns ~status ~filename ~dirty ~line ~column ~selection_count
    ~message =
  let dirty_marker = if dirty then " [+]" else "" in
  let pending =
    match Model_status.pending_input status with
    | None -> ""
    | Some value -> " pending:" ^ value
  in
  let base =
    Printf.sprintf "%s  %s%s  %d:%d  %d selection%s%s"
      (Model_status.label status)
      filename dirty_marker (line + 1) (column + 1) selection_count
      (if selection_count = 1 then "" else "s")
      pending
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

let inspector_frame dimensions lines =
  let rows = max 0 dimensions.rows in
  let rec take remaining values =
    match (remaining, values) with
    | 0, _ | _, [] -> []
    | remaining, value :: rest -> value :: take (remaining - 1) rest
  in
  let lines = take rows lines in
  let row text =
    let text = clipped_text text dimensions.columns in
    [ Frame.cell ~style:Frame.Message ~width:(Display.text_width text) text ]
  in
  {
    frame =
      Frame.create ~width:dimensions.columns ~height:dimensions.rows
        ~rows:(List.map row lines) ~cursor:None;
    viewport = Viewport.origin;
  }

let render_with_inspector ~inspector ~context ~status ~filename ~dirty ~message
    ~viewport ~dimensions =
  if dimensions.rows < 2 || dimensions.columns < 1 then tiny_frame dimensions
  else
    match inspector with
    | Some lines -> inspector_frame dimensions lines
    | None ->
        let contents = Editor_context.contents context in
        let source_lines = Display.source_lines contents in
        let selections = Editor_context.selections context in
        let primary = List.nth selections.selections selections.primary_index in
        let primary_source_line =
          Display.source_line_at source_lines primary.head_offset
        in
        let primary_line = Display.layout contents primary_source_line in
        let primary_column =
          Display.column_at primary_line primary.head_offset
        in
        let viewport =
          Viewport.reconcile viewport ~line:primary_line.number
            ~column:primary_column ~width:dimensions.columns
            ~height:dimensions.rows
        in
        let content_rows = dimensions.rows - 1 in
        let first = viewport.top_line in
        let last = first + content_rows - 1 in
        let visible_rows =
          source_lines
          |> List.filter (fun source_line ->
              source_line.Display.number >= first && source_line.number <= last)
          |> List.map (fun source_line ->
              let line = Display.layout contents source_line in
              row_for_line ~columns:dimensions.columns
                ~left_column:viewport.left_column ~selections
                ~primary_index:selections.primary_index line)
        in
        let missing_rows = content_rows - List.length visible_rows in
        let blank_row =
          [ Frame.cell ~width:dimensions.columns (spaces dimensions.columns) ]
        in
        let rows =
          visible_rows
          @ List.init missing_rows (fun _ -> blank_row)
          @ [
              status_row ~columns:dimensions.columns ~status ~filename ~dirty
                ~line:primary_line.number ~column:primary_column
                ~selection_count:(List.length selections.selections)
                ~message;
            ]
        in
        let cursor =
          let row = primary_line.number - viewport.top_line in
          let column = primary_column - viewport.left_column in
          if
            row < 0 || row >= content_rows || column < 0
            || column >= dimensions.columns
          then None
          else Some { Frame.column; row }
        in
        {
          frame =
            Frame.create ~width:dimensions.columns ~height:dimensions.rows ~rows
              ~cursor;
          viewport;
        }

let render ~context ~status ~filename ~dirty ~message ~viewport ~dimensions =
  render_with_inspector ~inspector:None ~context ~status ~filename ~dirty
    ~message ~viewport ~dimensions
