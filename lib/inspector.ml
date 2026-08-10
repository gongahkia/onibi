open Zenbu_kernel

type description = {
  kind : string;
  id : string;
  title : string;
  summary : string option;
  provider : Provider.t;
  fields : (string * string) list;
}

type selection = {
  primary : bool;
  anchor_offset : int;
  head_offset : int;
  start_offset : int;
  stop_offset : int;
}

type edit_preview = {
  start_offset : int;
  stop_offset : int;
  replacement : string;
  removed : string;
}

type change = {
  id : int;
  source_version : int;
  result_version : int;
  provenance : Provenance.t option;
  edits : edit_preview list;
}

type history_node = {
  id : int;
  parent_id : int option;
  child_ids : int list;
  change : change option;
  current : bool;
  saved : bool;
}

type history = { current_id : int; nodes : history_node list }

type syntax_node = {
  kind : string;
  start_offset : int;
  stop_offset : int;
  parent_kind : string option;
  child_count : int;
  named : bool;
  error : bool;
}

type syntax = {
  language_id : string;
  document_version : int;
  has_error : bool;
  node : syntax_node option;
}

type syntax_service = {
  language_id : string;
  cached_version : int option;
  last_strategy : string option;
}

type why = { execution_id : int; events : Trace_event.t list }

type api = {
  models : description list;
  commands : description list;
  selectors : description list;
  transformations : description list;
  languages : Zenbu_syntax.Syntax.Language.t list;
}

let provider_fields provider =
  [
    ("provider", Provider.id provider);
    ("provider-kind", Provider.kind provider |> Provider.kind_name);
  ]

let describe_command descriptor =
  {
    kind = "command";
    id = Command_descriptor.id descriptor |> Command_id.to_string;
    title = Command_descriptor.title descriptor;
    summary = Command_descriptor.description descriptor;
    provider = Command_descriptor.provider descriptor;
    fields =
      provider_fields (Command_descriptor.provider descriptor)
      @ Option.to_list
          (Option.map (fun category -> ("category", category))
             (Command_descriptor.category descriptor));
  }

let describe_model descriptor =
  {
    kind = "model";
    id = Editing_model.id descriptor;
    title = Editing_model.title descriptor;
    summary = Editing_model.description descriptor;
    provider = Editing_model.provider descriptor;
    fields = provider_fields (Editing_model.provider descriptor);
  }

let describe_semantic descriptor =
  {
    kind =
      (match Semantic_descriptor.kind descriptor with
      | Semantic_descriptor.Selector -> "selector"
      | Semantic_descriptor.Transformation -> "transformation");
    id = Semantic_descriptor.id descriptor;
    title = Semantic_descriptor.title descriptor;
    summary = Some (Semantic_descriptor.description descriptor);
    provider = Semantic_descriptor.provider descriptor;
    fields =
      provider_fields (Semantic_descriptor.provider descriptor)
      @ [
          ( "requires-syntax",
            string_of_bool (Semantic_descriptor.requires_syntax descriptor) );
        ];
  }

let description_id (value : description) = value.id
let description_kind (value : description) = value.kind
let description_title (value : description) = value.title
let description_summary (value : description) = value.summary
let description_provider (value : description) = value.provider
let description_fields (value : description) = value.fields

let commands registry = Command_registry.descriptors registry |> List.map describe_command

let find_command registry id =
  match Command_id.of_string id with
  | Error _ -> None
  | Ok id -> (
      match Command_registry.find registry id with
      | Error _ -> None
      | Ok command -> Some (Command.descriptor command |> describe_command))

let semantic_registry () =
  let values =
    Selector.descriptors () @ Transformation.descriptors ()
    @ Zenbu_syntax.Syntax.Selector.descriptors ()
  in
  List.fold_left
    (fun registry descriptor ->
      match registry with
      | Error _ as error -> error
      | Ok registry -> Semantic_registry.register registry descriptor)
    (Ok Semantic_registry.empty) values
  |> Result.get_ok

let find_semantic registry id =
  Semantic_registry.find registry id |> Option.map describe_semantic

let selections context =
  let selection_set = Editor_context.selections context in
  List.mapi
    (fun index selection ->
      let anchor_offset = selection.Editor_context.anchor_offset in
      let head_offset = selection.Editor_context.head_offset in
      {
        primary = index = selection_set.primary_index;
        anchor_offset;
        head_offset;
        start_offset = min anchor_offset head_offset;
        stop_offset = max anchor_offset head_offset;
      })
    selection_set.selections

