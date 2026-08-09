type t = { range : Range.t; text : string }

let replace range ~text =
  match Text_buffer.of_utf8 text with
  | Error _ as error -> error
  | Ok _ -> Ok { range; text }

let insert ~at ~text =
  match Range.make ~start:at ~stop:at with
  | Error _ as error -> error
  | Ok range -> replace range ~text

let delete range = { range; text = "" }
let range value = value.range
let text value = value.text
let is_insertion value = Range.is_empty value.range
