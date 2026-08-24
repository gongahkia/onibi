type orientation = Horizontal | Vertical
type dimension = Width | Height

type t =
  | Leaf of int
  | Split of { orientation : orientation; ratio : int; first : t; second : t }

type rectangle = { x : int; y : int; width : int; height : int }
type branch = First | Second
type divider = { path : branch list; orientation : orientation }

type error =
  | Unknown_pane of int
  | Cannot_close_last_pane
  | Cannot_resize_pane of int
  | Missing_frame of int
  | Frame_dimensions_mismatch of {
      pane : int;
      expected_width : int;
      expected_height : int;
      actual_width : int;
      actual_height : int;
    }

let single pane = Leaf pane
let equal_ratio = 500
let ratio_scale = 1000

let rec panes = function
  | Leaf pane -> [ pane ]
  | Split { first; second; _ } -> panes first @ panes second

let rec split tree ~pane ~new_pane orientation =
  match tree with
  | Leaf value when value = pane ->
      Ok
        (Split
           {
             orientation;
             ratio = equal_ratio;
             first = Leaf pane;
             second = Leaf new_pane;
           })
  | Leaf _ -> Error (Unknown_pane pane)
  | Split { orientation = current; ratio; first; second } -> (
      match split first ~pane ~new_pane orientation with
      | Ok first -> Ok (Split { orientation = current; ratio; first; second })
      | Error (Unknown_pane _) ->
          split second ~pane ~new_pane orientation
          |> Result.map (fun second ->
              Split { orientation = current; ratio; first; second })
      | Error _ as error -> error)

let rec close tree ~pane =
  match tree with
  | Leaf value when value = pane -> Error Cannot_close_last_pane
  | Leaf _ -> Error (Unknown_pane pane)
  | Split { first = Leaf value; second; _ } when value = pane -> Ok second
  | Split { first; second = Leaf value; _ } when value = pane -> Ok first
  | Split { orientation; ratio; first; second } -> (
      match close first ~pane with
      | Ok first -> Ok (Split { orientation; ratio; first; second })
      | Error (Unknown_pane _) ->
          close second ~pane
          |> Result.map (fun second ->
              Split { orientation; ratio; first; second })
      | Error Cannot_close_last_pane ->
          if List.length (panes tree) = 1 then Error Cannot_close_last_pane
          else Error (Unknown_pane pane)
      | Error _ as error -> error)

let clamp value = max 0 value
let first_extent available ratio = available * ratio / ratio_scale

let split_rectangles orientation ratio rectangle =
  match orientation with
  | Vertical ->
      let available = clamp (rectangle.width - 1) in
      let first_width = first_extent available ratio in
      ( available,
        first_width,
        { rectangle with width = first_width },
        {
          rectangle with
          x = rectangle.x + first_width + 1;
          width = available - first_width;
        } )
  | Horizontal ->
      let available = clamp (rectangle.height - 1) in
      let first_height = first_extent available ratio in
      ( available,
        first_height,
        { rectangle with height = first_height },
        {
          rectangle with
          y = rectangle.y + first_height + 1;
          height = available - first_height;
        } )

let rec bounds_in tree rectangle =
  match tree with
  | Leaf pane -> [ (pane, rectangle) ]
  | Split { orientation; ratio; first; second } ->
      let _, _, first_rectangle, second_rectangle =
        split_rectangles orientation ratio rectangle
      in
      bounds_in first first_rectangle @ bounds_in second second_rectangle

let orientation_for_dimension = function
  | Width -> Vertical
  | Height -> Horizontal

let ratio_for_first_extent available first_extent =
  ((first_extent * ratio_scale) + available - 1) / available
  |> max 1
  |> min (ratio_scale - 1)

let in_rectangle rectangle ~column ~row =
  column >= rectangle.x
  && column < rectangle.x + rectangle.width
  && row >= rectangle.y
  && row < rectangle.y + rectangle.height

let rec divider_at_in tree rectangle path_rev ~column ~row =
  match tree with
  | Leaf _ -> None
  | Split { orientation; ratio; first; second } ->
      let available, first_extent, first_rectangle, second_rectangle =
        split_rectangles orientation ratio rectangle
      in
      let hits_divider =
        available >= 2
        &&
        match orientation with
        | Vertical ->
            column = rectangle.x + first_extent
            && row >= rectangle.y
            && row < rectangle.y + rectangle.height
        | Horizontal ->
            row = rectangle.y + first_extent
            && column >= rectangle.x
            && column < rectangle.x + rectangle.width
      in
      if hits_divider then Some { path = List.rev path_rev; orientation }
      else if in_rectangle first_rectangle ~column ~row then
        divider_at_in first first_rectangle (First :: path_rev) ~column ~row
      else if in_rectangle second_rectangle ~column ~row then
        divider_at_in second second_rectangle (Second :: path_rev) ~column ~row
      else None

