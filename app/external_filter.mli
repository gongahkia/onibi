(** Run one trusted-local executable with a selection as standard input.

    The host accepts an absolute executable path and argument vector only; it
    never parses a shell command. Output is bounded and must be valid UTF-8. *)

val maximum_bytes : int

val run :
  Zenbu_model_api.Model_effect.external_filter_request ->
  string ->
  (string, Zenbu_kernel.Error.t) result
