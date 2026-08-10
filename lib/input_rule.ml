type pattern =
  | Exact of string
  | Named of string
  | Text_input
  | Text_range of string

type kind = Binding | Prefix | Catch_all

type t = {
  id : string;
  pattern : pattern;
  kind : kind;
  summary : string;
  next_status : string option;
  command_id : string option;
  selector_id : string option;
  transformation_id : string option;
  requires_syntax : bool;
}

let create ~id ~pattern ~kind ~summary ?next_status ?command_id ?selector_id
    ?transformation_id ?(requires_syntax = false) () =
  if String.length id = 0 || String.length summary = 0 then
    Error
      (Zenbu_kernel.Error.Invalid_model_status
         "input rule fields must not be empty")
  else
    Ok
      {
        id;
        pattern;
        kind;
        summary;
        next_status;
        command_id;
        selector_id;
        transformation_id;
        requires_syntax;
      }

let id value = value.id
let pattern value = value.pattern
let kind value = value.kind
let summary value = value.summary
let next_status value = value.next_status
let command_id value = value.command_id
let selector_id value = value.selector_id
let transformation_id value = value.transformation_id
let requires_syntax value = value.requires_syntax

let pattern_to_string = function
  | Exact value -> value
  | Named value -> value
  | Text_input -> "committed text"
  | Text_range value -> value
