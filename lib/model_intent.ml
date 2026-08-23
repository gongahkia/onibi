open Zenbu_kernel

type selector =
  | Current_selections
  | Document
  | Next_text_unit
  | Previous_text_unit
  | Next_word
  | Previous_word
  | Word_end
  | Current_word
  | Around_word
  | Current_line
  | Line_start
  | Line_end
  | First_nonblank
  | Document_start
  | Document_end
  | Next_line
  | Previous_line
  | All_occurrences

type transformation =
  | Select
  | Delete
  | Replace_text of string
  | Collapse_to_start
  | Collapse_to_end

type t = Intent.t

let selector_to_kernel = function
  | Current_selections -> Selector.Current_selections
  | Document -> Selector.Document
  | Next_text_unit -> Selector.Next_text_unit
  | Previous_text_unit -> Selector.Previous_text_unit
  | Next_word -> Selector.Next_word
  | Previous_word -> Selector.Previous_word
  | Word_end -> Selector.Word_end
  | Current_word -> Selector.Current_word
  | Around_word -> Selector.Around_word
  | Current_line -> Selector.Current_line
  | Line_start -> Selector.Line_start
  | Line_end -> Selector.Line_end
  | First_nonblank -> Selector.First_nonblank
  | Document_start -> Selector.Document_start
  | Document_end -> Selector.Document_end
  | Next_line -> Selector.Next_line
  | Previous_line -> Selector.Previous_line
  | All_occurrences -> Selector.All_occurrences

let transformation_to_kernel = function
  | Select -> Transformation.Select
  | Delete -> Transformation.Delete
  | Replace_text text -> Transformation.Replace_text text
  | Collapse_to_start -> Transformation.Collapse_to_start
  | Collapse_to_end -> Transformation.Collapse_to_end

let selector_of_string value =
  match Selector.of_string value with
  | Error _ as error -> error
  | Ok selector ->
      Ok
        (match selector with
        | Selector.Current_selections -> Current_selections
        | Selector.Document -> Document
        | Selector.Next_text_unit -> Next_text_unit
        | Selector.Previous_text_unit -> Previous_text_unit
        | Selector.Next_word -> Next_word
        | Selector.Previous_word -> Previous_word
        | Selector.Word_end -> Word_end
        | Selector.Current_word -> Current_word
        | Selector.Around_word -> Around_word
        | Selector.Current_line -> Current_line
        | Selector.Line_start -> Line_start
        | Selector.Line_end -> Line_end
        | Selector.First_nonblank -> First_nonblank
        | Selector.Document_start -> Document_start
        | Selector.Document_end -> Document_end
        | Selector.Next_line -> Next_line
        | Selector.Previous_line -> Previous_line
        | Selector.All_occurrences -> All_occurrences)

let transformation_of_string value =
  match value with
  | "select" -> Ok Select
  | "delete" -> Ok Delete
  | "collapse-to-start" -> Ok Collapse_to_start
  | "collapse-to-end" -> Ok Collapse_to_end
  | value when String.starts_with ~prefix:"replace:" value ->
      Ok
        (Replace_text
           (String.sub value 8 (String.length value - String.length "replace:")))
  | _ -> Error (Error.Invalid_transformation value)

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

let is_textual = function
  | Intent.Insert_text _ | Intent.Delete_selected_ranges
  | Intent.Replace_selected_ranges _ ->
      true
  | Intent.Set_selections _ -> false
  | Intent.Apply { transformation = Transformation.Delete; _ }
  | Intent.Apply { transformation = Transformation.Replace_text _; _ } ->
      true
  | Intent.Apply
      {
        transformation =
          ( Transformation.Select | Transformation.Collapse_to_start
          | Transformation.Collapse_to_end );
        _;
      } ->
      false

let semantic_components value =
  match value with
  | Intent.Apply { selector; transformation } ->
      ( Some (Selector.to_string selector),
        Some (Transformation.name transformation) )
  | Intent.Insert_text _ | Intent.Delete_selected_ranges
  | Intent.Replace_selected_ranges _ | Intent.Set_selections _ ->
      (None, None)

let to_kernel value = value
