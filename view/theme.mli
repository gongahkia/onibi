(** Terminal-independent presentation themes.

    A theme maps stable semantic [Frame.style] values to colours and text
    decorations. Rendering continues to emit semantic styles, so changing a
    theme cannot change editing state, layout, or transaction behavior. *)

type ansi =
  | Black
  | Red
  | Green
  | Yellow
  | Blue
  | Magenta
  | Cyan
  | White
  | Light_black
  | Light_red
  | Light_green
  | Light_yellow
  | Light_blue
  | Light_magenta
  | Light_cyan
  | Light_white

type color = Default | Ansi of ansi | Rgb of int * int * int
type decoration = Bold | Italic | Underline

type attribute = {
  foreground : color;
  background : color;
  decorations : decoration list;
}

type t

val default : t
val dark : t
val light : t
val builtins : unit -> t list
val name : t -> string
val find_builtin : string -> t option
val attribute : t -> Frame.style -> attribute
val style_name : Frame.style -> string

val load : string -> (t, string) result
(** Load a TOML overlay over the built-in default theme.

    The optional top-level [name] is a string. Each semantic style can be a TOML
    table with optional [foreground] and [background] values of [default] or
    [#RRGGBB], and optional boolean [bold], [italic], and [underline] values.
    Unknown style names and fields are errors so theme typos cannot silently
    change the appearance. *)
