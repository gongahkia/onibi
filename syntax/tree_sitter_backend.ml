type grammar = Ocaml | Ocaml_interface | Json
type parser = Tree_sitter.Parser.t
type tree = Tree_sitter.Tree.t
type node = Tree_sitter.Node.t
type query = Tree_sitter.Query.t
type query_cursor = Tree_sitter.Query_cursor.t
type query_capture = { name : string option; node : node }

type edit = {
  start_byte : int;
  old_end_byte : int;
  new_end_byte : int;
  start_point : Tree_sitter.point;
  old_end_point : Tree_sitter.point;
  new_end_point : Tree_sitter.point;
}

let language = function
  | Ocaml -> Tree_sitter_ocaml.ocaml ()
  | Ocaml_interface -> Tree_sitter_ocaml.interface ()
  | Json -> Tree_sitter_json.language ()

let create_parser grammar = Tree_sitter.Parser.create (language grammar)
let reset = Tree_sitter.Parser.reset
let parse parser source = Tree_sitter.Parser.parse_string parser source

let parse_incremental parser ~old ~edits source =
  let edited = Tree_sitter.Tree.copy old in
  List.iter
    (fun edit ->
      Tree_sitter.Tree.edit edited ~start_byte:edit.start_byte
        ~old_end_byte:edit.old_end_byte ~new_end_byte:edit.new_end_byte
        ~start_point:edit.start_point ~old_end_point:edit.old_end_point
        ~new_end_point:edit.new_end_point)
    edits;
  Tree_sitter.Parser.parse_string ~old:edited parser source

let root = Tree_sitter.Tree.root_node
let kind = Tree_sitter.Node.kind
let is_named = Tree_sitter.Node.is_named
let is_error = Tree_sitter.Node.is_error
let is_missing = Tree_sitter.Node.is_missing
let has_error = Tree_sitter.Node.has_error
let start_byte = Tree_sitter.Node.start_byte
let end_byte = Tree_sitter.Node.end_byte
let child_count = Tree_sitter.Node.child_count
let child = Tree_sitter.Node.child
let named_child_count = Tree_sitter.Node.named_child_count
let named_child = Tree_sitter.Node.named_child
let parent = Tree_sitter.Node.parent
let next_named_sibling = Tree_sitter.Node.next_named_sibling
let previous_named_sibling = Tree_sitter.Node.prev_named_sibling

let named_descendant_for_byte_range node ~start ~stop =
  Tree_sitter.Node.named_descendant_for_byte_range node ~start ~end_:stop

let copy_tree = Tree_sitter.Tree.copy

let compile_query grammar ~source =
  Tree_sitter.Query.create (language grammar) ~source

let query_pattern_count = Tree_sitter.Query.pattern_count
let query_capture_count = Tree_sitter.Query.capture_count
let create_query_cursor = Tree_sitter.Query_cursor.create

let execute_query cursor query node =
  Tree_sitter.Query_cursor.exec cursor query node

let next_query_capture cursor query =
  match Tree_sitter.Query_cursor.next_capture cursor query with
  | None -> None
  | Some capture ->
      Some
        {
          name =
            Tree_sitter.Query.capture_name_for_id query capture.capture_index;
          node = capture.node;
        }

let query_capture_name value = value.name
let query_capture_node value = value.node
