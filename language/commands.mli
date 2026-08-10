(** Built-in semantic edit behavior used by language-service completion and
    rename results. It is intentionally independent of LSP. *)

val provider : Zenbu_kernel.Provider.t
val apply_edits_id : string
val descriptors : unit -> Zenbu_kernel.Semantic_descriptor.t list
val behaviors : Zenbu_model_api.Semantic_behavior_registry.t
val apply_edits : Language.text_edit list -> Zenbu_model_api.Model_effect.t
