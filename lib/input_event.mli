type modifier = Shift | Control | Alt | Meta
type physical_key

type named_key =
  | Escape
  | Enter
  | Backspace
  | Tab
  | Delete
  | Arrow_up
  | Arrow_down
  | Arrow_left
  | Arrow_right
  | Home
  | End

type key = Logical_text of string | Named_key of named_key
type mouse_button = Primary | Middle | Secondary | Wheel_up | Wheel_down
type mouse_action = Press of mouse_button | Drag | Release

type t =
  | Key_press of {
      key : key;
      modifiers : modifier list;
      physical_key : physical_key option;
    }
  | Text_input of string
  | Mouse of {
      action : mouse_action;
      column : int;
      row : int;
      modifiers : modifier list;
    }

type binding_pattern =
  | Exact_event of t
  | Any_text_input
      (** A static input event or the committed-text wildcard used by a custom
          text-entry binding. *)

val physical_key : string -> (physical_key, Zenbu_kernel.Error.t) result
val logical_text : string -> (key, Zenbu_kernel.Error.t) result
val named_key : named_key -> key

val key_press :
  ?modifiers:modifier list -> ?physical_key:physical_key -> key -> t

val text_input : string -> (t, Zenbu_kernel.Error.t) result

val mouse :
  ?modifiers:modifier list ->
  mouse_action ->
  column:int ->
  row:int ->
  (t, Zenbu_kernel.Error.t) result

val modifiers : t -> modifier list
val key : t -> key option
val physical : t -> physical_key option
val text : t -> string option
val mouse_action : t -> mouse_action option
val mouse_position : t -> (int * int) option

val binding_event_of_string : string -> (t, Zenbu_kernel.Error.t) result
(** Parse a configuration binding token such as [Ctrl-X], [Alt-Enter], or
    [Space]. Modifiers are joined to the logical or named key with [-]. *)

val binding_sequence_of_string : string -> (t list, Zenbu_kernel.Error.t) result
(** Parse one to sixteen binding tokens separated by one ASCII space. A literal
    space key is written [Space]. *)

val binding_pattern_sequence_of_string :
  string -> (binding_pattern list, Zenbu_kernel.Error.t) result
(** Parse one to sixteen binding tokens, additionally accepting [<text>] as a
    wildcard for one committed [Text_input] event. *)

val binding_pattern_matches : binding_pattern -> t -> bool
val binding_patterns_overlap : binding_pattern -> binding_pattern -> bool
val modifier_to_string : modifier -> string
val named_key_to_string : named_key -> string
val mouse_button_to_string : mouse_button -> string
val binding_sequence_to_string : t list -> string
val binding_pattern_to_string : binding_pattern -> string
val binding_pattern_sequence_to_string : binding_pattern list -> string
val to_string : t -> string
