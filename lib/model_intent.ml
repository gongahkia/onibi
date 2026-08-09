open Zenbu_kernel

type selector = Current_selections | Document | Next_text_unit | Previous_text_unit
type transformation = Select | Delete | Replace_text of string
type t = Intent.t

let selector_to_kernel = function
  | Current_selections -> Selector.Current_selections
  | Document -> Selector.Document
  | Next_text_unit -> Selector.Next_text_unit
  | Previous_text_unit -> Selector.Previous_text_unit

let transformation_to_kernel = function
  | Select -> Transformation.Select
  | Delete -> Transformation.Delete
  | Replace_text text -> Transformation.Replace_text text

let insert_text text = Intent.Insert_text text
let delete_selected_ranges = Intent.Delete_selected_ranges
let replace_selected_ranges text = Intent.Replace_selected_ranges text

let set_selections ~selections ~primary =
  let rec specs values = function
    | [] -> Ok (List.rev values)
    | (anchor_offset, head_offset) :: rest -> (
        match Selection_spec.make ~anchor_offset ~head_offset with
        | Error _ as error -> error
        | Ok spec -> specs (spec :: values) rest)
  in
  match specs [] selections with
  | Error _ as error -> error
  | Ok selections -> Ok (Intent.Set_selections { selections; primary })

let apply ~selector ~transformation =
  Intent.Apply
    {
      selector = selector_to_kernel selector;
      transformation = transformation_to_kernel transformation;
    }

let identity = Intent.identity
let to_kernel value = value

