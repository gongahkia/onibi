open Zenbu_kernel

type direction = Forward | Backward

type offsets = {
  anchor_offset : int;
  head_offset : int;
  start_offset : int;
  stop_offset : int;
}

type group = {
  anchor_offset : int;
  head_offset : int;
  start_offset : int;
  stop_offset : int;
  count : int;
  contains_primary : bool;
}

let invalid message = Error.Invalid_command_arguments message

let selections context =
  Editor_context.selections context |> fun values ->
  ( values.primary_index,
    List.map
      (fun (selection : Editor_context.selection) ->
        {
          anchor_offset = selection.anchor_offset;
          head_offset = selection.head_offset;
          start_offset = min selection.anchor_offset selection.head_offset;
          stop_offset = max selection.anchor_offset selection.head_offset;
        })
      values.selections )

let set_selections context ~selections ~primary =
  if selections = [] then
    Error
      (Error.Invalid_selection_set "selection operation produced no selections")
  else
    match Text_buffer.of_utf8 (Editor_context.contents context) with
    | Error _ as error -> error
    | Ok buffer ->
        let valid (anchor_offset, head_offset) =
          Text_buffer.is_code_point_boundary buffer anchor_offset
          && Text_buffer.is_code_point_boundary buffer head_offset
        in
        if List.for_all valid selections then
          Model_intent.set_selections ~selections ~primary
        else
          Error
            (invalid
               "selection operation produced an offset that splits a UTF-8 \
                code point")

let compile pattern =
  try Ok (Str.regexp pattern)
  with Failure reason | Invalid_argument reason ->
    Error (invalid ("invalid selection regex: " ^ reason))

let matches regex text ~start_offset ~stop_offset =
  let selected = String.sub text start_offset (stop_offset - start_offset) in
  let rec collect cursor values =
    try
      ignore (Str.search_forward regex selected cursor);
      let start = Str.match_beginning () in
      let stop = Str.match_end () in
      if start = stop then
        Error (invalid "selection regex must not match empty text")
      else collect stop ((start_offset + start, start_offset + stop) :: values)
    with Not_found -> Ok (List.rev values)
  in
  collect 0 []

let selected_matches context ~pattern =
  match compile pattern with
  | Error _ as error -> error
  | Ok regex ->
      let text = Editor_context.contents context in
      let primary, values = selections context in
      let rec collect index selected primary_index = function
        | [] -> Ok (List.rev selected, Option.value ~default:0 primary_index)
        | (value : offsets) :: rest -> (
            match
              matches regex text ~start_offset:value.start_offset
                ~stop_offset:value.stop_offset
            with
            | Error _ as error -> error
            | Ok found ->
                let primary_index =
                  if index = primary && found <> [] then
                    Some (List.length selected)
                  else primary_index
                in
                collect (index + 1)
                  (List.rev_append found selected)
                  primary_index rest)
      in
      collect 0 [] None values

let select_regex context ~pattern =
  match selected_matches context ~pattern with
  | Error _ as error -> error
  | Ok (matches, primary) -> set_selections context ~selections:matches ~primary

let split_selection regex text (value : offsets) =
  match
    matches regex text ~start_offset:value.start_offset
      ~stop_offset:value.stop_offset
  with
  | Error _ as error -> error
  | Ok [] -> Ok [ (value.anchor_offset, value.head_offset) ]
  | Ok separators ->
      let rec pieces cursor values = function
        | [] ->
            let values =
              if cursor < value.stop_offset then
                (cursor, value.stop_offset) :: values
              else values
            in
            Ok (List.rev values)
        | (start, stop) :: rest ->
            let values =
              if cursor < start then (cursor, start) :: values else values
            in
            pieces stop values rest
      in
      pieces value.start_offset [] separators

