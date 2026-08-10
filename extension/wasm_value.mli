(** Private encoder for the non-recursive WIT value tree in
    [docs/wit/zenbu-plugin.wit]. *)

val encode :
  Zenbu_model_api.Extension_value.t -> Zenbu_model_api.Extension_value.t

val decode :
  Zenbu_model_api.Extension_value.t ->
  (Zenbu_model_api.Extension_value.t, string) result
