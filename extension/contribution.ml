type t = Commands | Selectors | Transformations | Bindings | Events

let all = [ Commands; Selectors; Transformations; Bindings; Events ]

let id = function
  | Commands -> "commands"
  | Selectors -> "selectors"
  | Transformations -> "transformations"
  | Bindings -> "bindings"
  | Events -> "events"

let description = function
  | Commands -> "semantic command registrations"
  | Selectors -> "dynamic selector registrations"
  | Transformations -> "dynamic transformation registrations"
  | Bindings -> "scoped logical input bindings"
  | Events -> "document-changed and after-save event handlers"

let of_id value =
  match List.find_opt (fun contribution -> String.equal value (id contribution)) all with
  | Some contribution -> Ok contribution
  | None ->
      Error
        (Zenbu_kernel.Error.Extension_error
           {
             code = Zenbu_kernel.Error.Unknown_contribution;
             plugin_id = None;
             provider = None;
             operation = Some value;
             required = None;
             granted = [];
             message = "the manifest declares an unsupported contribution class";
           })
