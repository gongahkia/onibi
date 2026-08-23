type orientation = Horizontal | Vertical

type t =
  | Leaf of int
  | Split of { orientation : orientation; first : t; second : t }

type rectangle = { x : int; y : int; width : int; height : int }

type error =
  | Unknown_pane of int
  | Cannot_close_last_pane
  | Missing_frame of int
  | Frame_dimensions_mismatch of {
      pane : int;
      expected_width : int;
      expected_height : int;
      actual_width : int;
      actual_height : int;
    }

let single pane = Leaf pane

let rec panes = function
  | Leaf pane -> [ pane ]
  | Split { first; second; _ } -> panes first @ panes second

let rec split tree ~pane ~new_pane orientation =
  match tree with
  | Leaf value when value = pane ->
      Ok (Split { orientation; first = Leaf pane; second = Leaf new_pane })
  | Leaf _ -> Error (Unknown_pane pane)
  | Split { orientation = current; first; second } -> (
      match split first ~pane ~new_pane orientation with
      | Ok first -> Ok (Split { orientation = current; first; second })
      | Error (Unknown_pane _) ->
          split second ~pane ~new_pane orientation
          |> Result.map (fun second ->
              Split { orientation = current; first; second })
      | Error _ as error -> error)

let rec close tree ~pane =
  match tree with
  | Leaf value when value = pane -> Error Cannot_close_last_pane
  | Leaf _ -> Error (Unknown_pane pane)
  | Split { first = Leaf value; second; _ } when value = pane -> Ok second
  | Split { first; second = Leaf value; _ } when value = pane -> Ok first
  | Split { orientation; first; second } -> (
      match close first ~pane with
      | Ok first -> Ok (Split { orientation; first; second })
      | Error (Unknown_pane _) ->
          close second ~pane
          |> Result.map (fun second -> Split { orientation; first; second })
      | Error Cannot_close_last_pane ->
          if List.length (panes tree) = 1 then Error Cannot_close_last_pane
          else Error (Unknown_pane pane)
      | Error _ as error -> error)

let clamp value = max 0 value

let rec bounds_in tree rectangle =
  match tree with
  | Leaf pane -> [ (pane, rectangle) ]
  | Split { orientation = Vertical; first; second } ->
      let available = clamp (rectangle.width - 1) in
      let first_width = available / 2 in
      let second_width = available - first_width in
      bounds_in first { rectangle with width = first_width }
      @ bounds_in second
          {
            x = rectangle.x + first_width + 1;
            y = rectangle.y;
            width = second_width;
            height = rectangle.height;
          }
  | Split { orientation = Horizontal; first; second } ->
      let available = clamp (rectangle.height - 1) in
      let first_height = available / 2 in
      let second_height = available - first_height in
      bounds_in first { rectangle with height = first_height }
      @ bounds_in second
          {
            x = rectangle.x;
            y = rectangle.y + first_height + 1;
            width = rectangle.width;
            height = second_height;
          }

let bounds tree ~width ~height =
  bounds_in tree { x = 0; y = 0; width = clamp width; height = clamp height }

let frame_for frames pane =
  match List.assoc_opt pane frames with
  | Some frame -> Ok frame
  | None -> Error (Missing_frame pane)

let validate_frame ~pane rectangle frame =
  let actual_width = Frame.width frame in
  let actual_height = Frame.height frame in
  if actual_width = rectangle.width && actual_height = rectangle.height then
    Ok ()
  else
    Error
      (Frame_dimensions_mismatch
         {
           pane;
           expected_width = rectangle.width;
           expected_height = rectangle.height;
           actual_width;
           actual_height;
         })

let separator_cell width text = Frame.cell ~style:Frame.Dim ~width text
let vertical_separator = separator_cell 1 "│"

let horizontal_separator width =
  let rec repeat values remaining =
    if remaining <= 0 then values else repeat ("─" :: values) (remaining - 1)
  in
  [ separator_cell width (repeat [] width |> List.rev |> String.concat "") ]

