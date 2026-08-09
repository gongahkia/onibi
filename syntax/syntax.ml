open Zenbu_kernel

module Error = struct
  type t =
    | Unknown_language of string
    | Stale_document of {
        expected_id : string;
        expected_version : int;
        actual_id : string;
        actual_version : int;
      }
    | Invalid_edit of string
    | Backend_failure of string

  let to_string = function
    | Unknown_language id -> "unknown syntax language: " ^ id
    | Stale_document
        { expected_id; expected_version; actual_id; actual_version } ->
        Printf.sprintf "stale syntax document: expected %s@%d, got %s@%d"
          expected_id expected_version actual_id actual_version
    | Invalid_edit message -> "invalid syntax edit: " ^ message
    | Backend_failure message -> "syntax backend failure: " ^ message
end

module Language = struct
  type t = {
    id : string;
    display_name : string;
    extensions : string list;
    grammar : Tree_sitter_backend.grammar;
  }

  let ocaml =
    {
      id = "ocaml";
      display_name = "OCaml";
      extensions = [ ".ml"; ".mli" ];
      grammar = Tree_sitter_backend.Ocaml;
    }

  let json =
    {
      id = "json";
      display_name = "JSON";
      extensions = [ ".json" ];
      grammar = Tree_sitter_backend.Json;
    }

  let supported () = [ ocaml; json ]
  let id value = value.id
  let display_name value = value.display_name
  let extensions value = value.extensions

  let find id =
    List.find_opt (fun language -> String.equal language.id id) (supported ())

  let detect_path path =
    let extension = String.lowercase_ascii (Filename.extension path) in
    match extension with
    | ".mli" ->
        Some { ocaml with grammar = Tree_sitter_backend.Ocaml_interface }
    | _ ->
        List.find_opt
          (fun language -> List.mem extension language.extensions)
          (supported ())
end

module Kind = struct
  type t = string

  let of_string value = value
  let to_string value = value
  let equal = String.equal
end

module Snapshot = struct
  type snapshot = {
    document : Document_snapshot.t;
    language : Language.t;
    tree : Tree_sitter_backend.tree;
  }

  type t = snapshot

  module Node = struct
    type t = { snapshot : snapshot; node : Tree_sitter_backend.node }

    let document_id value =
      Document_id.to_string
        (Document_snapshot.document_id value.snapshot.document)

    let document_version value =
      Document_version.to_int
        (Document_snapshot.version value.snapshot.document)

    let kind value = Kind.of_string (Tree_sitter_backend.kind value.node)
    let is_named value = Tree_sitter_backend.is_named value.node
    let is_error value = Tree_sitter_backend.is_error value.node
    let is_missing value = Tree_sitter_backend.is_missing value.node
    let has_error value = Tree_sitter_backend.has_error value.node
    let start_offset value = Tree_sitter_backend.start_byte value.node
    let stop_offset value = Tree_sitter_backend.end_byte value.node

    let range value =
      match
        Document_snapshot.range value.snapshot.document
          ~start_offset:(start_offset value) ~stop_offset:(stop_offset value)
      with
      | Ok range -> Ok range
      | Error error ->
          Error (Error.Invalid_edit (Zenbu_kernel.Error.to_string error))

    let wrap snapshot = function
      | None -> None
      | Some node -> Some { snapshot; node }

    let rec parent_named value =
      match Tree_sitter_backend.parent value.node with
      | None -> None
      | Some parent when Tree_sitter_backend.is_named parent ->
          Some { snapshot = value.snapshot; node = parent }
      | Some parent -> parent_named { snapshot = value.snapshot; node = parent }

    let first_named_child value =
      wrap value.snapshot (Tree_sitter_backend.named_child value.node 0)

    let named_children value =
      List.init (Tree_sitter_backend.named_child_count value.node) (fun index ->
          Tree_sitter_backend.named_child value.node index)
      |> List.filter_map (fun node -> wrap value.snapshot node)

    let next_named_sibling value =
      wrap value.snapshot (Tree_sitter_backend.next_named_sibling value.node)

    let previous_named_sibling value =
      wrap value.snapshot
        (Tree_sitter_backend.previous_named_sibling value.node)
  end

  let document_id value =
    Document_id.to_string (Document_snapshot.document_id value.document)

  let document_version value =
    Document_version.to_int (Document_snapshot.version value.document)

  let language value = value.language

  let has_error value =
    Tree_sitter_backend.has_error (Tree_sitter_backend.root value.tree)

  let matches_document value document =
    Document_id.equal
      (Document_snapshot.document_id value.document)
      (Document_snapshot.document_id document)
    && Document_version.equal
         (Document_snapshot.version value.document)
         (Document_snapshot.version document)

  let root value =
    { Node.snapshot = value; node = Tree_sitter_backend.root value.tree }

  let smallest_named_containing value ~start_offset ~stop_offset =
    let root = Tree_sitter_backend.root value.tree in
    Tree_sitter_backend.named_descendant_for_byte_range root ~start:start_offset
      ~stop:stop_offset
    |> Option.map (fun node -> { Node.snapshot = value; node })
