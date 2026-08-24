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

type grammar_source = Built_in of { package : string; revision : string }
type grammar_bundle = Ocaml_bundle | Json_bundle

type grammar_candidate = {
  candidate_id : string;
  candidate_display_name : string;
  candidate_extensions : string list;
  candidate_source : grammar_source;
  candidate_version : string;
  candidate_abi : int;
  candidate_integrity : string;
  candidate_bundle : grammar_bundle;
}

type grammar_entry = {
  entry_id : string;
  entry_display_name : string;
  entry_extensions : string list;
  entry_source : grammar_source;
  entry_version : string;
  entry_abi : int;
  entry_integrity : string;
  entry_bundle : grammar_bundle;
}

type grammar_registry = { entries : grammar_entry list }

type grammar_error =
  | Empty_registry
  | Too_many_grammars of int
  | Invalid_identifier of string
  | Invalid_display_name of string
  | Invalid_extension of string
  | Duplicate_language of string
  | Duplicate_extension of string
  | Manifest_mismatch of string
  | Incompatible_abi of { grammar : string; actual : int }
  | Integrity_mismatch of string
  | Activation_failed of string

module Sha256 = struct
  let initial =
    [|
      0x6a09e667l;
      0xbb67ae85l;
      0x3c6ef372l;
      0xa54ff53al;
      0x510e527fl;
      0x9b05688cl;
      0x1f83d9abl;
      0x5be0cd19l;
    |]

  let constants =
    [|
      0x428a2f98l;
      0x71374491l;
      0xb5c0fbcfl;
      0xe9b5dba5l;
      0x3956c25bl;
      0x59f111f1l;
      0x923f82a4l;
      0xab1c5ed5l;
      0xd807aa98l;
      0x12835b01l;
      0x243185bel;
      0x550c7dc3l;
      0x72be5d74l;
      0x80deb1fel;
      0x9bdc06a7l;
      0xc19bf174l;
      0xe49b69c1l;
      0xefbe4786l;
      0x0fc19dc6l;
      0x240ca1ccl;
      0x2de92c6fl;
      0x4a7484aal;
      0x5cb0a9dcl;
      0x76f988dal;
      0x983e5152l;
      0xa831c66dl;
      0xb00327c8l;
      0xbf597fc7l;
      0xc6e00bf3l;
      0xd5a79147l;
      0x06ca6351l;
      0x14292967l;
      0x27b70a85l;
      0x2e1b2138l;
      0x4d2c6dfcl;
      0x53380d13l;
      0x650a7354l;
      0x766a0abbl;
      0x81c2c92el;
      0x92722c85l;
      0xa2bfe8a1l;
      0xa81a664bl;
      0xc24b8b70l;
      0xc76c51a3l;
      0xd192e819l;
      0xd6990624l;
      0xf40e3585l;
      0x106aa070l;
      0x19a4c116l;
      0x1e376c08l;
      0x2748774cl;
      0x34b0bcb5l;
      0x391c0cb3l;
      0x4ed8aa4al;
      0x5b9cca4fl;
      0x682e6ff3l;
      0x748f82eel;
      0x78a5636fl;
      0x84c87814l;
      0x8cc70208l;
      0x90befffal;
      0xa4506cebl;
      0xbef9a3f7l;
      0xc67178f2l;
    |]

  let rotate_right value amount =
    Int32.logor
      (Int32.shift_right_logical value amount)
      (Int32.shift_left value (32 - amount))

  let choice x y z =
    Int32.logxor (Int32.logand x y) (Int32.logand (Int32.lognot x) z)

  let majority x y z =
    Int32.logxor
      (Int32.logxor (Int32.logand x y) (Int32.logand x z))
      (Int32.logand y z)

  let big_sigma0 value =
    Int32.logxor
      (Int32.logxor (rotate_right value 2) (rotate_right value 13))
      (rotate_right value 22)

  let big_sigma1 value =
    Int32.logxor
      (Int32.logxor (rotate_right value 6) (rotate_right value 11))
      (rotate_right value 25)

  let small_sigma0 value =
    Int32.logxor
      (Int32.logxor (rotate_right value 7) (rotate_right value 18))
      (Int32.shift_right_logical value 3)

  let small_sigma1 value =
    Int32.logxor
      (Int32.logxor (rotate_right value 17) (rotate_right value 19))
      (Int32.shift_right_logical value 10)

  let word bytes offset =
    let byte index = Int32.of_int (Char.code (Bytes.get bytes index)) in
    Int32.logor
      (Int32.shift_left (byte offset) 24)
      (Int32.logor
         (Int32.shift_left (byte (offset + 1)) 16)
         (Int32.logor
            (Int32.shift_left (byte (offset + 2)) 8)
            (byte (offset + 3))))

  let digest source =
    let source_length = String.length source in
    let bit_length = Int64.mul (Int64.of_int source_length) 8L in
    let padding =
      let remainder = (source_length + 1) mod 64 in
      if remainder <= 56 then 56 - remainder else 120 - remainder
    in
    let bytes = Bytes.make (source_length + 1 + padding + 8) '\000' in
    Bytes.blit_string source 0 bytes 0 source_length;
    Bytes.set bytes source_length '\128';
    for index = 0 to 7 do
      let shift = 8 * (7 - index) in
      let byte = Int64.shift_right_logical bit_length shift |> Int64.to_int in
      Bytes.set bytes
        (Bytes.length bytes - 8 + index)
        (Char.chr (byte land 0xff))
    done;
    let hash = Array.copy initial in
    for block = 0 to (Bytes.length bytes / 64) - 1 do
      let schedule = Array.make 64 0l in
      for index = 0 to 15 do
        schedule.(index) <- word bytes ((block * 64) + (index * 4))
      done;
      for index = 16 to 63 do
        schedule.(index) <-
          Int32.add
            (small_sigma1 schedule.(index - 2))
            (Int32.add
               schedule.(index - 7)
               (Int32.add
                  (small_sigma0 schedule.(index - 15))
                  schedule.(index - 16)))
      done;
      let a = ref hash.(0) in
      let b = ref hash.(1) in
      let c = ref hash.(2) in
      let d = ref hash.(3) in
      let e = ref hash.(4) in
      let f = ref hash.(5) in
      let g = ref hash.(6) in
      let h = ref hash.(7) in
      for index = 0 to 63 do
        let first =
          Int32.add !h
            (Int32.add (big_sigma1 !e)
               (Int32.add (choice !e !f !g)
                  (Int32.add constants.(index) schedule.(index))))
        in
        let second = Int32.add (big_sigma0 !a) (majority !a !b !c) in
        h := !g;
        g := !f;
        f := !e;
        e := Int32.add !d first;
        d := !c;
        c := !b;
        b := !a;
        a := Int32.add first second
      done;
      hash.(0) <- Int32.add hash.(0) !a;
      hash.(1) <- Int32.add hash.(1) !b;
      hash.(2) <- Int32.add hash.(2) !c;
      hash.(3) <- Int32.add hash.(3) !d;
      hash.(4) <- Int32.add hash.(4) !e;
      hash.(5) <- Int32.add hash.(5) !f;
      hash.(6) <- Int32.add hash.(6) !g;
      hash.(7) <- Int32.add hash.(7) !h
    done;
    Array.to_list hash |> List.map (Printf.sprintf "%08lx") |> String.concat ""
