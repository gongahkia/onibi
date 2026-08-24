open Zenbu_kernel

type kind = Command | Selector | Transformation | Model | Event

type invocation = {
  token : string;
  provider : Provider.t;
  granted : string list;
}

type request = {
  kind : kind;
  operation : string;
  provider : Provider.t;
  granted : string list;
  context : Extension_value.t;
  arguments : Extension_value.t;
}

type response = Immediate of Extension_value.t | Deferred of deferred
and deferred = { start : unit -> (call, Error.t) result }

and call = {
  wakeup_fd : Unix.file_descr;
  closed : unit -> bool;
  take : unit -> (Extension_value.t, Error.t) result option;
  cancel : unit -> unit;
}

type t = {
  runtime : string;
  invoke : invocation -> request -> (response, Error.t) result;
}

let create ~runtime ~invoke = { runtime; invoke }
let runtime value = value.runtime
let invocation ~token ~provider ~granted = { token; provider; granted }
let invocation_provider (value : invocation) = value.provider
let invocation_granted (value : invocation) = value.granted
let invocation_token (value : invocation) = value.token

let kind_name = function
  | Command -> "command"
  | Selector -> "selector"
  | Transformation -> "transformation"
  | Model -> "model"
  | Event -> "event"

let has values value = List.mem value values

let compact_node node =
  let open Zenbu_syntax.Syntax.Snapshot.Node in
  Extension_value.Record
    [
      ( "kind",
        Extension_value.Text (kind node |> Zenbu_syntax.Syntax.Kind.to_string)
      );
      ("start", Extension_value.Integer (start_offset node));
      ("stop", Extension_value.Integer (stop_offset node));
      ("named", Extension_value.Bool (is_named node));
      ("error", Extension_value.Bool (has_error node));
      ("missing", Extension_value.Bool (is_missing node));
    ]

let node_value node =
  let open Zenbu_syntax.Syntax.Snapshot.Node in
  Extension_value.Record
    [
      ( "kind",
        Extension_value.Text (kind node |> Zenbu_syntax.Syntax.Kind.to_string)
      );
      ("start", Extension_value.Integer (start_offset node));
      ("stop", Extension_value.Integer (stop_offset node));
      ("named", Extension_value.Bool (is_named node));
      ("error", Extension_value.Bool (has_error node));
      ("missing", Extension_value.Bool (is_missing node));
      ( "parent",
        Option.value ~default:Extension_value.Nil
          (Option.map compact_node (parent_named node)) );
      ( "first_child",
        Option.value ~default:Extension_value.Nil
          (Option.map compact_node (first_named_child node)) );
      ( "children",
        Extension_value.List (List.map compact_node (named_children node)) );
      ( "next_sibling",
        Option.value ~default:Extension_value.Nil
          (Option.map compact_node (next_named_sibling node)) );
      ( "previous_sibling",
        Option.value ~default:Extension_value.Nil
          (Option.map compact_node (previous_named_sibling node)) );
    ]

let rec tree_value node =
  let open Zenbu_syntax.Syntax.Snapshot.Node in
  Extension_value.Record
    [
      ( "kind",
        Extension_value.Text (kind node |> Zenbu_syntax.Syntax.Kind.to_string)
      );
      ("start", Extension_value.Integer (start_offset node));
      ("stop", Extension_value.Integer (stop_offset node));
      ("named", Extension_value.Bool (is_named node));
      ("error", Extension_value.Bool (has_error node));
      ("missing", Extension_value.Bool (is_missing node));
      ( "children",
        Extension_value.List (List.map tree_value (named_children node)) );
    ]