let selection_primary (value : selection) = value.primary
let selection_anchor_offset (value : selection) = value.anchor_offset
let selection_head_offset (value : selection) = value.head_offset
let selection_start_offset (value : selection) = value.start_offset
let selection_stop_offset (value : selection) = value.stop_offset

let preview text =
  let limit = 80 in
  if String.length text <= limit then text
  else String.sub text 0 limit ^ "…"

let change change =
  let before = Document.snapshot (History.before change) in
  let contents = Document_snapshot.contents before in
  let edits =
    History.transaction change |> Transaction.edits
    |> List.map (fun edit ->
           let range = Edit.range edit in
           let start_offset = Anchor.byte_offset (Range.start range) in
           let stop_offset = Anchor.byte_offset (Range.stop range) in
           {
             start_offset;
             stop_offset;
             replacement = Edit.text edit |> preview;
             removed = String.sub contents start_offset (stop_offset - start_offset) |> preview;
           })
  in
  {
    id = History.change_id change;
    source_version = Document.version (History.before change) |> Document_version.to_int;
    result_version = Document.version (History.after change) |> Document_version.to_int;
    provenance = History.transaction change |> Transaction.metadata_of |> Transaction.provenance;
    edits;
  }

let change_id (value : change) = value.id
let change_source_version (value : change) = value.source_version
let change_result_version (value : change) = value.result_version
let change_edit_count (value : change) = List.length value.edits
let change_provenance (value : change) = value.provenance
let change_edits (value : change) = value.edits
let edit_start_offset (value : edit_preview) = value.start_offset
let edit_stop_offset (value : edit_preview) = value.stop_offset
let edit_replacement (value : edit_preview) = value.replacement
let edit_removed (value : edit_preview) = value.removed

let history ?saved_version source =
  let nodes =
    History.nodes source
    |> List.map (fun node ->
           let change = History.node_change node |> Option.map change in
           let saved =
             match (saved_version, change) with
             | Some version, Some change -> change.result_version = version
             | None, _ | _, None -> false
           in
           {
             id = History.node_id node;
             parent_id = History.parent_id node;
             child_ids = History.child_ids node;
             change;
             current = History.is_current source node;
             saved;
           })
  in
  { current_id = History.current_id source; nodes }

let history_nodes (value : history) = value.nodes
let history_current_id (value : history) = value.current_id
let history_node_id (value : history_node) = value.id
let history_node_parent_id (value : history_node) = value.parent_id
let history_node_child_ids (value : history_node) = value.child_ids
let history_node_change (value : history_node) = value.change
let history_node_current (value : history_node) = value.current
let history_node_saved (value : history_node) = value.saved

let find_change history id =
  history.nodes |> List.find_map (fun node ->
      match node.change with Some change when change.id = id -> Some change | _ -> None)

let syntax context =
  match Editor_context.syntax context with
  | None -> None
  | Some snapshot ->
      let primary = List.find selection_primary (selections context) in
      let node =
        Zenbu_syntax.Syntax.Snapshot.smallest_named_containing snapshot
          ~start_offset:primary.start_offset ~stop_offset:primary.stop_offset
        |> Option.map (fun node ->
               {
                 kind =
                   Zenbu_syntax.Syntax.Snapshot.Node.kind node
                   |> Zenbu_syntax.Syntax.Kind.to_string;
                 start_offset = Zenbu_syntax.Syntax.Snapshot.Node.start_offset node;
                 stop_offset = Zenbu_syntax.Syntax.Snapshot.Node.stop_offset node;
                 parent_kind =
                   Zenbu_syntax.Syntax.Snapshot.Node.parent_named node
                   |> Option.map (fun parent ->
                          Zenbu_syntax.Syntax.Snapshot.Node.kind parent
                          |> Zenbu_syntax.Syntax.Kind.to_string);
                 child_count =
                   Zenbu_syntax.Syntax.Snapshot.Node.named_children node
                   |> List.length;
                 named = Zenbu_syntax.Syntax.Snapshot.Node.is_named node;
                 error = Zenbu_syntax.Syntax.Snapshot.Node.has_error node;
               })
      in
      Some
        {
          language_id =
            Zenbu_syntax.Syntax.Snapshot.language snapshot
            |> Zenbu_syntax.Syntax.Language.id;
          document_version = Zenbu_syntax.Syntax.Snapshot.document_version snapshot;
          has_error = Zenbu_syntax.Syntax.Snapshot.has_error snapshot;
          node;
        }

