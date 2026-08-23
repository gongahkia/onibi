open Zenbu_kernel

type kind = Characterwise | Linewise
type placement = Before | After | Replace
type slot = string
type entry = { kind : kind; contents : string }

module Slot_map = Map.Make (String)

type t = { slots : entry Slot_map.t; kill_ring : entry list }

let unnamed = "unnamed"
let maximum_kill_ring_entries = 120

let valid_slot_name value =
  String.length value > 0
  && String.for_all
       (function
         | 'a' .. 'z' | 'A' .. 'Z' | '0' .. '9' | '-' | '_' | '.' -> true
         | _ -> false)
       value

let slot value =
  if valid_slot_name value then Ok value
  else Error (Error.Invalid_clipboard_slot value)

let slot_name value = value

let entry ~kind ~contents =
  match Text_buffer.of_utf8 contents with
  | Error _ as error -> error
  | Ok _ -> Ok { kind; contents }

let contents value = value.contents
let kind value = value.kind
let empty = { slots = Slot_map.empty; kill_ring = [] }
let find value ~slot = Slot_map.find_opt slot value.slots

let store value ~slot ~entry =
  { value with slots = Slot_map.add slot entry value.slots }

let take_kill_entries entries =
  let rec take remaining values = function
    | _ when remaining = 0 -> List.rev values
    | [] -> List.rev values
    | entry :: rest -> take (remaining - 1) (entry :: values) rest
  in
  take maximum_kill_ring_entries [] entries

let store_kill value ~slot ~entry =
  {
    slots = Slot_map.add slot entry value.slots;
    kill_ring = take_kill_entries (entry :: value.kill_ring);
  }

let find_kill value ~index =
  if index < 0 then None else List.nth_opt value.kill_ring index

let kill_ring_length value = List.length value.kill_ring
let kill_ring_entries value = value.kill_ring

let with_kill_ring value kill_ring =
  { value with kill_ring = take_kill_entries kill_ring }

let kind_name = function
  | Characterwise -> "characterwise"
  | Linewise -> "linewise"

let placement_name = function
  | Before -> "before"
  | After -> "after"
  | Replace -> "replace"
