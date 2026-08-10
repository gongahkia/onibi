open Zenbu_kernel

type t = {
  manifest_version : int;
  id : Plugin_id.t;
  name : string;
  description : string option;
  version : Plugin_version.t;
  api : int;
  runtime : string;
  entrypoint : string;
  entrypoint_path : string;
  package_dir : string;
  path : string;
  contributions : Contribution.t list;
  requested_capabilities : Capability.t list;
}

let filename = "zenbu-plugin.toml"

let invalid ?plugin_id ?operation message =
  Error
    (Error.Extension_error
       {
         code = Error.Invalid_manifest;
         plugin_id;
         provider = None;
         operation;
         required = None;
         granted = [];
         message;
       })

let ( let* ) = Result.bind

let table = function
  | Otoml.TomlTable values | Otoml.TomlInlineTable values -> Some values
  | _ -> None

let field fields name = List.assoc_opt name fields

let required_string fields name =
  match field fields name with
  | Some (Otoml.TomlString value) when String.length value > 0 -> Ok value
  | _ -> invalid ~operation:name "manifest field must be a nonempty string"

let optional_string fields name =
  match field fields name with
  | None -> Ok None
  | Some (Otoml.TomlString value) -> Ok (Some value)
  | Some _ -> invalid ~operation:name "manifest field must be a string"

let required_int fields name =
  match field fields name with
  | Some (Otoml.TomlInteger value) -> Ok value
  | _ -> invalid ~operation:name "manifest field must be an integer"

let required_strings fields name =
  match field fields name with
  | Some (Otoml.TomlArray values) ->
      let rec collect results = function
        | [] -> Ok (List.rev results)
        | Otoml.TomlString value :: rest when String.length value > 0 ->
            collect (value :: results) rest
        | _ -> invalid ~operation:name "manifest field must be an array of nonempty strings"
      in
      collect [] values
  | _ -> invalid ~operation:name "manifest field must be an array of strings"

let no_duplicates field_name values =
  let seen = Hashtbl.create (List.length values) in
  match
    List.find_opt
      (fun value ->
        if Hashtbl.mem seen value then true
        else (
          Hashtbl.add seen value ();
          false))
      values
  with
  | None -> Ok values
  | Some value ->
      invalid ~operation:field_name
        ("manifest field contains a duplicate value: " ^ value)

let safe_entrypoint package_dir entrypoint =
  let components = String.split_on_char '/' entrypoint in
  if
    not (Filename.is_relative entrypoint)
    || List.exists (fun component -> component = ".." || component = "") components
  then
    invalid ~operation:"plugin.entrypoint"
      "entrypoint must be a nonempty relative path contained in its package"
  else
    let candidate = Filename.concat package_dir entrypoint in
    try
      let package_dir = Unix.realpath package_dir in
      let candidate = Unix.realpath candidate in
      let prefix = package_dir ^ Filename.dir_sep in
      if not (String.starts_with ~prefix candidate) then
        invalid ~operation:"plugin.entrypoint"
          "entrypoint resolves outside its plugin package"
      else if not (Sys.file_exists candidate) || Sys.is_directory candidate then
        invalid ~operation:"plugin.entrypoint" "entrypoint is not a regular file"
      else Ok candidate
    with Unix.Unix_error (_, _, _) | Sys_error _ ->
      invalid ~operation:"plugin.entrypoint" "entrypoint cannot be resolved"

let rec contributions_of values =
  match values with
  | [] -> Ok []
  | value :: rest ->
      let* value = Contribution.of_id value in
      let* rest = contributions_of rest in
      Ok (value :: rest)

let rec capabilities_of values =
  match values with
  | [] -> Ok []
  | value :: rest ->
      let* value = Capability.of_id value in
      let* rest = capabilities_of rest in
      Ok (value :: rest)

let parse path =
  let* document =
    match Otoml.Parser.from_file_result path with
    | Ok document -> Ok document
    | Error message -> invalid ~operation:"manifest" message
  in
  match table document with
  | None -> invalid ~operation:"manifest" "manifest root must be a table"
  | Some root -> (
      match field root "plugin" with
      | None -> invalid ~operation:"plugin" "manifest requires a [plugin] table"
      | Some plugin -> (
          match table plugin with
          | None -> invalid ~operation:"plugin" "[plugin] must be a TOML table"
          | Some plugin ->
              let* manifest_version = required_int root "manifest_version" in
              if manifest_version <> Contract.manifest_version then
                invalid ~operation:"manifest_version"
                  ("unsupported manifest version " ^ string_of_int manifest_version)
              else
                let package_dir = Filename.dirname path in
                let* id_text = required_string plugin "id" in
                let* id = Plugin_id.of_string id_text in
                let* name = required_string plugin "name" in
                let* description = optional_string plugin "description" in
                let* version_text = required_string plugin "version" in
                let* version = Plugin_version.of_string version_text in
                let* api = required_int plugin "api" in
                let* runtime = required_string plugin "runtime" in
                let* entrypoint = required_string plugin "entrypoint" in
                let* contribution_ids = required_strings plugin "contributions" in
                let* contribution_ids = no_duplicates "contributions" contribution_ids in
                let* contributions = contributions_of contribution_ids in
                let* capability_ids = required_strings plugin "capabilities" in
                let* capability_ids = no_duplicates "capabilities" capability_ids in
                let* requested_capabilities = capabilities_of capability_ids in
                let* entrypoint_path = safe_entrypoint package_dir entrypoint in
                Ok
                  {
                    manifest_version;
                    id;
                    name;
                    description;
                    version;
                    api;
                    runtime;
                    entrypoint;
                    entrypoint_path;
                    package_dir;
                    path;
                    contributions;
                    requested_capabilities;
                  }))

let manifest_version value = value.manifest_version
let id value = value.id
let name value = value.name
let description value = value.description
let version value = value.version
let api value = value.api
let runtime value = value.runtime
let entrypoint value = value.entrypoint
let entrypoint_path value = value.entrypoint_path
let package_dir value = value.package_dir
let path value = value.path
let contributions value = value.contributions
let requested_capabilities value = value.requested_capabilities
