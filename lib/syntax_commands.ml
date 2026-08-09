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

let command_id operation = static (Command_id.of_string (id operation))

let primary_selection context =
  let selections = Editor_context.selections context in
  List.nth selections.Editor_context.selections selections.primary_index

let selection_intent nodes =
  let selections =
    List.map
      (fun node ->
        let range =
          match Zenbu_syntax.Syntax.Snapshot.Node.range node with
          | Ok range -> range
          | Error error ->
              failwith
                ("syntax invariant violated while converting node range: "
               ^ Zenbu_syntax.Syntax.Error.to_string error)
        in
        ( Anchor.byte_offset (Range.start range),
          Anchor.byte_offset (Range.stop range) ))
      nodes
  in
  match selections with
  | [] -> Ok []
  | _ ->
      Model_intent.set_selections ~selections ~primary:0
      |> Result.map (fun intent -> [ intent ])

let resolve context operation =
  match Editor_context.syntax context with
  | None -> Error (Error.Invalid_selector "syntax is unavailable")
  | Some syntax ->
      let selection = primary_selection context in
      Zenbu_syntax.Syntax.Selector.resolve syntax
        ~anchor_offset:selection.Editor_context.anchor_offset
        ~head_offset:selection.Editor_context.head_offset
        (selector operation)
      |> selection_intent

let invocation operation =
  Command_invocation.create ~id:(command_id operation) ~arguments:[]

let command operation =
  let descriptor =
    static
      (Command_descriptor.create ~id:(command_id operation)
         ~title:(title operation)
         ~description:"Resolve an abstract, version-matched syntax selection."
         ~category:"syntax" ())
  in
  Command.create ~descriptor ~handler:(fun context _ -> resolve context operation)

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