let syntax_language_id (value : syntax) = value.language_id
let syntax_document_version (value : syntax) = value.document_version
let syntax_has_error (value : syntax) = value.has_error
let syntax_node (value : syntax) = value.node
let syntax_node_kind (value : syntax_node) = value.kind
let syntax_node_start_offset (value : syntax_node) = value.start_offset
let syntax_node_stop_offset (value : syntax_node) = value.stop_offset
let syntax_node_parent_kind (value : syntax_node) = value.parent_kind
let syntax_node_child_count (value : syntax_node) = value.child_count
let syntax_node_named (value : syntax_node) = value.named
let syntax_node_error (value : syntax_node) = value.error

let syntax_service service =
  let status = Zenbu_syntax.Syntax.Service.status service in
  {
    language_id =
      Zenbu_syntax.Syntax.Service.status_language status
      |> Zenbu_syntax.Syntax.Language.id;
    cached_version = Zenbu_syntax.Syntax.Service.status_cached_version status;
    last_strategy =
      Zenbu_syntax.Syntax.Service.status_last_strategy status
      |> Option.map Zenbu_syntax.Syntax.Service.strategy_to_string;
  }

let syntax_service_language_id (value : syntax_service) = value.language_id
let syntax_service_cached_version (value : syntax_service) = value.cached_version
let syntax_service_last_strategy (value : syntax_service) = value.last_strategy

let why trace ~execution_id =
  let events =
    Trace.events trace
    |> List.filter (fun event -> Trace_event.execution_id event = execution_id)
  in
  if events = [] then None else Some { execution_id; events }

let why_execution_id (value : why) = value.execution_id
let why_events (value : why) = value.events

let latest_why trace =
  match List.rev (Trace.events trace) with
  | [] -> None
  | event :: _ -> why trace ~execution_id:(Trace_event.execution_id event)

let api ~models ~commands:registry =
  let semantic = semantic_registry () |> Semantic_registry.descriptors in
  {
    models =
      List.map describe_model models
      |> List.sort (fun (a : description) (b : description) ->
             String.compare a.id b.id);
    commands = commands registry;
    selectors =
      semantic
      |> List.filter (fun descriptor -> Semantic_descriptor.kind descriptor = Semantic_descriptor.Selector)
      |> List.map describe_semantic;
    transformations =
      semantic
      |> List.filter (fun descriptor -> Semantic_descriptor.kind descriptor = Semantic_descriptor.Transformation)
      |> List.map describe_semantic;
    languages = Zenbu_syntax.Syntax.Language.supported ();
  }

let api_models value = value.models
let api_commands value = value.commands
let api_selectors value = value.selectors
let api_transformations value = value.transformations
let api_languages value = value.languages

let format_description (value : description) =
  [ value.kind ^ ": " ^ value.id; "title: " ^ value.title ]
  @ Option.to_list (Option.map (fun summary -> "description: " ^ summary) value.summary)
  @ List.map (fun (name, content) -> name ^ ": " ^ content) value.fields

let format_commands values =
  values
  |> List.concat_map (fun (value : description) ->
         (value.id ^ " — " ^ value.title)
         :: Option.to_list (Option.map (fun summary -> "  " ^ summary) value.summary))

let format_bindings model status rules =
  (Editing_model.title model ^ " / " ^ Model_status.label status)
  :: List.map
       (fun rule ->
         Printf.sprintf "%-16s %s" (Input_rule.pattern rule |> Input_rule.pattern_to_string)
           (Input_rule.summary rule))
       rules

let format_selection values =
  List.map
    (fun value ->
      Printf.sprintf "%s anchor=%d head=%d range=%d:%d"
        (if value.primary then "primary" else "secondary") value.anchor_offset
        value.head_offset value.start_offset value.stop_offset)
    values

let format_provenance provenance =
  Provenance.entries provenance
  |> List.map Provenance.entry_name |> String.concat " -> "

let format_change (value : change) =
  (Printf.sprintf "change %d: v%d -> v%d (%d edit%s)" value.id
     value.source_version value.result_version (List.length value.edits)
     (if List.length value.edits = 1 then "" else "s"))
  :: Option.to_list
       (Option.map (fun provenance -> "provenance: " ^ format_provenance provenance) value.provenance)
  @ List.map
      (fun (edit : edit_preview) ->
        Printf.sprintf "edit %d:%d removed=%S replacement=%S" edit.start_offset
          edit.stop_offset edit.removed edit.replacement)
      value.edits