end

type bundle_manifest = {
  source : grammar_source;
  version : string;
  abi : int;
  integrity : string;
  probe_grammars : (Tree_sitter_backend.grammar * string * int * int) list;
}

let maximum_registered_grammars = 32
let maximum_extensions_per_grammar = 16
let minimum_tree_sitter_abi = 13
let maximum_tree_sitter_abi = 15

let source_equal left right =
  match (left, right) with
  | ( Built_in { package = left_package; revision = left_revision },
      Built_in { package = right_package; revision = right_revision } ) ->
      String.equal left_package right_package
      && String.equal left_revision right_revision

let integrity material = "sha256:" ^ Sha256.digest material

let bundle_manifest = function
  | Ocaml_bundle ->
      {
        source = Built_in { package = "tree-sitter.ocaml"; revision = "0.1.0" };
        version = "0.1.0";
        abi = 15;
        integrity =
          integrity
            "tree-sitter.ocaml|0.1.0|ocaml|abi=15|symbols=456|fields=38|interface=ocaml_interface|interface-symbols=455|interface-fields=38";
        probe_grammars =
          [
            (Tree_sitter_backend.Ocaml, "ocaml", 456, 38);
            (Tree_sitter_backend.Ocaml_interface, "ocaml_interface", 455, 38);
          ];
      }
  | Json_bundle ->
      {
        source = Built_in { package = "tree-sitter.json"; revision = "0.1.0" };
        version = "0.1.0";
        abi = 15;
        integrity =
          integrity "tree-sitter.json|0.1.0|json|abi=15|symbols=25|fields=2";
        probe_grammars = [ (Tree_sitter_backend.Json, "json", 25, 2) ];
      }

