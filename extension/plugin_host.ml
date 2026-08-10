open Zenbu_kernel
open Zenbu_model_api
module Scripting = Zenbu_scripting.Scripting

type config = Default | Directories of string list | Disabled
type state = Active | Failed

type active = { manifest : Manifest.t; generation : Scripting.t; last_error : Error.t option }

type failure = { path : string; manifest : Manifest.t option; error : Error.t }

type view = {
  state : state;
  manifest : Manifest.t option;
  registered_ids : string list;
  error : Error.t option;
}

type t = { config : config; active : active list; failures : failure list }

let state_name = function Active -> "active" | Failed -> "failed"

let default_dirs () =
  let root =
    match Sys.getenv_opt "XDG_CONFIG_HOME" with
    | Some path when String.length path > 0 -> path
    | None | Some _ -> (
        match Sys.getenv_opt "HOME" with
        | Some path when String.length path > 0 -> Filename.concat path ".config"
        | None | Some _ -> ".config")
  in
  [ Filename.concat root "zenbu/plugins" ]

let dirs = function Default -> default_dirs () | Directories values -> values | Disabled -> []

let plugin_error code ?plugin_id ?operation message =
  Error.Extension_error
    {
      code;
      plugin_id;
      provider = None;
      operation;
      required = None;
      granted = [];
      message;
    }

let manifest_paths config =
  let rec collect paths = function
    | [] -> paths
    | directory :: rest ->
        let entries =
          try Sys.readdir directory |> Array.to_list |> List.sort String.compare
          with Sys_error _ -> []
        in
        let paths =
          List.fold_left
            (fun paths entry ->
              let child = Filename.concat directory entry in
              if Sys.file_exists child && Sys.is_directory child then
                let manifest = Filename.concat child Manifest.filename in
                if Sys.file_exists manifest then manifest :: paths else paths
              else paths)
            paths entries
        in
        collect paths rest
  in
  collect [] (dirs config) |> List.sort String.compare

let parse_candidates config =
  manifest_paths config
  |> List.map (fun path ->
         match Manifest.parse path with
         | Ok manifest -> Ok manifest
         | Error error -> Error { path; manifest = None; error })

let provider manifest =
  Provider.create_with_metadata
    ~id:(Manifest.id manifest |> Plugin_id.to_string)
    ~kind:Provider.Plugin ~source:(Manifest.path manifest)
    ~plugin_id:(Manifest.id manifest |> Plugin_id.to_string)
    ~version:(Manifest.version manifest |> Plugin_version.to_string)
    ~runtime:(Manifest.runtime manifest) ()

let validate_manifest manifest =
  if Manifest.api manifest <> Contract.api_version then
    Error
      (plugin_error Error.Incompatible_api
         ~plugin_id:(Manifest.id manifest |> Plugin_id.to_string) ~operation:"api"
         ("plugin requires Extension API v" ^ string_of_int (Manifest.api manifest)
        ^ ", but Zenbu supports v" ^ string_of_int Contract.api_version))
  else if not (Contract.supports_runtime (Manifest.runtime manifest)) then
    Error
      (plugin_error Error.Unknown_runtime
         ~plugin_id:(Manifest.id manifest |> Plugin_id.to_string)
         ~operation:(Manifest.runtime manifest)
         "plugin requests an unsupported runtime adapter")
  else Ok ()

let stage ~base_commands ~base_semantics manifest =
  Result.bind (validate_manifest manifest) (fun () ->
      Result.bind (provider manifest) (fun provider ->
          Scripting.load_plugin ~provider
            ~capabilities:(List.map Capability.id (Manifest.requested_capabilities manifest))
            ~contributions:(List.map Contribution.id (Manifest.contributions manifest))
            ~base_commands ~base_semantics ~entrypoint:(Manifest.entrypoint_path manifest)
          |> Result.map (fun generation -> { manifest; generation; last_error = None })))

