type t =
  | Document_read
  | Document_edit
  | Selection_read
  | Selection_write
  | Syntax_read
  | Command_invoke
  | Ui_message
  | Event_subscribe

let all =
  [
    Document_read;
    Document_edit;
    Selection_read;
    Selection_write;
    Syntax_read;
    Command_invoke;
    Ui_message;
    Event_subscribe;
  ]

let id = function
  | Document_read -> "document.read"
  | Document_edit -> "document.edit"
  | Selection_read -> "selection.read"
  | Selection_write -> "selection.write"
  | Syntax_read -> "syntax.read"
  | Command_invoke -> "command.invoke"
  | Ui_message -> "ui.message"
  | Event_subscribe -> "event.subscribe"

let description = function
  | Document_read -> "inspect copied document metadata and UTF-8 text"
  | Document_edit -> "return declarative text edits for Zenbu validation"
  | Selection_read -> "inspect copied current selections"
  | Selection_write -> "return declarative selection changes"
  | Syntax_read -> "inspect Zenbu-owned data-only syntax summaries"
  | Command_invoke -> "invoke a registered semantic command by stable ID"
  | Ui_message -> "emit a user-facing informational message"
  | Event_subscribe -> "register document-changed or after-save handlers"

let of_id value =
  match
    List.find_opt (fun capability -> String.equal value (id capability)) all
  with
  | Some capability -> Ok capability
  | None ->
      Error
        (Zenbu_kernel.Error.Extension_error
           {
             code = Zenbu_kernel.Error.Unknown_capability;
             plugin_id = None;
             provider = None;
             operation = Some value;
             required = None;
             granted = [];
             message =
               "the manifest requests an unsupported extension capability";
           })
