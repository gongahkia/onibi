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
  clipboard : Clipboard.t;
  macro_recording_register : string option;
  syntax : Zenbu_syntax.Syntax.Snapshot.t option;
}

let from_snapshot ~snapshot ~commands ?(clipboard = Clipboard.empty)
    ?macro_recording_register ?syntax () =
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
    clipboard;
    macro_recording_register;
    syntax =
      (match syntax with
      | Some syntax
        when Zenbu_syntax.Syntax.Snapshot.matches_document syntax snapshot ->
          Some syntax
      | Some _ | None -> None);
  }

let document_id value = value.document_id
let document_version value = value.document_version
let contents value = value.contents
let byte_length value = value.byte_length
let selections value = value.selections
let with_selections value selections = { value with selections }
let command_descriptors value = value.commands
let clipboard_entry value ~slot = Clipboard.find value.clipboard ~slot
let macro_recording_register value = value.macro_recording_register
let syntax value = value.syntax
