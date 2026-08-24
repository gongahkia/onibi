type stage =
  | Model_handle
  | Selector_resolve
  | Transformation_apply
  | Transaction_commit
  | Syntax_update
  | Script_load
  | Script_reload
  | Script_command
  | Script_selector
  | Script_transformation
  | Script_event
  | Extension_load
  | Extension_reload
  | Extension_command
  | Extension_selector
  | Extension_transformation
  | Extension_event
  | Extension_wasm_compile
  | Extension_wasm_instantiate
  | Extension_wasm_register
  | Extension_wasm_call
  | Language_sync
  | Language_hover
  | Language_definition
  | Language_completion
  | Language_code_action
  | Language_formatting
  | Language_rename
  | Lsp_decode

type key = { stage : stage; model_id : string option }
type sample = { key : key; duration : float }

type aggregate = {
  key : key;
  count : int;
  total_seconds : float;
  max_seconds : float;
}

type enabled = { capacity : int; samples : sample Queue.t }
type t = Disabled | Enabled of enabled

let disabled () = Disabled

let enabled ~capacity =
  if capacity <= 0 then
    Error
      (Zenbu_kernel.Error.Invalid_provenance "profile capacity must be positive")
  else Ok (Enabled { capacity; samples = Queue.create () })

let is_enabled = function Disabled -> false | Enabled _ -> true

let capacity = function
  | Disabled -> None
  | Enabled value -> Some value.capacity

let stage_name = function
  | Model_handle -> "model.handle"
  | Selector_resolve -> "selector.resolve"
  | Transformation_apply -> "transformation.apply"
  | Transaction_commit -> "transaction.commit"
  | Syntax_update -> "syntax.update"
  | Script_load -> "script.load"
  | Script_reload -> "script.reload"
  | Script_command -> "script.command"
  | Script_selector -> "script.selector"
  | Script_transformation -> "script.transformation"
  | Script_event -> "script.event"
  | Extension_load -> "extension.load"
  | Extension_reload -> "extension.reload"
  | Extension_command -> "extension.command"
  | Extension_selector -> "extension.selector"
  | Extension_transformation -> "extension.transformation"
  | Extension_event -> "extension.event"
  | Extension_wasm_compile -> "extension.wasm.compile"
  | Extension_wasm_instantiate -> "extension.wasm.instantiate"
  | Extension_wasm_register -> "extension.wasm.register"
  | Extension_wasm_call -> "extension.wasm.call"
  | Language_sync -> "language.sync"
  | Language_hover -> "language.hover"
  | Language_definition -> "language.definition"
  | Language_completion -> "language.completion"
  | Language_code_action -> "language.code-action"
  | Language_formatting -> "language.formatting"
  | Language_rename -> "language.rename"
  | Lsp_decode -> "lsp.decode"

let measure profiler ?model_id stage f =
  match profiler with
  | Disabled -> f ()
  | Enabled value ->
      let started = Sys.time () in
      Fun.protect
        ~finally:(fun () ->
          let duration = max 0. (Sys.time () -. started) in
          if Queue.length value.samples = value.capacity then
            ignore (Queue.take value.samples);
          Queue.add { key = { stage; model_id }; duration } value.samples)
        f

let record profiler ?model_id stage ~seconds =
  match profiler with
  | Disabled -> ()
  | Enabled value ->
      if Queue.length value.samples = value.capacity then
        ignore (Queue.take value.samples);
      Queue.add
        { key = { stage; model_id }; duration = max 0. seconds }
        value.samples

let reset = function
  | Disabled -> ()
  | Enabled value -> Queue.clear value.samples

let same_key left right =
  left.stage = right.stage && left.model_id = right.model_id

let aggregates = function
  | Disabled -> []
  | Enabled value ->
      let add (aggregates : aggregate list) (sample : sample) =
        let rec loop before = function
          | [] ->
              List.rev
                ({
                   key = sample.key;
                   count = 1;
                   total_seconds = sample.duration;
                   max_seconds = sample.duration;
                 }
                :: before)
          | aggregate :: rest when same_key aggregate.key sample.key ->
              List.rev_append before
                ({
                   aggregate with
                   count = aggregate.count + 1;
                   total_seconds = aggregate.total_seconds +. sample.duration;
                   max_seconds = max aggregate.max_seconds sample.duration;
                 }
                :: rest)
          | aggregate :: rest -> loop (aggregate :: before) rest
        in
        loop [] aggregates
      in
      Queue.to_seq value.samples |> List.of_seq |> List.fold_left add []
      |> List.sort (fun left right ->
          match
            String.compare
              (stage_name left.key.stage)
              (stage_name right.key.stage)
          with
          | 0 ->
              Option.compare String.compare left.key.model_id right.key.model_id
          | value -> value)

let aggregate_stage value = value.key.stage
let aggregate_model_id value = value.key.model_id
let aggregate_count value = value.count
let aggregate_total_seconds value = value.total_seconds

let aggregate_mean_seconds value =
  value.total_seconds /. float_of_int value.count

let aggregate_max_seconds value = value.max_seconds
