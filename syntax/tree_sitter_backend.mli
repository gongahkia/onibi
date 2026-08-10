type grammar = Ocaml | Ocaml_interface | Json
type parser
type tree
type node

type edit = {
  start_byte : int;
  old_end_byte : int;
  new_end_byte : int;
  start_point : Tree_sitter.point;
  old_end_point : Tree_sitter.point;
  new_end_point : Tree_sitter.point;
}

val create_parser : grammar -> parser
val reset : parser -> unit
val parse : parser -> string -> tree
val parse_incremental : parser -> old:tree -> edits:edit list -> string -> tree
val root : tree -> node
val kind : node -> string
val is_named : node -> bool
val is_error : node -> bool
val is_missing : node -> bool
val has_error : node -> bool
val start_byte : node -> int
val end_byte : node -> int
val child_count : node -> int
val child : node -> int -> node option
val named_child_count : node -> int
val named_child : node -> int -> node option
val parent : node -> node option
val next_named_sibling : node -> node option
val previous_named_sibling : node -> node option

val named_descendant_for_byte_range :
  node -> start:int -> stop:int -> node option

val copy_tree : tree -> tree