let valid_identifier value =
  let length = String.length value in
  let valid_first = function 'a' .. 'z' | '0' .. '9' -> true | _ -> false in
  let valid_rest = function
    | 'a' .. 'z' | '0' .. '9' | '.' | '-' | '_' -> true
    | _ -> false
  in
  length > 0 && length <= 64
  && valid_first value.[0]
  &&
  let rec loop index =
    index = length || (valid_rest value.[index] && loop (index + 1))
  in
  loop 1

let has_control_character value =
  String.exists
    (fun character ->
      let code = Char.code character in
      code < 32 || code = 127)
    value

let valid_display_name value =
  String.length value > 0
  && String.length value <= 120
  && not (has_control_character value)

let valid_extension value =
  let length = String.length value in
  length > 1 && length <= 32
  && value.[0] = '.'
  && String.equal value (String.lowercase_ascii value)
  &&
  let rec loop index =
    index = length
    ||
    match value.[index] with
    | 'a' .. 'z' | '0' .. '9' | '_' | '-' -> loop (index + 1)
    | _ -> false
  in
  loop 1

let valid_integrity value =
  let prefix = "sha256:" in
  String.starts_with ~prefix value
  && String.length value = String.length prefix + 64
  &&
  let rec loop index =
    index = String.length value
    ||
    match value.[index] with
    | '0' .. '9' | 'a' .. 'f' -> loop (index + 1)
    | _ -> false
  in
  loop (String.length prefix)

let extension_variant entry path =
  match
    (entry.entry_bundle, String.lowercase_ascii (Filename.extension path))
  with
  | Ocaml_bundle, ".mli" -> Tree_sitter_backend.Ocaml_interface
  | Ocaml_bundle, _ -> Tree_sitter_backend.Ocaml
  | Json_bundle, _ -> Tree_sitter_backend.Json

let activation_error message = Error (Activation_failed message)

let validate_runtime_manifest candidate manifest =
  let rec validate = function
    | [] -> Ok ()
    | (grammar, expected_name, expected_symbols, expected_fields) :: rest -> (
        try
          let language = Tree_sitter_backend.language grammar in
          let name = Tree_sitter.Language.name language in
          let abi = Tree_sitter.Language.version language in
          let symbols = Tree_sitter.Language.symbol_count language in
          let fields = Tree_sitter.Language.field_count language in
          if abi < minimum_tree_sitter_abi || abi > maximum_tree_sitter_abi then
            Error
              (Incompatible_abi
                 { grammar = candidate.candidate_id; actual = abi })
          else if abi <> manifest.abi then
            Error
              (Incompatible_abi
                 { grammar = candidate.candidate_id; actual = abi })
          else if
            not
              (String.equal name expected_name
              && symbols = expected_symbols && fields = expected_fields)
          then
            Error
              (Manifest_mismatch
                 (Printf.sprintf
                    "%s runtime metadata does not match its bundled manifest"
                    candidate.candidate_id))
          else
            let parser = Tree_sitter_backend.create_parser grammar in
            ignore (Tree_sitter_backend.parse parser "");
            validate rest
        with exception_ ->
          activation_error
            (Printf.sprintf "%s activation probe failed: %s"
               candidate.candidate_id
               (Printexc.to_string exception_)))
  in
  validate manifest.probe_grammars

let validate_candidate candidate =
  let manifest = bundle_manifest candidate.candidate_bundle in
  if not (valid_identifier candidate.candidate_id) then
    Error (Invalid_identifier candidate.candidate_id)
  else if not (valid_display_name candidate.candidate_display_name) then
    Error (Invalid_display_name candidate.candidate_id)
  else if candidate.candidate_extensions = [] then
    Error (Invalid_extension (candidate.candidate_id ^ " has no extensions"))
  else if
    List.length candidate.candidate_extensions > maximum_extensions_per_grammar
  then
    Error
      (Invalid_extension (candidate.candidate_id ^ " has too many extensions"))
  else if
    List.exists
      (fun extension -> not (valid_extension extension))
      candidate.candidate_extensions
  then Error (Invalid_extension candidate.candidate_id)
  else if
    List.length candidate.candidate_extensions
    <> List.length
         (List.sort_uniq String.compare candidate.candidate_extensions)
  then Error (Duplicate_extension candidate.candidate_id)
  else if not (source_equal candidate.candidate_source manifest.source) then
    Error
      (Manifest_mismatch (candidate.candidate_id ^ " source is not approved"))
  else if not (String.equal candidate.candidate_version manifest.version) then
    Error
      (Manifest_mismatch (candidate.candidate_id ^ " version is not approved"))
  else if candidate.candidate_abi <> manifest.abi then
    Error
      (Incompatible_abi
         { grammar = candidate.candidate_id; actual = candidate.candidate_abi })
  else if not (valid_integrity candidate.candidate_integrity) then
    Error
      (Integrity_mismatch (candidate.candidate_id ^ " has malformed integrity"))
  else if not (String.equal candidate.candidate_integrity manifest.integrity)
  then
    Error
      (Integrity_mismatch (candidate.candidate_id ^ " integrity does not match"))
  else
    Result.map
      (fun () ->
        {
          entry_id = candidate.candidate_id;
          entry_display_name = candidate.candidate_display_name;
          entry_extensions = candidate.candidate_extensions;
          entry_source = candidate.candidate_source;
          entry_version = candidate.candidate_version;
          entry_abi = candidate.candidate_abi;
          entry_integrity = candidate.candidate_integrity;
          entry_bundle = candidate.candidate_bundle;
        })
      (validate_runtime_manifest candidate manifest)

