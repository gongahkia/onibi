type modifier = Shift | Control | Alt | Meta
type physical_key
type named_key = Escape | Enter | Backspace | Tab | Delete | Arrow_up | Arrow_down | Arrow_left | Arrow_right | Home | End
type key = Logical_text of string | Named_key of named_key
type t = Key_press of { key : key; modifiers : modifier list; physical_key : physical_key option } | Text_input of string

val physical_key : string -> (physical_key, Error.t) result
val logical_text : string -> (key, Error.t) result
val named_key : named_key -> key
val key_press : ?modifiers:modifier list -> ?physical_key:physical_key -> key -> t
val text_input : string -> (t, Error.t) result
val modifiers : t -> modifier list
val key : t -> key option
val physical : t -> physical_key option
val text : t -> string option
val modifier_to_string : modifier -> string
val named_key_to_string : named_key -> string
val to_string : t -> string

