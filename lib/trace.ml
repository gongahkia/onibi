type enabled = { capacity : int; events : Trace_event.t Queue.t }
type t = Disabled | Enabled of enabled

let disabled () = Disabled

let enabled ~capacity =
  if capacity <= 0 then
    Error (Zenbu_kernel.Error.Invalid_provenance "trace capacity must be positive")
  else Ok (Enabled { capacity; events = Queue.create () })

let is_enabled = function Disabled -> false | Enabled _ -> true
let capacity = function Disabled -> None | Enabled value -> Some value.capacity

let emit_lazy trace create =
  match trace with
  | Disabled -> ()
  | Enabled value ->
      if Queue.length value.events = value.capacity then ignore (Queue.take value.events);
      Queue.add (create ()) value.events

let events = function
  | Disabled -> []
  | Enabled value -> Queue.to_seq value.events |> List.of_seq

let clear = function Disabled -> () | Enabled value -> Queue.clear value.events