let stage_grammar_registry candidates =
  if candidates = [] then Error Empty_registry
  else if List.length candidates > maximum_registered_grammars then
    Error (Too_many_grammars (List.length candidates))
  else
    let rec validate entries = function
      | [] ->
          Ok
            (List.sort
               (fun left right -> String.compare left.entry_id right.entry_id)
               entries)
      | candidate :: rest ->
          Result.bind (validate_candidate candidate) (fun entry ->
              if
                List.exists
                  (fun known -> String.equal known.entry_id entry.entry_id)
                  entries
              then Error (Duplicate_language entry.entry_id)
              else
                match
                  List.find_opt
                    (fun known ->
                      List.exists
                        (fun extension ->
                          List.mem extension known.entry_extensions)
                        entry.entry_extensions)
                    entries
                with
                | Some known ->
                    let extension =
                      List.find
                        (fun extension ->
                          List.mem extension known.entry_extensions)
                        entry.entry_extensions
                    in
                    Error (Duplicate_extension extension)
                | None -> validate (entry :: entries) rest)
    in
    Result.map (fun entries -> { entries }) (validate [] candidates)

let bundled_candidate ~id ~display_name ~extensions bundle =
  let manifest = bundle_manifest bundle in
  {
    candidate_id = id;
    candidate_display_name = display_name;
    candidate_extensions = extensions;
    candidate_source = manifest.source;
    candidate_version = manifest.version;
    candidate_abi = manifest.abi;
    candidate_integrity = manifest.integrity;
    candidate_bundle = bundle;
  }

let builtin_grammar_registry () =
  stage_grammar_registry
    [
      bundled_candidate ~id:"ocaml" ~display_name:"OCaml"
        ~extensions:[ ".ml"; ".mli" ] Ocaml_bundle;
      bundled_candidate ~id:"json" ~display_name:"JSON" ~extensions:[ ".json" ]
        Json_bundle;
    ]
  |> Result.get_ok

let grammar_registry_lock = Mutex.create ()
let active_grammar_registry = ref (builtin_grammar_registry ())

let current_grammar_registry () =
  Mutex.lock grammar_registry_lock;
  let registry = !active_grammar_registry in
  Mutex.unlock grammar_registry_lock;
  registry

let reload_grammar_registry candidates =
  Result.bind (stage_grammar_registry candidates) (fun staged ->
      Mutex.lock grammar_registry_lock;
      active_grammar_registry := staged;
      Mutex.unlock grammar_registry_lock;
      Ok staged)

module Language = struct
  type t = {
    id : string;
    display_name : string;
    extensions : string list;
    grammar : Tree_sitter_backend.grammar;
    entry : grammar_entry;
  }

  let of_entry ?path entry =
    {
      id = entry.entry_id;
      display_name = entry.entry_display_name;
      extensions = entry.entry_extensions;
      grammar =
        (match path with
        | None -> extension_variant entry ""
        | Some path -> extension_variant entry path);
      entry;
    }

  let supported () = (current_grammar_registry ()).entries |> List.map of_entry
  let id value = value.id
  let display_name value = value.display_name
  let extensions value = value.extensions

  let find id =
    (current_grammar_registry ()).entries
    |> List.find_opt (fun entry -> String.equal entry.entry_id id)
    |> Option.map of_entry

  let detect_path path =
    let extension = String.lowercase_ascii (Filename.extension path) in
    (current_grammar_registry ()).entries
    |> List.find_opt (fun entry -> List.mem extension entry.entry_extensions)
    |> Option.map (of_entry ~path)
