open Zenbu_kernel

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

let default_limits =
  {
    maximum_files = 512;
    maximum_results = 256;
    maximum_bytes_per_file = 1_048_576;
    maximum_total_bytes = 33_554_432;
  }

let error message = Error ("project search: " ^ message)

let valid_limits limits =
  if limits.maximum_files <= 0 then error "maximum files must be positive"
  else if limits.maximum_results <= 0 then
    error "maximum results must be positive"
  else if limits.maximum_bytes_per_file <= 0 then
    error "maximum bytes per file must be positive"
  else if limits.maximum_total_bytes <= 0 then
    error "maximum total bytes must be positive"
  else Ok ()

let read_prefix path maximum =
  try
    let channel = open_in_bin path in
    Fun.protect
      ~finally:(fun () -> close_in_noerr channel)
      (fun () ->
        let bytes = Bytes.create maximum in
        let count = input channel bytes 0 maximum in
        let truncated =
          (Unix.fstat (Unix.descr_of_in_channel channel)).Unix.st_size > count
        in
        Ok (Bytes.sub_string bytes 0 count, truncated))
  with
  | Sys_error message -> error ("cannot read " ^ path ^ ": " ^ message)
  | Unix.Unix_error (reason, _, _) ->
      error ("cannot read " ^ path ^ ": " ^ Unix.error_message reason)
  | exception_ ->
      error ("cannot read " ^ path ^ ": " ^ Printexc.to_string exception_)

let line_at contents offset =
  let rec loop index line =
    if index >= offset then line
    else loop (index + 1) (if contents.[index] = '\n' then line + 1 else line)
  in
  loop 0 1

let utf8_prefix contents =
  let rec trim length remaining =
    match Text_buffer.of_utf8 (String.sub contents 0 length) with
    | Ok buffer -> Ok (buffer, String.length contents - length)
    | Error _ when remaining > 0 -> trim (length - 1) (remaining - 1)
    | Error _ -> Error ()
  in
  trim (String.length contents) (min 3 (String.length contents))

let literal_offsets buffer ~query =
  let contents = Text_buffer.contents buffer in
  let query_length = String.length query in
  let content_length = String.length contents in
  let rec loop offset values =
    if offset + query_length > content_length then List.rev values
    else if
      String.sub contents offset query_length = query
      && Text_buffer.is_code_point_boundary buffer offset
      && Text_buffer.is_code_point_boundary buffer (offset + query_length)
    then loop (offset + query_length) (offset :: values)
    else loop (offset + 1) values
  in
  loop 0 []

let search root ~limits ~query =
  if String.length query = 0 then error "query must not be empty"
  else
    match valid_limits limits with
    | Error _ as error -> error
    | Ok () -> (
        match Project_root.discover root with
        | Error error -> Error error
        | Ok entries ->
            let rec scan files bytes results truncated = function
              | [] ->
                  Ok
                    {
                      query;
                      results = List.rev results;
                      scanned_files = files;
                      scanned_bytes = bytes;
                      truncated;
                    }
              | _ when files = limits.maximum_files ->
                  Ok
                    {
                      query;
                      results = List.rev results;
                      scanned_files = files;
                      scanned_bytes = bytes;
                      truncated = true;
                    }
              | _ when bytes = limits.maximum_total_bytes ->
                  Ok
                    {
                      query;
                      results = List.rev results;
                      scanned_files = files;
                      scanned_bytes = bytes;
                      truncated = true;
                    }
              | (entry : Project_root.entry) :: rest -> (
                  match
                    Project_root.resolve root ~relative_path:entry.relative_path
                  with
                  | Error _ -> scan files bytes results truncated rest
                  | Ok path -> (
                      match
                        read_prefix path
                          (min limits.maximum_bytes_per_file
                             (limits.maximum_total_bytes - bytes))
                      with
                      | Error _ -> scan files bytes results truncated rest
                      | Ok (contents, file_truncated) -> (
                          match utf8_prefix contents with
                          | Error _ ->
                              scan (files + 1)
                                (bytes + String.length contents)
                                results true rest
                          | Ok (_, trimmed)
                            when trimmed > 0 && not file_truncated ->
                              scan (files + 1)
                                (bytes + String.length contents)
                                results true rest
                          | Ok (buffer, _) ->
                              let contents = Text_buffer.contents buffer in
                              let offsets = literal_offsets buffer ~query in
                              let capacity =
                                limits.maximum_results - List.length results
                              in
                              let accepted =
                                if capacity <= 0 then []
                                else
                                  offsets |> List.to_seq |> Seq.take capacity
                                  |> List.of_seq
                              in
                              let results =
                                List.fold_left
                                  (fun results byte_offset ->
                                    {
                                      relative_path = entry.relative_path;
                                      byte_offset;
                                      line = line_at contents byte_offset;
                                    }
                                    :: results)
                                  results accepted
                              in
                              let truncated =
                                truncated || file_truncated
                                || List.length accepted < List.length offsets
                              in
                              if List.length results = limits.maximum_results
                              then
                                Ok
                                  {
                                    query;
                                    results = List.rev results;
                                    scanned_files = files + 1;
                                    scanned_bytes =
                                      bytes + String.length contents;
                                    truncated = true;
                                  }
                              else
                                scan (files + 1)
                                  (bytes + String.length contents)
                                  results truncated rest)))
            in
            scan 0 0 [] false entries)

let validate_result root ~query result =
  match Project_root.resolve root ~relative_path:result.relative_path with
  | Error error -> Error error
  | Ok path -> (
      match File_io.read path with
      | Error error -> Error (File_io.to_string error)
      | Ok contents -> (
          match Text_buffer.of_utf8 contents with
          | Error error -> Error (Error.to_string error)
          | Ok buffer ->
              let offset = result.byte_offset in
              if offset + String.length query > String.length contents then
                error "result is stale: offset is outside the current file"
              else if
                (not (Text_buffer.is_code_point_boundary buffer offset))
                || not
                     (Text_buffer.is_code_point_boundary buffer
                        (offset + String.length query))
              then error "result is stale: offset splits a UTF-8 code point"
              else if String.sub contents offset (String.length query) <> query
              then error "result is stale: query no longer matches"
              else Ok path))
