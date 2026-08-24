(** Bounded pane-local renderer policy.

    A pane either inherits the session presentation or selects one built-in
    profile without a buffer line. The session retains ownership of custom
    presentation files and the optional workspace-wide buffer line. *)

type t

val default : t
val maximum_scroll_margin : int

val create :
  scroll_margin:int -> presentation:string option -> (t, string) result

val scroll_margin : t -> int
val presentation : t -> string option
val effective_presentation : default:Presentation.t -> t -> Presentation.t