end

module Grammar = struct
  let maximum_registered_grammars = maximum_registered_grammars
  let maximum_extensions_per_grammar = maximum_extensions_per_grammar
  let minimum_tree_sitter_abi = minimum_tree_sitter_abi
  let maximum_tree_sitter_abi = maximum_tree_sitter_abi

  module Source = struct
    type t = grammar_source =
      | Built_in of { package : string; revision : string }

    let package = function Built_in value -> value.package
    let revision = function Built_in value -> value.revision
  end

  module Bundle = struct
    type t = Ocaml | Json

    let internal = function Ocaml -> Ocaml_bundle | Json -> Json_bundle
    let source value = (bundle_manifest (internal value)).source
    let version value = (bundle_manifest (internal value)).version
    let abi value = (bundle_manifest (internal value)).abi
    let integrity value = (bundle_manifest (internal value)).integrity
    let id = function Ocaml -> "ocaml" | Json -> "json"
  end

  module Candidate = struct
    type t = grammar_candidate

    let create ~id ~display_name ~extensions ~source ~version ~abi ~integrity
        ~bundle =
      {
        candidate_id = id;
        candidate_display_name = display_name;
        candidate_extensions = extensions;
        candidate_source = source;
        candidate_version = version;
        candidate_abi = abi;
        candidate_integrity = integrity;
        candidate_bundle = Bundle.internal bundle;
      }
  end

  module Registry = struct
    type t = grammar_registry
    type error = grammar_error

    let builtins () = builtin_grammar_registry ()
    let stage candidates = stage_grammar_registry candidates
    let current = current_grammar_registry
    let reload candidates = reload_grammar_registry candidates
    let languages value = value.entries |> List.map Language.of_entry

    let find value id =
      List.find_opt (fun entry -> String.equal entry.entry_id id) value.entries
      |> Option.map Language.of_entry

    let detect_path value path =
      let extension = String.lowercase_ascii (Filename.extension path) in
      List.find_opt
        (fun entry -> List.mem extension entry.entry_extensions)
        value.entries
      |> Option.map (Language.of_entry ~path)

    let error_to_string = function
      | Empty_registry -> "grammar registry must contain at least one grammar"
      | Too_many_grammars count ->
          Printf.sprintf "grammar registry exceeds the %d grammar limit" count
      | Invalid_identifier value -> "invalid grammar id: " ^ value
      | Invalid_display_name value -> "invalid grammar display name: " ^ value
      | Invalid_extension value -> "invalid grammar extension: " ^ value
      | Duplicate_language value -> "duplicate grammar id: " ^ value
      | Duplicate_extension value -> "duplicate grammar extension: " ^ value
      | Manifest_mismatch value -> "grammar manifest mismatch: " ^ value
      | Incompatible_abi { grammar; actual } ->
          Printf.sprintf "grammar %s has incompatible Tree-sitter ABI %d"
            grammar actual
      | Integrity_mismatch value -> "grammar integrity mismatch: " ^ value
      | Activation_failed value -> "grammar activation failed: " ^ value
  end

  let source language = language.Language.entry.entry_source
  let version language = language.Language.entry.entry_version
  let abi language = language.Language.entry.entry_abi
  let integrity language = language.Language.entry.entry_integrity
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