end

module Selector = struct
  type t =
    | Focus_primary
    | Containing
    | Parent
    | First_child
    | Next_sibling
    | Previous_sibling
    | Expand
    | Same_kind_siblings

  let current snapshot ~anchor_offset ~head_offset =
    let start_offset = min anchor_offset head_offset in
    let stop_offset = max anchor_offset head_offset in
    match
      Snapshot.smallest_named_containing snapshot ~start_offset ~stop_offset
    with
    | Some node -> Some node
    | None when start_offset = stop_offset ->
        Snapshot.smallest_named_containing snapshot ~start_offset
          ~stop_offset:
            (min (start_offset + 1)
               (Document_snapshot.byte_length snapshot.Snapshot.document))
    | None -> None

  let singleton = function None -> [] | Some node -> [ node ]

  let resolve snapshot ~anchor_offset ~head_offset = function
    | Focus_primary | Containing ->
        singleton (current snapshot ~anchor_offset ~head_offset)
    | Parent | Expand ->
        singleton
          (Option.bind
             (current snapshot ~anchor_offset ~head_offset)
             Snapshot.Node.parent_named)
    | First_child ->
        singleton
          (Option.bind
             (current snapshot ~anchor_offset ~head_offset)
             Snapshot.Node.first_named_child)
    | Next_sibling ->
        singleton
          (Option.bind
             (current snapshot ~anchor_offset ~head_offset)
             Snapshot.Node.next_named_sibling)
    | Previous_sibling ->
        singleton
          (Option.bind
             (current snapshot ~anchor_offset ~head_offset)
             Snapshot.Node.previous_named_sibling)
    | Same_kind_siblings -> (
        match current snapshot ~anchor_offset ~head_offset with
        | None -> []
        | Some node -> (
            match Snapshot.Node.parent_named node with
            | None -> [ node ]
            | Some parent ->
                let kind = Snapshot.Node.kind node in
                Snapshot.Node.named_children parent
                |> List.filter (fun sibling ->
                    Kind.equal kind (Snapshot.Node.kind sibling))))
end

