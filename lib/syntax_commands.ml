open Zenbu_kernel

type operation =
  | Focus_primary
  | Parent
  | First_child
  | Next_sibling
  | Previous_sibling
  | Expand
  | Same_kind_siblings

let selector = function
  | Focus_primary -> Zenbu_syntax.Syntax.Selector.Focus_primary
  | Parent -> Zenbu_syntax.Syntax.Selector.Parent
  | First_child -> Zenbu_syntax.Syntax.Selector.First_child
  | Next_sibling -> Zenbu_syntax.Syntax.Selector.Next_sibling
  | Previous_sibling -> Zenbu_syntax.Syntax.Selector.Previous_sibling
  | Expand -> Zenbu_syntax.Syntax.Selector.Expand
  | Same_kind_siblings -> Zenbu_syntax.Syntax.Selector.Same_kind_siblings

let id = function
  | Focus_primary -> "syntax.focus"
  | Parent -> "syntax.parent"
  | First_child -> "syntax.child"
  | Next_sibling -> "syntax.next-sibling"
  | Previous_sibling -> "syntax.previous-sibling"
  | Expand -> "syntax.expand"
  | Same_kind_siblings -> "syntax.select-same-kind"

let title = function
  | Focus_primary -> "Focus syntax node"
  | Parent -> "Select parent syntax node"
  | First_child -> "Select first syntax child"
  | Next_sibling -> "Select next syntax sibling"
  | Previous_sibling -> "Select previous syntax sibling"
  | Expand -> "Expand to containing syntax node"
  | Same_kind_siblings -> "Select same-kind syntax siblings"

let static = function
  | Ok value -> value
  | Error error -> failwith (Error.to_string error)

let provider = static (Provider.create ~id:"zenbu.syntax" ~kind:Provider.Syntax)
let command_id operation = static (Command_id.of_string (id operation))
let query_command_id = static (Command_id.of_string "syntax.query.select")

let primary_selection context =
  let selections = Editor_context.selections context in
  List.nth selections.Editor_context.selections selections.primary_index

let selection_intent ranges =
  let selections =
    List.map
      (fun range ->
        ( Anchor.byte_offset (Range.start range),
          Anchor.byte_offset (Range.stop range) ))
      ranges
  in
  match selections with
  | [] -> Ok []
  | _ ->
      Model_intent.set_selections ~selections ~primary:0
      |> Result.map (fun intent -> [ intent ])

let selection_intent_nodes nodes =
  let ranges =
    List.map
      (fun node ->
        match Zenbu_syntax.Syntax.Snapshot.Node.range node with
        | Ok range -> range
        | Error error ->
            failwith
              ("syntax invariant violated while converting node range: "
              ^ Zenbu_syntax.Syntax.Error.to_string error))
      nodes
  in
  selection_intent ranges

let resolve context operation =
  match Editor_context.syntax context with
  | None -> Error (Error.Invalid_selector "syntax is unavailable")
  | Some syntax ->
      let selection = primary_selection context in
      Zenbu_syntax.Syntax.Selector.resolve syntax
        ~anchor_offset:selection.Editor_context.anchor_offset
        ~head_offset:selection.Editor_context.head_offset (selector operation)
      |> selection_intent_nodes

let query_error error =
  Error.Invalid_selector
    ("syntax query: " ^ Zenbu_syntax.Syntax.Query.error_to_string error)

let resolve_query context ~source ~capture =
  match Editor_context.syntax context with
  | None -> Error (Error.Invalid_selector "syntax is unavailable")
  | Some syntax -> (
      match Zenbu_syntax.Syntax.Query.compile syntax ~source with
      | Error error -> Error (query_error error)
      | Ok query -> (
          match
            Zenbu_syntax.Syntax.Query.selections query ~snapshot:syntax ~capture
            |> Result.map_error query_error
          with
          | Error _ as error -> error
          | Ok selections ->
              Selection_set.to_list selections
              |> List.map Selection.range |> selection_intent))

let invocation operation =
  Command_invocation.create ~id:(command_id operation) ~arguments:[]

let query_invocation ~source ~capture =
  let argument name text =
    Command_argument.make ~name ~value:(Command_argument.Text text)
  in
  match (argument "query" source, argument "capture" capture) with
  | Ok query, Ok capture ->
      Command_invocation.create ~id:query_command_id
        ~arguments:[ query; capture ]
  | Error error, _ | _, Error error -> Error error

let command operation =
  let descriptor =
    static
      (Command_descriptor.create ~id:(command_id operation)
         ~title:(title operation)
         ~description:"Resolve an abstract, version-matched syntax selection."
         ~category:"syntax" ~provider ())
  in
  Command.create ~descriptor ~handler:(fun context _ ->
      resolve context operation)

let required_text_argument invocation name =
  match Command_invocation.find invocation ~name with
  | Ok (Command_argument.Text value) -> Ok value
  | Ok _ ->
      Error
        (Error.Invalid_command_arguments
           ("syntax query argument " ^ name ^ " must be text"))
  | Error _ as error -> error

let query_command () =
  let parameters =
    [
      Command_descriptor.
        {
          name = "query";
          description =
            "UTF-8 Tree-sitter query, bounded to the current syntax snapshot.";
          required = true;
          kind = Text;
        };
      {
        name = "capture";
        description = "Capture name, without @, to turn into selections.";
        required = true;
        kind = Text;
      };
    ]
  in
  let descriptor =
    static
      (Command_descriptor.create ~id:query_command_id
         ~title:"Select syntax query captures"
         ~description:
           "Run a bounded query and convert one named capture into ordinary \
            selections."
         ~category:"syntax" ~parameters ~provider ())
  in
  Command.create ~descriptor ~handler:(fun context invocation ->
      match
        ( required_text_argument invocation "query",
          required_text_argument invocation "capture" )
      with
      | Ok source, Ok capture -> resolve_query context ~source ~capture
      | Error error, _ | _, Error error -> Error error)

let commands () =
  [
    Focus_primary;
    Parent;
    First_child;
    Next_sibling;
    Previous_sibling;
    Expand;
    Same_kind_siblings;
  ]
  |> List.map command
  |> fun commands -> commands @ [ query_command () ]