let syntax_value context =
  match Editor_context.syntax context with
  | None -> Extension_value.Nil
  | Some snapshot ->
      let selections = Editor_context.selections context in
      let primary = List.nth selections.selections selections.primary_index in
      let start_offset = min primary.anchor_offset primary.head_offset in
      let stop_offset = max primary.anchor_offset primary.head_offset in
      let node =
        Zenbu_syntax.Syntax.Snapshot.smallest_named_containing snapshot
          ~start_offset ~stop_offset
        |> Option.map node_value
        |> Option.value ~default:Extension_value.Nil
      in
      Extension_value.Record
        [
          ( "language",
            Extension_value.Text
              (Zenbu_syntax.Syntax.Snapshot.language snapshot
              |> Zenbu_syntax.Syntax.Language.id) );
          ( "version",
            Extension_value.Integer
              (Zenbu_syntax.Syntax.Snapshot.document_version snapshot) );
          ( "has_error",
            Extension_value.Bool
              (Zenbu_syntax.Syntax.Snapshot.has_error snapshot) );
          ("node", node);
          ("tree", tree_value (Zenbu_syntax.Syntax.Snapshot.root snapshot));
        ]

let context_value ~granted context =
  let document =
    if has granted "document.read" then
      Extension_value.Record
        [
          ("id", Extension_value.Text (Editor_context.document_id context));
          ( "version",
            Extension_value.Integer (Editor_context.document_version context) );
          ( "length",
            Extension_value.Integer (Editor_context.byte_length context) );
          ("contents", Extension_value.Text (Editor_context.contents context));
        ]
    else Extension_value.Nil
  in
  let selections =
    if has granted "selection.read" then
      let values = Editor_context.selections context in
      Extension_value.List
        (List.map
           (fun selection ->
             let start =
               min selection.Editor_context.anchor_offset selection.head_offset
             in
             let stop =
               max selection.Editor_context.anchor_offset selection.head_offset
             in
             let text =
               String.sub (Editor_context.contents context) start (stop - start)
             in
             Extension_value.Record
               [
                 ( "anchor",
                   Extension_value.Integer
                     selection.Editor_context.anchor_offset );
                 ("head", Extension_value.Integer selection.head_offset);
                 ("start", Extension_value.Integer start);
                 ("stop", Extension_value.Integer stop);
                 ("text", Extension_value.Text text);
               ])
           values.selections)
    else Extension_value.Nil
  in
  let primary =
    if has granted "selection.read" then
      Extension_value.Integer (Editor_context.selections context).primary_index
    else Extension_value.Nil
  in
  let syntax =
    if has granted "syntax.read" then syntax_value context
    else Extension_value.Nil
  in
  Extension_value.Record
    [
      ("document", document);
      ("selections", selections);
      ("primary", primary);
      ("syntax", syntax);
    ]

let request (invocation : invocation) ~kind ~operation ~context ~arguments =
  {
    kind;
    operation;
    provider = invocation.provider;
    granted = invocation.granted;
    context = context_value ~granted:invocation.granted context;
    arguments;
  }

let invoke host invocation request = host.invoke invocation request
let deferred ~start = Deferred { start }
let call ~wakeup_fd ~closed ~take ~cancel = { wakeup_fd; closed; take; cancel }

let start = function
  | Immediate _ ->
      Error
        (Error.Invalid_provenance
           "attempted to start an immediate extension response")
  | Deferred deferred -> deferred.start ()

let immediate = function
  | Immediate value -> Ok value
  | Deferred _ ->
      Error
        (Error.Invalid_provenance
           "extension response is asynchronous in a synchronous callback")

let wakeup_fd call = call.wakeup_fd
let closed call = call.closed ()
let take call = call.take ()
let cancel call = call.cancel ()
let has_capability request capability = has request.granted capability

let require request ~capability =
  if has_capability request capability then Ok ()
  else
    Error
      (Error.Extension_error
         {
           code = Error.Capability_denied;
           plugin_id = Provider.plugin_id request.provider;
           provider = Some (Provider.id request.provider);
           operation = Some request.operation;
           required = Some capability;
           granted = request.granted;
           message = "extension host denied this service";
         })
