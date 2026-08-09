open Zenbu_kernel

type kind = Characterwise | Linewise
type placement = Before | After | Replace
type slot = string
type entry = { kind : kind; contents : string }

module Slot_map = Map.Make (String)

type t = entry Slot_map.t

let unnamed = "unnamed"

let valid_slot_name value =
  String.length value > 0
  && String.for_all
       (function
         | 'a' .. 'z' | 'A' .. 'Z' | '0' .. '9' | '-' | '_' | '.' -> true
         | _ -> false)
       value

let slot value =
  if valid_slot_name value then Ok value else Error (Error.Invalid_clipboard_slot value)

let slot_name value = value

let entry ~kind ~contents =
  match Text_buffer.of_utf8 contents with
  | Error _ as error -> error
  | Ok _ -> Ok { kind; contents }

let contents value = value.contents
let kind value = value.kind
let empty = Slot_map.empty
let find value ~slot = Slot_map.find_opt slot value
let store value ~slot ~entry = Slot_map.add slot entry value
let kind_name = function Characterwise -> "characterwise" | Linewise -> "linewise"

let placement_name = function
  | Before -> "before"
  | After -> "after"
  | Replace -> "replace"
