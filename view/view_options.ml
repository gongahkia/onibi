type t = { scroll_margin : int; presentation : string option }

let maximum_scroll_margin = 32
let default = { scroll_margin = 0; presentation = None }
let scroll_margin value = value.scroll_margin
let presentation value = value.presentation

let pane_local_presentation name =
  match Presentation.find_builtin name with
  | None -> Error "pane presentation must name a built-in profile"
  | Some presentation -> (
      match Presentation.buffer_line presentation with
      | Presentation.Hidden_buffer_line -> Ok ()
      | Presentation.Visible ->
          Error "pane presentation cannot change the session buffer line")

let create ~scroll_margin ~presentation =
  if scroll_margin < 0 || scroll_margin > maximum_scroll_margin then
    Error
      (Printf.sprintf "scroll margin must be between 0 and %d"
         maximum_scroll_margin)
  else
    match presentation with
    | None -> Ok { scroll_margin; presentation }
    | Some name ->
        pane_local_presentation name
        |> Result.map (fun () -> { scroll_margin; presentation })

let effective_presentation ~default value =
  match value.presentation with
  | Some name -> (
      match Presentation.find_builtin name with
      | Some presentation -> presentation
      | None -> default)
  | None -> default
