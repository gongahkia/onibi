type limits = {
  maximum_files : int;
  maximum_results : int;
  maximum_bytes_per_file : int;
  maximum_total_bytes : int;
}

type result = { relative_path : string; byte_offset : int; line : int }

type snapshot = {
  query : string;
  results : result list;
  scanned_files : int;
  scanned_bytes : int;
  truncated : bool;
}

val default_limits : limits

val search :
  Project_root.t ->
  limits:limits ->
  query:string ->
  (snapshot, string) Stdlib.result

val validate_result :
  Project_root.t -> query:string -> result -> (string, string) Stdlib.result
