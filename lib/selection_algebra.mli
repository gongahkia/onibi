(** Model-neutral operations over an immutable editor selection set.

    These functions calculate ordinary [set-selections] intents from copied
    editor context. They do not mutate a document, retain editor state, or
    bypass transaction validation. Regex operations use the OCaml [Str] dialect
    and reject zero-width matches and byte offsets that would split a UTF-8 code
    point. *)

type direction = Forward | Backward

val select_regex :
  Editor_context.t ->
  pattern:string ->
  (Model_intent.t, Zenbu_kernel.Error.t) result
(** Replace every current selection with its non-empty regex matches. *)

val split_regex :
  Editor_context.t ->
  pattern:string ->
  (Model_intent.t, Zenbu_kernel.Error.t) result
(** Split current selections at non-empty regex matches, dropping separators. *)

val keep_matching :
  Editor_context.t ->
  pattern:string ->
  (Model_intent.t, Zenbu_kernel.Error.t) result
(** Retain selections whose text contains a non-empty regex match. *)

val remove_matching :
  Editor_context.t ->
  pattern:string ->
  (Model_intent.t, Zenbu_kernel.Error.t) result
(** Remove selections whose text contains a non-empty regex match. *)

val merge_consecutive :
  Editor_context.t -> (Model_intent.t, Zenbu_kernel.Error.t) result
(** Merge selections that touch at a byte boundary. Merged ranges are forward.
*)

val rotate_primary :
  Editor_context.t -> direction -> (Model_intent.t, Zenbu_kernel.Error.t) result
(** Move the primary selection through the sorted selection set. *)

val rotate_contents :
  Editor_context.t ->
  direction ->
  ?group_size:int ->
  unit ->
  (Model_intent.t, Zenbu_kernel.Error.t) result
(** Replace each non-empty selection with a neighbouring selection's text in
    document order. [Forward] moves each value to the next selection. An
    optional [group_size] rotates adjacent, independent groups. *)

val flip : Editor_context.t -> (Model_intent.t, Zenbu_kernel.Error.t) result
(** Swap anchor and head for every current selection. *)

val ensure_forward :
  Editor_context.t -> (Model_intent.t, Zenbu_kernel.Error.t) result
(** Normalize every current selection to increasing anchor/head order. *)