let split_regex context ~pattern =
  match compile pattern with
  | Error _ as error -> error
  | Ok regex -> (
      let text = Editor_context.contents context in
      let primary, values = selections context in
      let rec collect index selected primary_index = function
        | [] -> Ok (List.rev selected, Option.value ~default:0 primary_index)
        | (value : offsets) :: rest -> (
            match split_selection regex text value with
            | Error _ as error -> error
            | Ok pieces ->
                let primary_index =
                  if index = primary && pieces <> [] then
                    Some (List.length selected)
                  else primary_index
                in
                collect (index + 1)
                  (List.rev_append pieces selected)
                  primary_index rest)
      in
      match collect 0 [] None values with
      | Error _ as error -> error
      | Ok (pieces, primary) ->
          set_selections context ~selections:pieces ~primary)

let filter_matching context ~pattern ~keep =
  match compile pattern with
  | Error _ as error -> error
  | Ok regex -> (
      let text = Editor_context.contents context in
      let primary, values = selections context in
      let rec collect index selected primary_index = function
        | [] -> Ok (List.rev selected, Option.value ~default:0 primary_index)
        | (value : offsets) :: rest -> (
            match
              matches regex text ~start_offset:value.start_offset
                ~stop_offset:value.stop_offset
            with
            | Error _ as error -> error
            | Ok found ->
                let retain = found <> [] = keep in
                let selected, primary_index =
                  if retain then
                    ( (value.anchor_offset, value.head_offset) :: selected,
                      if index = primary then Some (List.length selected)
                      else primary_index )
                  else (selected, primary_index)
                in
                collect (index + 1) selected primary_index rest)
      in
      match collect 0 [] None values with
      | Error _ as error -> error
      | Ok (selected, primary) ->
          set_selections context ~selections:selected ~primary)

let keep_matching context ~pattern = filter_matching context ~pattern ~keep:true

let remove_matching context ~pattern =
  filter_matching context ~pattern ~keep:false

let group_of_selection index primary (value : offsets) =
  {
    anchor_offset = value.anchor_offset;
    head_offset = value.head_offset;
    start_offset = value.start_offset;
    stop_offset = value.stop_offset;
    count = 1;
    contains_primary = index = primary;
  }

let extend_group group (value : offsets) contains_primary =
  {
    group with
    stop_offset = value.stop_offset;
    count = group.count + 1;
    contains_primary = group.contains_primary || contains_primary;
  }

let group_offsets group =
  if group.count = 1 then (group.anchor_offset, group.head_offset)
  else (group.start_offset, group.stop_offset)

let merge_consecutive context =
  let primary, values = selections context in
  match values with
  | [] -> assert false
  | first :: rest ->
      let rec collect index current groups = function
        | [] -> List.rev (current :: groups)
        | (value : offsets) :: rest ->
            if current.stop_offset = value.start_offset then
              collect (index + 1)
                (extend_group current value (index = primary))
                groups rest
            else
              collect (index + 1)
                (group_of_selection index primary value)
                (current :: groups) rest
      in
      let groups = collect 1 (group_of_selection 0 primary first) [] rest in
      let selections = List.map group_offsets groups in
      let primary =
        List.find_index (fun group -> group.contains_primary) groups
        |> Option.value ~default:0
      in
      set_selections context ~selections ~primary

let rotate_primary context direction =
  let primary, values = selections context in
  let count = List.length values in
  if count < 2 then
    Error
      (invalid "rotating the primary selection requires multiple selections")
  else
    let primary =
      match direction with
      | Forward -> (primary + 1) mod count
      | Backward -> (primary + count - 1) mod count
    in
    let selections =
      List.map
        (fun (value : offsets) -> (value.anchor_offset, value.head_offset))
        values
    in
    set_selections context ~selections ~primary

let flip context =
  let primary, values = selections context in
  set_selections context
    ~selections:
      (List.map
         (fun (value : offsets) -> (value.head_offset, value.anchor_offset))
         values)
    ~primary

let ensure_forward context =
  let primary, values = selections context in
  set_selections context
    ~selections:
      (List.map
         (fun (value : offsets) -> (value.start_offset, value.stop_offset))
         values)
    ~primary
