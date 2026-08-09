type t = { top_line : int; left_column : int }

let origin = { top_line = 0; left_column = 0 }

let reconcile viewport ~line ~column ~width ~height =
  let usable_height = max 1 (height - 1) in
  let top_line =
    if line < viewport.top_line then line
    else if line >= viewport.top_line + usable_height then line - usable_height + 1
    else viewport.top_line
  in
  let left_column =
    if column < viewport.left_column then column
    else if column >= viewport.left_column + max 1 width then
      column - max 1 width + 1
    else viewport.left_column
  in
  { top_line = max 0 top_line; left_column = max 0 left_column }
