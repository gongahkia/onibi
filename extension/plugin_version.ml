type t = { major : int; minor : int; patch : int }

let valid_component value =
  String.length value > 0
  && String.for_all (function '0' .. '9' -> true | _ -> false) value
  && (String.length value = 1 || value.[0] <> '0')

let invalid value =
  Error
    (Zenbu_kernel.Error.Extension_error
       {
         code = Zenbu_kernel.Error.Invalid_manifest;
         plugin_id = None;
         provider = None;
         operation = Some "plugin.version";
         required = None;
         granted = [];
         message =
           "plugin version must use the supported SemVer core \
            MAJOR.MINOR.PATCH form: " ^ value;
       })

let of_string value =
  match String.split_on_char '.' value with
  | [ major; minor; patch ]
    when valid_component major && valid_component minor && valid_component patch
    -> (
      try
        Ok
          {
            major = int_of_string major;
            minor = int_of_string minor;
            patch = int_of_string patch;
          }
      with Failure _ -> invalid value)
  | _ -> invalid value

let to_string value =
  Printf.sprintf "%d.%d.%d" value.major value.minor value.patch

let compare left right =
  match Int.compare left.major right.major with
  | 0 -> (
      match Int.compare left.minor right.minor with
      | 0 -> Int.compare left.patch right.patch
      | comparison -> comparison)
  | comparison -> comparison
