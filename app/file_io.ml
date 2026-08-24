type error =
  | Read_error of { path : string; message : string }
  | Write_error of { path : string; message : string }
  | Save_conflict of { path : string; message : string }

type snapshot = { contents : string; device : int; inode : int }

type watch_state =
  | Current
  | Changed
  | Replaced
  | Renamed
  | Deleted
  | Failure of string

let to_string = function
  | Read_error { path; message } ->
      Printf.sprintf "cannot read %s: %s" path message
  | Write_error { path; message } ->
      Printf.sprintf "cannot save %s: %s" path message
  | Save_conflict { path; message } ->
      Printf.sprintf "save conflict for %s: %s" path message

let read path =
  try
    let channel = open_in_bin path in
    Fun.protect
      ~finally:(fun () -> close_in_noerr channel)
      (fun () -> Ok (really_input_string channel (in_channel_length channel)))
  with
  | Sys_error message -> Error (Read_error { path; message })
  | Unix.Unix_error (error, _, _) ->
      Error (Read_error { path; message = Unix.error_message error })
  | exception_ ->
      Error (Read_error { path; message = Printexc.to_string exception_ })

let snapshot ~path ~contents =
  try
    let stats = Unix.stat path in
    Ok { contents; device = stats.Unix.st_dev; inode = stats.Unix.st_ino }
  with
  | Unix.Unix_error (error, _, _) ->
      Error (Read_error { path; message = Unix.error_message error })
  | Sys_error message -> Error (Read_error { path; message })
  | exception_ ->
      Error (Read_error { path; message = Printexc.to_string exception_ })

let read_snapshot path =
  Result.bind (read path) (fun contents ->
      Result.map
        (fun snapshot -> (contents, snapshot))
        (snapshot ~path ~contents))

let moved_within_directory expected path =
  let directory = Filename.dirname path in
  try
    Sys.readdir directory
    |> Array.exists (fun name ->
        let candidate = Filename.concat directory name in
        if String.equal candidate path then false
        else
          try
            let stats = Unix.stat candidate in
            stats.Unix.st_dev = expected.device
            && stats.Unix.st_ino = expected.inode
          with Unix.Unix_error _ | Sys_error _ -> false)
  with Sys_error _ | Unix.Unix_error _ -> false

let watch_state expected ~path =
  match read_snapshot path with
  | Error (Read_error { message; _ }) -> (
      try
        ignore (Unix.lstat path);
        Failure message
      with
      | Unix.Unix_error (Unix.ENOENT, _, _) ->
          if moved_within_directory expected path then Renamed else Deleted
      | Unix.Unix_error _ | Sys_error _ -> Failure message)
  | Error (Write_error _ | Save_conflict _) ->
      Failure "unexpected file-read error"
  | Ok (contents, actual) ->
      if expected.device <> actual.device || expected.inode <> actual.inode then
        Replaced
      else if not (String.equal expected.contents contents) then Changed
      else Current

let check_snapshot expected ~path =
  match watch_state expected ~path with
  | Current -> Ok ()
  | Changed ->
      Error (Save_conflict { path; message = "the target contents changed" })
  | Replaced ->
      Error (Save_conflict { path; message = "the target was replaced" })
  | Renamed ->
      Error (Save_conflict { path; message = "the target was renamed" })
  | Deleted ->
      Error
        (Save_conflict { path; message = "cannot verify the current target" })
  | Failure message ->
      Error
        (Save_conflict
           { path; message = "cannot verify the current target: " ^ message })

let open_temporary path =
  let directory = Filename.dirname path in
  let basename = Filename.basename path in
  let rec attempt number =
    if number = 100 then
      Error
        (Write_error
           { path; message = "could not allocate an adjacent temporary file" })
    else
      let temporary =
        Filename.concat directory
          (Printf.sprintf ".%s.zenbu-%d-%d" basename (Unix.getpid ()) number)
      in
      try
        Ok
          ( temporary,
            Unix.openfile temporary
              [ Unix.O_WRONLY; Unix.O_CREAT; Unix.O_EXCL ]
              0o600 )
      with
      | Unix.Unix_error (Unix.EEXIST, _, _) -> attempt (number + 1)
      | Unix.Unix_error (error, _, _) ->
          Error (Write_error { path; message = Unix.error_message error })
  in
  attempt 0

let write_all descriptor contents =
  let rec loop offset =
    if offset < String.length contents then
      let written =
        Unix.write_substring descriptor contents offset
          (String.length contents - offset)
      in
      if written = 0 then raise (Failure "short write")
      else loop (offset + written)
  in
  loop 0

let save_atomic ~path ~contents =
  match open_temporary path with
  | Error _ as error -> error
  | Ok (temporary, descriptor) -> (
      let descriptor_open = ref true in
      let cleanup () =
        if !descriptor_open then (
          descriptor_open := false;
          try Unix.close descriptor with Unix.Unix_error _ -> ());
        try Unix.unlink temporary with Unix.Unix_error _ -> ()
      in
      try
        write_all descriptor contents;
        Unix.fsync descriptor;
        (try Unix.chmod temporary (Unix.stat path).Unix.st_perm
         with Unix.Unix_error (Unix.ENOENT, _, _) -> ());
        Unix.close descriptor;
        descriptor_open := false;
        Unix.rename temporary path;
        Ok ()
      with
      | Unix.Unix_error (error, _, _) ->
          cleanup ();
          Error (Write_error { path; message = Unix.error_message error })
      | Failure message ->
          cleanup ();
          Error (Write_error { path; message })
      | exception_ ->
          cleanup ();
          Error (Write_error { path; message = Printexc.to_string exception_ }))

let save_atomic_snapshot ~path ~contents =
  Result.bind (save_atomic ~path ~contents) (fun () -> snapshot ~path ~contents)