let rows frame =
  let rows = Frame.rows frame in
  let missing = max 0 (Frame.height frame - List.length rows) in
  rows
  @ List.init missing (fun _ -> [ Frame.cell ~width:(Frame.width frame) "" ])

let cursor_for ~focused_pane pane frame ~x ~y =
  if pane <> focused_pane then None
  else
    Option.map
      (fun (cursor : Frame.cursor) ->
        { Frame.column = x + cursor.column; row = y + cursor.row })
      (Frame.cursor frame)

let rec compose_in tree rectangle ~focused_pane frames =
  match tree with
  | Leaf pane ->
      Result.bind (frame_for frames pane) (fun frame ->
          validate_frame ~pane rectangle frame
          |> Result.map (fun () ->
              (frame, cursor_for ~focused_pane pane frame ~x:0 ~y:0)))
  | Split { orientation = Vertical; first; second } ->
      let available = clamp (rectangle.width - 1) in
      let first_width = available / 2 in
      let second_width = available - first_width in
      let first_rectangle = { rectangle with width = first_width } in
      let second_rectangle =
        {
          rectangle with
          x = rectangle.x + first_width + 1;
          width = second_width;
        }
      in
      Result.bind (compose_in first first_rectangle ~focused_pane frames)
        (fun (first, first_cursor) ->
          compose_in second second_rectangle ~focused_pane frames
          |> Result.map (fun (second, second_cursor) ->
              let rows =
                List.map2
                  (fun first second -> first @ [ vertical_separator ] @ second)
                  (rows first) (rows second)
              in
              let cursor =
                match (first_cursor, second_cursor) with
                | Some cursor, None -> Some cursor
                | None, Some cursor ->
                    Some
                      {
                        Frame.column = cursor.Frame.column + first_width + 1;
                        row = cursor.Frame.row;
                      }
                | None, None -> None
                | Some _, Some _ -> assert false
              in
              ( Frame.create ~width:rectangle.width ~height:rectangle.height
                  ~rows ~cursor,
                cursor )))
  | Split { orientation = Horizontal; first; second } ->
      let available = clamp (rectangle.height - 1) in
      let first_height = available / 2 in
      let second_height = available - first_height in
      let first_rectangle = { rectangle with height = first_height } in
      let second_rectangle =
        {
          rectangle with
          y = rectangle.y + first_height + 1;
          height = second_height;
        }
      in
      Result.bind (compose_in first first_rectangle ~focused_pane frames)
        (fun (first, first_cursor) ->
          compose_in second second_rectangle ~focused_pane frames
          |> Result.map (fun (second, second_cursor) ->
              let rows =
                rows first
                @ [ horizontal_separator rectangle.width ]
                @ rows second
              in
              let cursor =
                match (first_cursor, second_cursor) with
                | Some cursor, None -> Some cursor
                | None, Some cursor ->
                    Some
                      {
                        Frame.column = cursor.Frame.column;
                        row = cursor.Frame.row + first_height + 1;
                      }
                | None, None -> None
                | Some _, Some _ -> assert false
              in
              ( Frame.create ~width:rectangle.width ~height:rectangle.height
                  ~rows ~cursor,
                cursor )))

let compose tree ~width ~height ~focused_pane ~frames =
  let rectangle =
    { x = 0; y = 0; width = clamp width; height = clamp height }
  in
  compose_in tree rectangle ~focused_pane frames |> Result.map fst

let error_to_string = function
  | Unknown_pane pane -> "unknown pane " ^ string_of_int pane
  | Cannot_close_last_pane -> "cannot close the last pane"
  | Missing_frame pane ->
      "missing rendered frame for pane " ^ string_of_int pane
  | Frame_dimensions_mismatch
      { pane; expected_width; expected_height; actual_width; actual_height } ->
      Printf.sprintf "pane %d frame dimensions are %dx%d, expected %dx%d" pane
        actual_width actual_height expected_width expected_height
