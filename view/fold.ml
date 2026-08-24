type source = Manual | Syntax

type range = {
  source : source;
  start_offset : int;
  stop_offset : int;
  start_line : int;
  stop_line : int;
}

type projected_line = { source_line : Display.source_line; fold : range option }

let source value = value.source
let start_offset value = value.start_offset
let stop_offset value = value.stop_offset
let start_line value = value.start_line
let stop_line value = value.stop_line
let source_line value = value.source_line
let fold value = value.fold

let hidden_line_count value =
  match value.fold with
  | None -> 0
  | Some range -> range.stop_line - range.start_line

let create ~source source_lines ~start_offset ~stop_offset =
  if start_offset < 0 || stop_offset < 0 then
    Error "fold offsets must be nonnegative"
  else
    match source_lines with
    | [] -> Error "cannot fold an empty source-line projection"
    | _ ->
        let first_offset = min start_offset stop_offset in
        let last_offset = max start_offset stop_offset in
        let final = List.hd (List.rev source_lines) in
        if last_offset > final.Display.end_offset then
          Error "fold offset exceeds source"
        else
          let first = Display.source_line_at source_lines first_offset in
          let last = Display.source_line_at source_lines last_offset in
          if first.number >= last.number then
            Error "a fold must cover at least two source lines"
          else
            Ok
              {
                source;
                start_offset = first.start_offset;
                stop_offset = last.stop_offset;
                start_line = first.number;
                stop_line = last.number;
              }

let overlaps left right =
  left.start_line <= right.stop_line && right.start_line <= left.stop_line

let strictly_contains outer inner =
  outer.start_line < inner.start_line && inner.stop_line <= outer.stop_line

let same_range left right =
  left.start_line = right.start_line && left.stop_line = right.stop_line

let validate ranges =
  let rec check = function
    | [] -> Ok ()
    | range :: rest -> (
        if range.start_line >= range.stop_line then
          Error "a fold must keep one header source line visible"
        else
          match
            List.find_opt
              (fun other ->
                overlaps range other
                && (not (strictly_contains range other))
                && not (strictly_contains other range))
              rest
          with
          | Some other when same_range range other ->
              Error "duplicate fold range"
          | Some _ ->
              Error "fold ranges may be nested but must not partially overlap"
          | None -> check rest)
  in
  check ranges

let fold_starting_at ranges line =
  List.find_opt (fun range -> range.start_line = line.Display.number) ranges

let project ranges source_lines =
  match validate ranges with
  | Error _ ->
      (* Session validates before storing ranges.  Rendering an invalid stale
         value as plain source is safer than hiding unrelated source lines. *)
      List.map (fun source_line -> { source_line; fold = None }) source_lines
  | Ok () ->
      let rec loop projected = function
        | [] -> List.rev projected
        | source_line :: rest -> (
            match fold_starting_at ranges source_line with
            | None -> loop ({ source_line; fold = None } :: projected) rest
            | Some range ->
                let remaining =
                  List.filter
                    (fun line -> line.Display.number > range.stop_line)
                    rest
                in
                loop ({ source_line; fold = Some range } :: projected) remaining
            )
      in
      loop [] source_lines

let index_for_offset projected source_lines offset =
  match projected with
  | [] -> 0
  | _ ->
      let target = Display.source_line_at source_lines offset in
      let rec loop index = function
        | [] -> max 0 (List.length projected - 1)
        | line :: rest -> (
            let source_line = line.source_line in
            if source_line.number = target.number then index
            else
              match line.fold with
              | Some range
                when target.number > range.start_line
                     && target.number <= range.stop_line ->
                  index
              | Some _ | None -> loop (index + 1) rest)
      in
      loop 0 projected
