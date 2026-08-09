open Zenbu_kernel

type selection = { anchor_offset : int; head_offset : int }
type selection_set = { selections : selection list; primary_index : int }

type t = {
  document_id : string;
  document_version : int;
  contents : string;
  byte_length : int;
  selections : selection_set;
  commands : Command_descriptor.t list;
}

let from_snapshot ~snapshot ~commands =
  let selections =
    List.map
      (fun selection ->
        {
          anchor_offset = Anchor.byte_offset (Selection.anchor selection);
          head_offset = Anchor.byte_offset (Selection.head selection);
        })
      (Selection_set.to_list (Document_snapshot.selections snapshot))
  in
  {
    document_id = Document_id.to_string (Document_snapshot.document_id snapshot);
    document_version =
      Document_version.to_int (Document_snapshot.version snapshot);
    contents = Document_snapshot.contents snapshot;
    byte_length = Document_snapshot.byte_length snapshot;
    selections =
      {
        selections;
        primary_index =
          Selection_set.primary_index (Document_snapshot.selections snapshot);
      };
    commands;
  }

let document_id value = value.document_id
let document_version value = value.document_version
let contents value = value.contents
let byte_length value = value.byte_length
let selections value = value.selections
let command_descriptors value = value.commands
