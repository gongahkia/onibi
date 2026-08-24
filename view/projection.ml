type source_row = {
  source_line : Display.source_line;
  fold : Fold.range option;
  inline : Decoration.resolved list;
}

type virtual_row = {
  anchor : Display.source_line;
  decoration : Decoration.resolved;
}

type row = Source of source_row | Virtual of virtual_row

let source_line value = value.source_line
let fold value = value.fold
let inline value = value.inline
let virtual_anchor value = value.anchor
let virtual_decoration value = value.decoration

let row_source_line = function Source row -> Some row.source_line | Virtual _ -> None

let row_anchor_line = function
  | Source row -> row.source_line
  | Virtual row -> row.anchor

let last_source_line rows =
  rows
  |> List.rev
  |> List.find_map row_source_line

let anchored_items collection source_lines source_line =
  Decoration.items collection
  |> List.filter (fun decoration ->
         let anchor =
           match Decoration.item decoration with
           | Decoration.Inline { anchor_offset; _ }
           | Decoration.Virtual_line { anchor_offset; _ } ->
               anchor_offset
         in
         let line = Display.source_line_at source_lines anchor in
         line.number = source_line.Display.number)

let project ~contents ~document_id ~document_version ~folds ~decorations
    source_lines =
  let collection =
    Decoration.collect ~contents ~document_id ~document_version decorations
  in
  let folded = Fold.project folds source_lines in
  let rows =
    folded
    |> List.concat_map (fun folded_line ->
           let source_line = Fold.source_line folded_line in
           let anchored = anchored_items collection source_lines source_line in
           let before, inline, after =
             List.fold_left
               (fun (before, inline, after) decoration ->
                 match Decoration.item decoration with
                 | Decoration.Inline _ -> (before, decoration :: inline, after)
                 | Decoration.Virtual_line { placement = Decoration.Before; _ }
                   ->
                     (decoration :: before, inline, after)
                 | Decoration.Virtual_line { placement = Decoration.After; _ } ->
                     (before, inline, decoration :: after))
               ([], [], []) anchored
           in
           let virtual_rows decorations =
             decorations
             |> List.rev
             |> List.map (fun decoration ->
                    Virtual { anchor = source_line; decoration })
           in
           virtual_rows before
           @ [
               Source
                 {
                   source_line;
                   fold = Fold.fold folded_line;
                   inline = List.rev inline;
                 };
             ]
           @ virtual_rows after)
  in
  (rows, collection)

let index_for_offset rows source_lines offset =
  match rows with
  | [] -> 0
  | _ ->
      let target = Display.source_line_at source_lines offset in
      let rec loop index = function
        | [] -> max 0 (List.length rows - 1)
        | Source row :: rest -> (
            if row.source_line.number = target.number then index
            else
              match row.fold with
              | Some fold
                when target.number > Fold.start_line fold
                     && target.number <= Fold.stop_line fold ->
                  index
              | Some _ | None -> loop (index + 1) rest)
        | Virtual _ :: rest -> loop (index + 1) rest
      in
      loop 0 rows
