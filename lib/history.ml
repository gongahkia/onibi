module Int_map = Map.Make (Int)

type change = {
  id : int;
  transaction : Transaction.t;
  before : Document.t;
  after : Document.t;
}

type node = {
  id : int;
  parent : int option;
  children : int list;
  change : change option;
  document : Document.t;
}

type t = {
  nodes : node Int_map.t;
  root_id : int;
  current_id : int;
  next_change_id : int;
  next_version : int;
}

let find_node history id =
  match Int_map.find_opt id history.nodes with
  | Some node -> Ok node
  | None -> Error (Error.Unknown_history_node id)

let create document =
  let root =
    { id = 0; parent = None; children = []; change = None; document }
  in
  {
    nodes = Int_map.singleton root.id root;
    root_id = root.id;
    current_id = root.id;
    next_change_id = 1;
    next_version = Document_version.to_int (Document.version document) + 1;
  }

let current history =
  match find_node history history.current_id with
  | Ok node -> node.document
  | Error error ->
      failwith ("history invariant violated: " ^ Error.to_string error)

let commit history transaction =
  match find_node history history.current_id with
  | Error _ as error -> error
  | Ok parent -> (
      match Document_version.of_int history.next_version with
      | Error _ as error -> error
      | Ok result_version -> (
          match Document.apply ~result_version parent.document transaction with
          | Error _ as error -> error
          | Ok after ->
              let id = history.next_change_id in
              let change =
                { id; transaction; before = parent.document; after }
              in
              let node =
                {
                  id;
                  parent = Some parent.id;
                  children = [];
                  change = Some change;
                  document = after;
                }
              in
              let updated_parent =
                { parent with children = parent.children @ [ id ] }
              in
              Ok
                {
                  nodes =
                    history.nodes
                    |> Int_map.add parent.id updated_parent
                    |> Int_map.add id node;
                  root_id = history.root_id;
                  current_id = id;
                  next_change_id = id + 1;
                  next_version = history.next_version + 1;
                }))

let apply_intent ~source ?description ?provenance history intent =
  match
    Intent.resolve ~source ?description ?provenance
      (Document.snapshot (current history))
      intent
  with
  | Error _ as error -> error
  | Ok transaction -> commit history transaction

let undo history =
  match find_node history history.current_id with
  | Error _ as error -> error
  | Ok { parent = None; _ } -> Error Error.History_at_root
  | Ok { parent = Some parent_id; _ } ->
      if Int_map.mem parent_id history.nodes then
        Ok { history with current_id = parent_id }
      else Error (Error.Unknown_history_node parent_id)

let redo ?change_id history =
  match find_node history history.current_id with
  | Error _ as error -> error
  | Ok parent -> (
      let selected_child =
        match change_id with
        | Some id -> if List.mem id parent.children then Some id else None
        | None -> (
            match List.rev parent.children with
            | [] -> None
            | child :: _ -> Some child)
      in
      match selected_child with
      | None -> Error Error.History_no_redo
      | Some id ->
          if Int_map.mem id history.nodes then
            Ok { history with current_id = id }
          else Error (Error.Unknown_history_node id))

let current_change history =
  match find_node history history.current_id with
  | Ok node -> node.change
  | Error error ->
      failwith ("history invariant violated: " ^ Error.to_string error)

let lineage history =
  let rec collect id changes =
    match find_node history id with
    | Error error ->
        failwith ("history invariant violated: " ^ Error.to_string error)
    | Ok { parent = None; _ } -> changes
    | Ok { parent = Some parent; change = Some change; _ } ->
        collect parent (change :: changes)
    | Ok { parent = Some _; change = None; _ } ->
        failwith "history invariant violated: child without change"
  in
  collect history.current_id []

let change_id (value : change) = value.id
let transaction value = value.transaction
let before value = value.before
let after value = value.after

type node_view = {
  view_id : int;
  view_parent : int option;
  view_children : int list;
  view_change : change option;
}

let nodes history =
  history.nodes |> Int_map.bindings
  |> List.map (fun (_, node) ->
         {
           view_id = node.id;
           view_parent = node.parent;
           view_children = node.children;
           view_change = node.change;
         })

let root_id history = history.root_id
let current_id history = history.current_id
let node_id value = value.view_id
let parent_id value = value.view_parent
let child_ids value = value.view_children
let node_change value = value.view_change
let is_current history value = value.view_id = history.current_id