let binding_key binding =
  Input_event.to_string (Scripting.binding_input binding)
  ^ "\000"
  ^
  match Scripting.binding_scope binding with
  | Scripting.Global -> "global"
  | Scripting.Model model -> "model:" ^ model
  | Scripting.Model_status { model; status } -> "model:" ^ model ^ ":" ^ status

let registered_ids active =
  let commands =
    Scripting.commands active.generation
    |> List.map (fun command -> Command.descriptor command |> Command_descriptor.id |> Command_id.to_string)
  in
  let semantics =
    Scripting.descriptors active.generation |> List.map Semantic_descriptor.id
  in
  commands @ semantics

let collisions ~base_bindings (staged : active list) =
  let owners = Hashtbl.create 32 in
  let conflicted = Hashtbl.create 8 in
  let mark (left : active) (right : active) =
    Hashtbl.replace conflicted (Manifest.id left.manifest |> Plugin_id.to_string) ();
    Hashtbl.replace conflicted (Manifest.id right.manifest |> Plugin_id.to_string) ()
  in
  List.iter
    (fun (active : active) ->
      List.iter
        (fun id ->
          match Hashtbl.find_opt owners id with
          | None -> Hashtbl.add owners id active
          | Some prior -> mark prior active)
        (registered_ids active))
    staged;
  let binding_owners = Hashtbl.create 16 in
  List.iter (fun binding -> Hashtbl.replace binding_owners (binding_key binding) None) base_bindings;
  List.iter
    (fun (active : active) ->
      Scripting.bindings active.generation
      |> List.iter (fun binding ->
             let key = binding_key binding in
             match Hashtbl.find_opt binding_owners key with
             | None -> Hashtbl.add binding_owners key (Some active)
             | Some None ->
                 Hashtbl.replace conflicted (Manifest.id active.manifest |> Plugin_id.to_string) ()
             | Some (Some prior) -> mark prior active))
    staged;
  staged
  |> List.filter_map (fun (active : active) ->
         let id = Manifest.id active.manifest |> Plugin_id.to_string in
         if Hashtbl.mem conflicted id then Some active else None)

let failure_of_collision (active : active) =
  {
    path = Manifest.path active.manifest;
    manifest = Some active.manifest;
    error =
      plugin_error Error.Invalid_plugin_package
        ~plugin_id:(Manifest.id active.manifest |> Plugin_id.to_string)
        ~operation:"registry"
        "plugin contribution or binding collides with another active provider";
  }

let build ?(previous = []) ~config ~base_commands ~base_semantics ~base_bindings () =
  let parsed = parse_candidates config in
  let parse_failures = List.filter_map (function Error failure -> Some failure | Ok _ -> None) parsed in
  let manifests = List.filter_map (function Ok manifest -> Some manifest | Error _ -> None) parsed in
  let duplicate_ids = Hashtbl.create 8 in
  List.iter
    (fun manifest ->
      let id = Manifest.id manifest |> Plugin_id.to_string in
      Hashtbl.replace duplicate_ids id (1 + Option.value ~default:0 (Hashtbl.find_opt duplicate_ids id)))
    manifests;
  let stages, stage_failures =
    List.fold_left
      (fun (stages, failures) manifest ->
        let id = Manifest.id manifest |> Plugin_id.to_string in
        if Option.value ~default:0 (Hashtbl.find_opt duplicate_ids id) > 1 then
          ( stages,
            {
              path = Manifest.path manifest;
              manifest = Some manifest;
              error =
                plugin_error Error.Invalid_plugin_package ~plugin_id:id ~operation:"plugin.id"
                  "multiple discovered packages declare the same plugin ID";
            }
            :: failures )
        else
          match stage ~base_commands ~base_semantics manifest with
          | Ok active -> (active :: stages, failures)
          | Error error ->
              ( stages,
                { path = Manifest.path manifest; manifest = Some manifest; error }
                :: failures ))
      ([], []) manifests
  in
  let stages = List.rev stages in
  let collision_active = collisions ~base_bindings stages in
  let collision_ids =
    List.map
      (fun (active : active) -> Manifest.id active.manifest |> Plugin_id.to_string)
      collision_active
  in
  let kept =
    stages
    |> List.filter (fun (active : active) ->
           not
             (List.mem
                (Manifest.id active.manifest |> Plugin_id.to_string)
                collision_ids))
  in
  List.iter (fun active -> Scripting.dispose active.generation) collision_active;
  let current_paths = List.map Manifest.path manifests in
  let retained, replacement_failures =
    List.fold_left
      (fun (retained, failures) failure ->
        match
          List.find_opt
            (fun (active : active) ->
              String.equal (Manifest.path active.manifest) failure.path)
            previous
        with
        | Some active when List.mem failure.path current_paths ->
            ({ active with last_error = Some failure.error } :: retained, failures)
        | _ -> (retained, failure :: failures))
      ([], [])
      (parse_failures @ List.rev stage_failures @ List.map failure_of_collision collision_active)
  in
  let replaced_paths =
    List.map (fun (active : active) -> Manifest.path active.manifest) kept
  in
  List.iter
    (fun (active : active) ->
      if List.mem (Manifest.path active.manifest) replaced_paths then Scripting.dispose active.generation
      else if not (List.mem (Manifest.path active.manifest) current_paths) then Scripting.dispose active.generation)
    previous;
  {
    config;
    active =
      List.sort
        (fun (left : active) (right : active) ->
          Plugin_id.compare (Manifest.id left.manifest) (Manifest.id right.manifest))
        (kept @ retained);
    failures = List.sort (fun left right -> String.compare left.path right.path) replacement_failures;
  }