let divider_at tree ~column ~row ~width ~height =
  divider_at_in tree
    { x = 0; y = 0; width = clamp width; height = clamp height }
    [] ~column ~row

let rec drag_divider_in tree rectangle path ~target_orientation ~column ~row =
  match (tree, path) with
  | Split { orientation = actual; ratio; first; second }, [] ->
      let available, _, _, _ = split_rectangles actual ratio rectangle in
      if actual <> target_orientation || available < 2 then None
      else
        let desired_first =
          match actual with
          | Vertical -> column - rectangle.x
          | Horizontal -> row - rectangle.y
        in
        let desired_first = min (available - 1) (max 1 desired_first) in
        Some
          (Split
             {
               orientation = actual;
               ratio = ratio_for_first_extent available desired_first;
               first;
               second;
             })
  | Split { orientation; ratio; first; second }, First :: remaining ->
      let _, _, first_rectangle, _ =
        split_rectangles orientation ratio rectangle
      in
      drag_divider_in first first_rectangle remaining ~target_orientation
        ~column ~row
      |> Option.map (fun first -> Split { orientation; ratio; first; second })
  | Split { orientation; ratio; first; second }, Second :: remaining ->
      let _, _, _, second_rectangle =
        split_rectangles orientation ratio rectangle
      in
      drag_divider_in second second_rectangle remaining ~target_orientation
        ~column ~row
      |> Option.map (fun second -> Split { orientation; ratio; first; second })
  | Leaf _, _ -> None

let drag_divider tree divider ~column ~row ~width ~height =
  drag_divider_in tree
    { x = 0; y = 0; width = clamp width; height = clamp height }
    divider.path ~target_orientation:divider.orientation ~column ~row

let rec resize_in tree rectangle ~pane ~dimension ~delta =
  match tree with
  | Leaf value ->
      if value = pane then Error (Cannot_resize_pane pane)
      else Error (Unknown_pane pane)
  | Split { orientation; ratio; first; second } -> (
      let available, first_extent, first_rectangle, second_rectangle =
        split_rectangles orientation ratio rectangle
      in
      let resize_here first_contains =
        if orientation <> orientation_for_dimension dimension || available < 2
        then Error (Cannot_resize_pane pane)
        else
          let first_delta = if first_contains then delta else -delta in
          let desired_first =
            min (available - 1) (max 1 (first_extent + first_delta))
          in
          if desired_first = first_extent then Error (Cannot_resize_pane pane)
          else
            Ok
              (Split
                 {
                   orientation;
                   ratio = ratio_for_first_extent available desired_first;
                   first;
                   second;
                 })
      in
      match resize_in first first_rectangle ~pane ~dimension ~delta with
      | Ok first -> Ok (Split { orientation; ratio; first; second })
      | Error (Cannot_resize_pane _) -> resize_here true
      | Error (Unknown_pane _) -> (
          match resize_in second second_rectangle ~pane ~dimension ~delta with
          | Ok second -> Ok (Split { orientation; ratio; first; second })
          | Error (Cannot_resize_pane _) -> resize_here false
          | Error _ as error -> error)
      | Error _ as error -> error)

let resize tree ~pane ~dimension ~delta ~width ~height =
  if delta = 0 then Error (Cannot_resize_pane pane)
  else
    resize_in tree
      { x = 0; y = 0; width = clamp width; height = clamp height }
      ~pane ~dimension ~delta

let rec balance = function
  | Leaf _ as leaf -> leaf
  | Split { orientation; first; second; _ } ->
      Split
        {
          orientation;
          ratio = equal_ratio;
          first = balance first;
          second = balance second;
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
  | Split { orientation = Vertical; ratio; first; second } ->
      let _, first_width, first_rectangle, second_rectangle =
        split_rectangles Vertical ratio rectangle
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
  | Split { orientation = Horizontal; ratio; first; second } ->
      let _, first_height, first_rectangle, second_rectangle =
        split_rectangles Horizontal ratio rectangle
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
  | Cannot_resize_pane pane ->
      "cannot resize pane " ^ string_of_int pane ^ " in that direction"
  | Missing_frame pane ->
      "missing rendered frame for pane " ^ string_of_int pane
  | Frame_dimensions_mismatch
      { pane; expected_width; expected_height; actual_width; actual_height } ->
      Printf.sprintf "pane %d frame dimensions are %dx%d, expected %dx%d" pane
        actual_width actual_height expected_width expected_height