module Service = struct
  type t = {
    language : Language.t;
    parser : Tree_sitter_backend.parser;
    mutable cached : Snapshot.t option;
  }

  let create language =
    {
      language;
      parser = Tree_sitter_backend.create_parser language.Language.grammar;
      cached = None;
    }

  let language value = value.language
  let cached value = value.cached

  let make_snapshot service document tree =
    { Snapshot.document; language = service.language; tree }

  let protect parse =
    try Ok (parse ())
    with exception_ ->
      Error (Error.Backend_failure (Printexc.to_string exception_))

  let parse_full service document =
    Tree_sitter_backend.reset service.parser;
    protect (fun () ->
        let tree =
          Tree_sitter_backend.parse service.parser
            (Document_snapshot.contents document)
        in
        let snapshot = make_snapshot service document tree in
        service.cached <- Some snapshot;
        snapshot)

  let refresh service document =
    match service.cached with
    | Some snapshot when Snapshot.matches_document snapshot document ->
        Ok snapshot
    | _ -> parse_full service document

  let point_at source offset =
    if offset < 0 || offset > String.length source then
      Error (Error.Invalid_edit "edit offset is outside the source text")
    else
      let row = ref 0 in
      let column = ref 0 in
      for index = 0 to offset - 1 do
        if Char.equal source.[index] '\n' then (
          incr row;
          column := 0)
        else incr column
      done;
      Ok Tree_sitter.{ row = !row; column = !column }

  let edit_of_kernel source edit =
    let range = Edit.range edit in
    let start_offset = Anchor.byte_offset (Range.start range) in
    let old_end_offset = Anchor.byte_offset (Range.stop range) in
    let new_end_offset = start_offset + String.length (Edit.text edit) in
    match (point_at source start_offset, point_at source old_end_offset) with
    | (Error _ as error), _ -> error
    | _, (Error _ as error) -> error
    | Ok start_point, Ok old_end_point ->
        let replacement_point =
          match String.rindex_opt (Edit.text edit) '\n' with
          | None ->
              Tree_sitter.
                {
                  row = start_point.row;
                  column = start_point.column + String.length (Edit.text edit);
                }
          | Some last_newline ->
              let rows =
                String.fold_left
                  (fun count character ->
                    if Char.equal character '\n' then count + 1 else count)
                  0 (Edit.text edit)
              in
              Tree_sitter.
                {
                  row = start_point.row + rows;
                  column = String.length (Edit.text edit) - last_newline - 1;
                }
        in
        Ok
          Tree_sitter_backend.
            {
              start_byte = start_offset;
              old_end_byte = old_end_offset;
              new_end_byte = new_end_offset;
              start_point;
              old_end_point;
              new_end_point = replacement_point;
            }

  let validate_transaction before transaction after =
    let expected_id =
      Document_snapshot.document_id before |> Document_id.to_string
    in
    let expected_version =
      Document_snapshot.version before |> Document_version.to_int
    in
    let actual_id =
      Transaction.document_id transaction |> Document_id.to_string
    in
    let actual_version =
      Transaction.source_version transaction |> Document_version.to_int
    in
    if
      not
        (String.equal expected_id actual_id && expected_version = actual_version)
    then
      Error
        (Error.Stale_document
           { expected_id; expected_version; actual_id; actual_version })
    else if
      not
        (Document_id.equal
           (Document_snapshot.document_id before)
           (Document_snapshot.document_id after))
    then
      Error (Error.Invalid_edit "a transaction cannot change document identity")
    else Ok ()

  let update service ~before ~transaction ~after =
    match validate_transaction before transaction after with
    | Error _ as error -> error
    | Ok () -> (
        match service.cached with
        | Some previous when Snapshot.matches_document previous before ->
            let source = Document_snapshot.contents before in
            let edits = Transaction.edits transaction in
            let rec convert values = function
              | [] -> Ok (List.rev values)
              | edit :: rest -> (
                  match edit_of_kernel source edit with
                  | Error _ as error -> error
                  | Ok edit -> convert (edit :: values) rest)
            in
            Result.bind (convert [] edits) (fun edits ->
                let edits = List.rev edits in
                protect (fun () ->
                    let tree =
                      if edits = [] then
                        Tree_sitter_backend.copy_tree previous.Snapshot.tree
                      else
                        Tree_sitter_backend.parse_incremental service.parser
                          ~old:previous.Snapshot.tree ~edits
                          (Document_snapshot.contents after)
                    in
                    let snapshot = make_snapshot service after tree in
                    service.cached <- Some snapshot;
                    snapshot))
        | _ -> parse_full service after)
end
