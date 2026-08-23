(** Terminal chrome policy kept separate from semantic editor state.

    A presentation profile controls only renderer-owned space allocation. It
    cannot create widgets, receive input, or mutate a document. *)

type line_numbers = Hidden | Absolute | Relative
type status_line = Detailed | Minimal | Hidden_status
type buffer_line = Visible | Hidden_buffer_line
type t

val default : t
val numbered : t
val relative : t
val minimal : t
val bare : t
val buffered : t
val builtins : unit -> t list
val name : t -> string
val line_numbers : t -> line_numbers
val status_line : t -> status_line
val buffer_line : t -> buffer_line
val find_builtin : string -> t option

val load : string -> (t, string) result
(** Load a validated TOML presentation profile.

    The root accepts [name], [line_numbers] ([none], [absolute], or [relative]),
    [status_line] ([detailed], [minimal], or [hidden]), and [buffer_line]
    ([visible] or [hidden]). *)
