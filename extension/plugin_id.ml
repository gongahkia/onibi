type t = string

let valid_segment segment =
  let length = String.length segment in
  let alphanumeric = function 'a' .. 'z' | '0' .. '9' -> true | _ -> false in
  length > 0
  && alphanumeric segment.[0]
  && alphanumeric segment.[length - 1]
  && String.for_all
       (function 'a' .. 'z' | '0' .. '9' | '-' -> true | _ -> false)
       segment

let of_string value =
  let segments = String.split_on_char '.' value in
  if List.length segments < 2 || not (List.for_all valid_segment segments) then
    Error
      (Zenbu_kernel.Error.Extension_error
         {
           code = Zenbu_kernel.Error.Invalid_manifest;
           plugin_id = Some value;
           provider = None;
           operation = Some "plugin.id";
           required = None;
           granted = [];
           message =
             "plugin IDs require at least two lowercase dot-separated \
              segments; each segment may contain lowercase letters, digits, \
              and hyphens";
         })
  else Ok value

let to_string value = value
let equal = String.equal
let compare = String.compare

let owns value semantic_id =
  String.starts_with ~prefix:(value ^ ".") semantic_id