let format_history (value : history) =
  value.nodes
  |> List.map (fun (node : history_node) ->
         let marker = if node.current then "*" else " " in
         let saved = if node.saved then " saved" else "" in
         match node.change with
         | None -> marker ^ " root"
         | Some change ->
             Printf.sprintf "%s %d v%d -> v%d%s children=%s" marker change.id
               change.source_version change.result_version saved
               (node.child_ids |> List.map string_of_int |> String.concat ","))

let format_syntax (value : syntax) =
  [
    "language: " ^ value.language_id;
    "document-version: " ^ string_of_int value.document_version;
    "has-error: " ^ string_of_bool value.has_error;
  ]
  @
  match value.node with
  | None -> ["node: none"]
  | Some node ->
      [
        "node: " ^ node.kind;
        Printf.sprintf "range: %d:%d" node.start_offset node.stop_offset;
        "parent: " ^ Option.value node.parent_kind ~default:"none";
        "children: " ^ string_of_int node.child_count;
      ]

let format_syntax_service (value : syntax_service) =
  [
    "language: " ^ value.language_id;
    "cached-version: "
    ^ Option.value (Option.map string_of_int value.cached_version) ~default:"none";
    "last-strategy: " ^ Option.value value.last_strategy ~default:"none";
  ]

let format_event = function
  | Trace_event.Input_received { input; _ } -> "input: " ^ input
  | Model_before { model_id; status_label; _ } ->
      "model: " ^ model_id ^ " / " ^ status_label
  | Model_transition { previous_status; next_status; _ } ->
      "transition: " ^ previous_status ^ " -> " ^ next_status
  | Model_effect { effect_id; _ } -> "effect: " ^ effect_id
  | Command_invoked { command_id; _ } -> "command: " ^ command_id
  | Selector_resolved { selector_id; selection_count; _ } ->
      Printf.sprintf "selector: %s (%d selections)" selector_id selection_count
  | Transformation_applied { transformation_id; _ } ->
      "transformation: " ^ transformation_id
  | Transaction_created { source_version; edit_count; _ } ->
      Printf.sprintf "transaction: v%d (%d edits)" source_version edit_count
  | Transaction_committed { change_id; source_version; result_version; edit_count; provenance; _ } ->
      Printf.sprintf "committed: #%d v%d -> v%d (%d edits)\nprovenance: %s"
        change_id source_version result_version edit_count (format_provenance provenance)
  | Transaction_rejected { reason; _ } -> "transaction rejected: " ^ reason
  | History_changed { operation; current_change; _ } ->
      "history: " ^ operation ^ " -> "
      ^ Option.value (Option.map string_of_int current_change) ~default:"root"
  | Syntax_refreshed { language_id; document_version; strategy; has_error; _ } ->
      Printf.sprintf "syntax: %s v%d %s error=%b" language_id document_version
        strategy has_error
  | Error_reported { reason; _ } -> "error: " ^ reason

let format_why (value : why) =
  ("execution: " ^ string_of_int value.execution_id)
  :: List.map format_event value.events

let format_profile profiler =
  let values = Profiler.aggregates profiler in
  if values = [] then ["profile: no samples"]
  else
    List.map
      (fun value ->
        let model =
          Option.map (fun id -> " [" ^ id ^ "]")
            (Profiler.aggregate_model_id value)
          |> Option.value ~default:""
        in
        Printf.sprintf "%s%s %d calls mean %.3fms max %.3fms"
          (Profiler.aggregate_stage value |> Profiler.stage_name) model
          (Profiler.aggregate_count value)
          (Profiler.aggregate_mean_seconds value *. 1000.)
          (Profiler.aggregate_max_seconds value *. 1000.))
      values

let format_api (value : api) =
  [
    Printf.sprintf "models: %d" (List.length value.models);
    Printf.sprintf "commands: %d" (List.length value.commands);
    Printf.sprintf "selectors: %d" (List.length value.selectors);
    Printf.sprintf "transformations: %d" (List.length value.transformations);
    "languages: "
    ^ (value.languages
      |> List.map Zenbu_syntax.Syntax.Language.id |> String.concat ", ");
  ]