module Query = struct
  type binding = {
    document_id : string;
    document_version : int;
    language_id : string;
    language_version : string;
    language_abi : int;
    language_integrity : string;
    grammar : Tree_sitter_backend.grammar;
  }

  type t = { binding : binding; query : Tree_sitter_backend.query }
  type capture = { name : string; range : Range.t }

  type error =
    | Query_too_large of { maximum_bytes : int; actual_bytes : int }
    | Query_too_complex of {
        maximum_patterns : int;
        actual_patterns : int;
        maximum_capture_names : int;
        actual_capture_names : int;
      }
    | Invalid_query of string
    | Snapshot_has_parse_error
    | Stale_snapshot of {
        expected_id : string;
        expected_version : int;
        actual_id : string;
        actual_version : int;
      }
    | Wrong_language of {
        expected_id : string;
        expected_version : string;
        actual_id : string;
        actual_version : string;
      }
    | Result_limit_exceeded of { maximum_captures : int }
    | Invalid_capture of string
    | Backend_failure of string

  let maximum_query_bytes = 64 * 1024
  let maximum_patterns = 256
  let maximum_capture_names = 128
  let maximum_captures = 1_024

  let binding snapshot =
    let language = Snapshot.language snapshot in
    {
      document_id = Snapshot.document_id snapshot;
      document_version = Snapshot.document_version snapshot;
      language_id = Language.id language;
      language_version = Grammar.version language;
      language_abi = Grammar.abi language;
      language_integrity = Grammar.integrity language;
      grammar = language.Language.grammar;
    }

  let document_id value = value.binding.document_id
  let document_version value = value.binding.document_version
  let language_id value = value.binding.language_id
  let language_version value = value.binding.language_version

  let error_to_string = function
    | Query_too_large { maximum_bytes; actual_bytes } ->
        Printf.sprintf "syntax query is %d bytes; limit is %d" actual_bytes
          maximum_bytes
    | Query_too_complex
        {
          maximum_patterns;
          actual_patterns;
          maximum_capture_names;
          actual_capture_names;
        } ->
        Printf.sprintf
          "syntax query has %d patterns and %d capture names; limits are %d \
           and %d"
          actual_patterns actual_capture_names maximum_patterns
          maximum_capture_names
    | Invalid_query message -> "invalid syntax query: " ^ message
    | Snapshot_has_parse_error ->
        "syntax query requires an error-free syntax snapshot"
    | Stale_snapshot
        { expected_id; expected_version; actual_id; actual_version } ->
        Printf.sprintf "stale syntax query snapshot: expected %s@%d, got %s@%d"
          expected_id expected_version actual_id actual_version
    | Wrong_language
        { expected_id; expected_version; actual_id; actual_version } ->
        Printf.sprintf
          "syntax query language changed: expected %s@%s, got %s@%s" expected_id
          expected_version actual_id actual_version
    | Result_limit_exceeded { maximum_captures } ->
        Printf.sprintf "syntax query exceeds the %d capture limit"
          maximum_captures
    | Invalid_capture message -> "invalid syntax query capture: " ^ message
    | Backend_failure message -> "syntax query backend failure: " ^ message

  let validate_query_source source =
    let actual_bytes = String.length source in
    if actual_bytes > maximum_query_bytes then
      Error
        (Query_too_large { maximum_bytes = maximum_query_bytes; actual_bytes })
    else if String.length source = 0 then Error (Invalid_query "query is empty")
    else
      match Text_buffer.of_utf8 source with
      | Ok _ -> Ok ()
      | Error _ -> Error (Invalid_query "query is not valid UTF-8")

  let validate_snapshot value snapshot =
    let actual = binding snapshot in
    if
      not
        (String.equal value.document_id actual.document_id
        && value.document_version = actual.document_version)
    then
      Error
        (Stale_snapshot
           {
             expected_id = value.document_id;
             expected_version = value.document_version;
             actual_id = actual.document_id;
             actual_version = actual.document_version;
           })
    else if
      not
        (String.equal value.language_id actual.language_id
        && String.equal value.language_version actual.language_version
        && value.language_abi = actual.language_abi
        && String.equal value.language_integrity actual.language_integrity
        && value.grammar = actual.grammar)
    then
      Error
        (Wrong_language
           {
             expected_id = value.language_id;
             expected_version = value.language_version;
             actual_id = actual.language_id;
             actual_version = actual.language_version;
           })
    else if Snapshot.has_error snapshot then Error Snapshot_has_parse_error
    else Ok ()

  let compile snapshot ~source =
    match validate_query_source source with
    | Error _ as error -> error
    | Ok () -> (
        if Snapshot.has_error snapshot then Error Snapshot_has_parse_error
        else
          try
            let language = Snapshot.language snapshot in
            let query =
              Tree_sitter_backend.compile_query language.Language.grammar
                ~source
            in
            let actual_patterns =
              Tree_sitter_backend.query_pattern_count query
            in
            let actual_capture_names =
              Tree_sitter_backend.query_capture_count query
            in
            if
              actual_patterns > maximum_patterns
              || actual_capture_names > maximum_capture_names
            then
              Error
                (Query_too_complex
                   {
                     maximum_patterns;
                     actual_patterns;
                     maximum_capture_names;
                     actual_capture_names;
                   })
            else Ok { binding = binding snapshot; query }
          with
          | Failure message | Invalid_argument message ->
              Error (Invalid_query message)
          | exception_ ->
              Error (Backend_failure (Printexc.to_string exception_)))

  let capture_name value = value.name
  let capture_range value = value.range

  let capture_of_backend snapshot value =
    match Tree_sitter_backend.query_capture_name value with
    | None ->
        Error
          (Backend_failure
             "Tree-sitter returned a capture without a declared capture name")
    | Some name ->
        let node = Tree_sitter_backend.query_capture_node value in
        Document_snapshot.range snapshot.Snapshot.document
          ~start_offset:(Tree_sitter_backend.start_byte node)
          ~stop_offset:(Tree_sitter_backend.end_byte node)
        |> Result.map_error (fun error ->
            Invalid_capture (Zenbu_kernel.Error.to_string error))
        |> Result.map (fun range -> { name; range })

  let captures value ~snapshot =
    match validate_snapshot value.binding snapshot with
    | Error _ as error -> error
    | Ok () -> (
        try
          let cursor = Tree_sitter_backend.create_query_cursor () in
          Tree_sitter_backend.execute_query cursor value.query
            (Tree_sitter_backend.root snapshot.Snapshot.tree);
          let rec collect count values =
            match Tree_sitter_backend.next_query_capture cursor value.query with
            | None -> Ok (List.rev values)
            | Some _ when count = maximum_captures ->
                Error (Result_limit_exceeded { maximum_captures })
            | Some capture -> (
                match capture_of_backend snapshot capture with
                | Error _ as error -> error
                | Ok capture -> collect (count + 1) (capture :: values))
          in
          collect 0 []
        with exception_ ->
          Error (Backend_failure (Printexc.to_string exception_)))

  let selections value ~snapshot ~capture =
    if String.length capture = 0 then
      Error (Invalid_capture "capture name must not be empty")
    else
      match captures value ~snapshot with
      | Error _ as error -> error
      | Ok captures -> (
          let selected =
            List.filter
              (fun candidate -> String.equal candidate.name capture)
              captures
          in
          if selected = [] then
            Error (Invalid_capture ("query did not produce @" ^ capture))
          else
            let rec to_selections values = function
              | [] -> Ok (List.rev values)
              | capture :: rest -> (
                  match
                    Selection.make
                      ~anchor:(Range.start capture.range)
                      ~head:(Range.stop capture.range)
                  with
                  | Error error ->
                      Error
                        (Invalid_capture (Zenbu_kernel.Error.to_string error))
                  | Ok selection -> to_selections (selection :: values) rest)
            in
            match to_selections [] selected with
            | Error _ as error -> error
            | Ok selections ->
                Selection_set.create ~primary:0 selections
                |> Result.map_error (fun error ->
                    Invalid_capture (Zenbu_kernel.Error.to_string error)))
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

  let id = function
    | Focus_primary -> "syntax.focus"
    | Containing -> "syntax.containing"
    | Parent -> "syntax.parent"
    | First_child -> "syntax.child"
    | Next_sibling -> "syntax.next-sibling"
    | Previous_sibling -> "syntax.previous-sibling"
    | Expand -> "syntax.expand"
    | Same_kind_siblings -> "syntax.select-same-kind"

  let descriptors () =
    let provider =
      Provider.create ~id:"zenbu.syntax" ~kind:Provider.Syntax |> Result.get_ok
    in
    let declare selector title description =
      Semantic_descriptor.create ~id:(id selector) ~title ~description ~provider
        ~kind:Semantic_descriptor.Selector ~requires_syntax:true ()
      |> Result.get_ok
    in
    [
      declare Focus_primary "Focus syntax node"
        "Resolves the smallest named syntax node at the primary selection.";
      declare Containing "Containing syntax node"
        "Resolves the named syntax node containing the primary selection.";
      declare Parent "Syntax parent" "Resolves the named parent syntax node.";
      declare First_child "Syntax first child"
        "Resolves the first named child syntax node.";
      declare Next_sibling "Syntax next sibling"
        "Resolves the next named sibling syntax node.";
      declare Previous_sibling "Syntax previous sibling"
        "Resolves the previous named sibling syntax node.";
      declare Expand "Syntax expand" "Resolves the named parent syntax node.";
      declare Same_kind_siblings "Syntax same-kind siblings"
        "Resolves named siblings having the current node kind.";
    ]

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

