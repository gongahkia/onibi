type kind = Selector | Transformation

type t = {
  id : string;
  title : string;
  description : string;
  provider : Provider.t;
  kind : kind;
  requires_syntax : bool;
}

let create ~id ~title ~description ~provider ~kind ?(requires_syntax = false) ()
    =
  if
    String.length id = 0
    || String.length title = 0
    || String.length description = 0
  then
    Error
      (Error.Invalid_provenance "semantic descriptor fields must not be empty")
  else Ok { id; title; description; provider; kind; requires_syntax }

let id value = value.id
let title value = value.title
let description value = value.description
let provider value = value.provider
let kind value = value.kind
let requires_syntax value = value.requires_syntax
