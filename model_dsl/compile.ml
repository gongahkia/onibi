type t = Compile_internal.t

let compile = Compile_internal.compile
let descriptor (compiled : t) = compiled.descriptor
let model_id (compiled : t) = compiled.ir.model_id
let title (compiled : t) = compiled.ir.title
let language_version (compiled : t) = compiled.ir.version
let source_name (compiled : t) = compiled.ir.source_name
let source_fingerprint (compiled : t) = compiled.source_fingerprint
let state_count (compiled : t) = List.length compiled.states

let transition_count (compiled : t) =
  List.fold_left
    (fun total (state : Compile_internal.compiled_state) ->
      total + List.length state.ir.transitions)
    0 compiled.states

let prefix_count (compiled : t) =
  List.fold_left
    (fun total state -> total + List.length (Compile_internal.prefixes state))
    0 compiled.states

let action_count (compiled : t) = List.length compiled.ir.actions