module Highlight = struct
  type class_ = Keyword | String | Number | Comment | Type | Constructor
  type span = { class_ : class_; start_offset : int; stop_offset : int }

  let class_ value = value.class_
  let start_offset value = value.start_offset
  let stop_offset value = value.stop_offset

  let class_name = function
    | Keyword -> "keyword"
    | String -> "string"
    | Number -> "number"
    | Comment -> "comment"
    | Type -> "type"
    | Constructor -> "constructor"

  let member value values = List.mem value values

  let classify language kind =
    let language = Language.id language in
    if member kind [ "string"; "quoted_string"; "character" ] then Some String
    else if
      member kind
        [ "integer"; "float"; "number"; "integer_literal"; "float_literal" ]
    then Some Number
    else if member kind [ "comment"; "line_comment"; "block_comment" ] then
      Some Comment
    else if
      member kind
        [ "type_constructor"; "type_variable"; "type_name"; "module_type_name" ]
    then Some Type
    else if
      member kind
        [
          "constructor_name";
          "constructor";
          "variant_constructor";
          "module_name";
        ]
    then Some Constructor
    else
      let keywords =
        match language with
        | "json" -> [ "true"; "false"; "null" ]
        | "ocaml" ->
            [
              "and";
              "as";
              "assert";
              "begin";
              "class";
              "do";
              "done";
              "downto";
              "else";
              "end";
              "exception";
              "external";
              "for";
              "fun";
              "function";
              "functor";
              "if";
              "in";
              "include";
              "inherit";
              "initializer";
              "lazy";
              "let";
              "match";
              "method";
              "module";
              "mutable";
              "new";
              "object";
              "of";
              "open";
              "or";
              "private";
              "rec";
              "sig";
              "struct";
              "then";
              "to";
              "try";
              "type";
              "val";
              "virtual";
              "when";
              "while";
              "with";
            ]
        | _ -> []
      in
      if member kind keywords then Some Keyword else None

  let spans snapshot =
    let values = ref [] in
    let rec visit node =
      let kind = Tree_sitter_backend.kind node in
      let start_offset = Tree_sitter_backend.start_byte node in
      let stop_offset = Tree_sitter_backend.end_byte node in
      (match classify (Snapshot.language snapshot) kind with
      | Some class_ when start_offset < stop_offset ->
          values := { class_; start_offset; stop_offset } :: !values
      | Some _ | None -> ());
      for index = 0 to Tree_sitter_backend.child_count node - 1 do
        match Tree_sitter_backend.child node index with
        | None -> ()
        | Some child -> visit child
      done
    in
    visit (Tree_sitter_backend.root snapshot.Snapshot.tree);
    !values
    |> List.sort (fun left right ->
        let length value = value.stop_offset - value.start_offset in
        Int.compare (length left) (length right))