let load ~config ~base_commands ~base_semantics ?(base_bindings = []) () =
  build ~config ~base_commands ~base_semantics ~base_bindings ()

let reload value ~base_commands ~base_semantics ?(base_bindings = []) () =
  build ~previous:value.active ~config:value.config ~base_commands ~base_semantics ~base_bindings ()

let deactivate value id =
  let active, removed =
    List.partition
      (fun (active : active) ->
        not (Plugin_id.equal (Manifest.id active.manifest) id))
      value.active
  in
  List.iter (fun active -> Scripting.dispose active.generation) removed;
  { value with active }

let dispose value = List.iter (fun active -> Scripting.dispose active.generation) value.active

let commands value = List.concat_map (fun active -> Scripting.commands active.generation) value.active

let semantic_behaviors value =
  List.fold_left
    (fun result active ->
      Result.bind result (fun registry ->
          Semantic_behavior_registry.merge registry (Scripting.semantic_behaviors active.generation)))
    (Ok Semantic_behavior_registry.empty) value.active
  |> Result.get_ok

let bindings value = List.concat_map (fun active -> Scripting.bindings active.generation) value.active
let hooks value = List.concat_map (fun active -> Scripting.hooks active.generation) value.active

let view_of_active (active : active) =
  {
    state = Active;
    manifest = Some active.manifest;
    registered_ids = registered_ids active;
    error = active.last_error;
  }
let view_of_failure (failure : failure) =
  {
    state = Failed;
    manifest = failure.manifest;
    registered_ids = [];
    error = Some failure.error;
  }
let views value = List.map view_of_active value.active @ List.map view_of_failure value.failures

let find value id =
  views value
  |> List.find_opt (fun view ->
         Option.value ~default:false
           (Option.map (Plugin_id.equal id) (Option.map Manifest.id view.manifest)))

let view_id value = Option.map Manifest.id value.manifest
let view_name value = Option.map Manifest.name value.manifest
let view_version value = Option.map Manifest.version value.manifest
let view_api value = Option.map Manifest.api value.manifest
let view_runtime value = Option.map Manifest.runtime value.manifest
let view_manifest_path value = Option.value ~default:"<unknown>" (Option.map Manifest.path value.manifest)
let view_state value = value.state
let view_requested_capabilities value = Option.value ~default:[] (Option.map Manifest.requested_capabilities value.manifest)
let view_granted_capabilities = view_requested_capabilities
let view_contributions value = Option.value ~default:[] (Option.map Manifest.contributions value.manifest)
let view_registered_ids value = value.registered_ids
let view_error value = value.error