end

module Service = struct
  type strategy = Cached | Full_parse | Incremental_parse | Tree_copy

  let maximum_source_bytes = 8 * 1024 * 1024

  type status = {
    status_language : Language.t;
    status_cached_version : int option;
    status_last_strategy : strategy option;
  }

  type t = {
    language : Language.t;
    parser : Tree_sitter_backend.parser;
    mutable cached : Snapshot.t option;
    mutable last_strategy : strategy option;
  }

  let create language =
    {
      language;
      parser = Tree_sitter_backend.create_parser language.Language.grammar;
      cached = None;
      last_strategy = None;
    }

  let language value = value.language
  let cached value = value.cached

  let status value =
    {
      status_language = value.language;
      status_cached_version = Option.map Snapshot.document_version value.cached;
      status_last_strategy = value.last_strategy;
    }

  let status_language value = value.status_language
  let status_cached_version value = value.status_cached_version
  let status_last_strategy value = value.status_last_strategy

  let strategy_to_string = function
    | Cached -> "cached"
    | Full_parse -> "full"
    | Incremental_parse -> "incremental"
    | Tree_copy -> "tree-copy"

  let make_snapshot service document tree =
    { Snapshot.document; language = service.language; tree }

  let protect parse =
    try Ok (parse ())
    with exception_ ->
      Error (Error.Backend_failure (Printexc.to_string exception_))

  let validate_source_size document =
    let bytes = String.length (Document_snapshot.contents document) in
    if bytes > maximum_source_bytes then
      Error
        (Error.Backend_failure
           (Printf.sprintf "syntax source exceeds the %d byte limit"
              maximum_source_bytes))
    else Ok ()

  let parse_full service document =
    Result.bind (validate_source_size document) (fun () ->
        Tree_sitter_backend.reset service.parser;
        protect (fun () ->
            let tree =
              Tree_sitter_backend.parse service.parser
                (Document_snapshot.contents document)
            in
            let snapshot = make_snapshot service document tree in
            service.cached <- Some snapshot;
            service.last_strategy <- Some Full_parse;
            snapshot))

  let refresh service document =
    match service.cached with
    | Some snapshot when Snapshot.matches_document snapshot document ->
        service.last_strategy <- Some Cached;
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
        match validate_source_size after with
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
                        service.last_strategy <-
                          Some
                            (if edits = [] then Tree_copy else Incremental_parse);
                        snapshot))
            | _ -> parse_full service after))
end
