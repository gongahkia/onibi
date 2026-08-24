type placement = Before | After

type item =
  | Inline of { anchor_offset : int; text : string }
  | Virtual_line of { anchor_offset : int; placement : placement; text : string }

type contribution = {
  provider_id : string;
  priority : int;
  document_id : string;
  document_version : int;
  items : item list;
}

type response = (contribution, string) result

type resolved = {
  provider_id : string;
  item : item;
}

type collection = { items : resolved list; rejections : string list }

let maximum_providers = 32
let maximum_items_per_provider = 64
let maximum_items = 256
let maximum_text_bytes = 512
let maximum_total_text_bytes = 16 * 1024
let maximum_provider_id_bytes = 96
let maximum_priority = 1024

let item_anchor = function
  | Inline { anchor_offset; _ }
  | Virtual_line { anchor_offset; _ } ->
      anchor_offset

let item_text = function
  | Inline { text; _ } | Virtual_line { text; _ } -> text

let valid_text text =
  String.length text > 0
  && String.length text <= maximum_text_bytes
  && String.is_valid_utf_8 text
  && not (String.exists (fun character -> character = '\n' || character = '\r') text)

let valid_provider_id value =
  String.length value > 0
  && String.length value <= maximum_provider_id_bytes
  && String.is_valid_utf_8 value
  && not
       (String.exists
          (fun character ->
            let code = Char.code character in
            code < 32 || code = 127)
          value)

let create ~provider_id ~priority ~document_id ~document_version ~items =
  if not (valid_provider_id provider_id) then
    Error "provider id must be nonempty valid UTF-8 without controls"
  else if document_id = "" || not (String.is_valid_utf_8 document_id) then
    Error "document id must be nonempty valid UTF-8"
  else if document_version < 0 then Error "document version must be nonnegative"
  else if abs priority > maximum_priority then
    Error "provider priority exceeds the bounded range"
  else if List.length items > maximum_items_per_provider then
    Error "provider contribution exceeds the item limit"
  else if
    List.exists
      (fun item -> item_anchor item < 0 || not (valid_text (item_text item)))
      items
  then Error "decoration item has an invalid anchor or text payload"
  else Ok { provider_id; priority; document_id; document_version; items }

let utf8_boundary contents offset =
  offset = 0 || offset = String.length contents
  ||
  let decoded = String.get_utf_8_uchar contents offset in
  Uchar.utf_decode_is_valid decoded

let contribution_compare (left : contribution) (right : contribution) =
  match Int.compare left.priority right.priority with
  | 0 -> String.compare left.provider_id right.provider_id
  | result -> result

let collect ~contents ~document_id ~document_version responses =
  let accepted, rejections =
    List.fold_left
      (fun (accepted, rejections) -> function
        | Error reason -> (accepted, ("provider failure: " ^ reason) :: rejections)
        | Ok contribution
          when contribution.document_id <> document_id
               || contribution.document_version <> document_version ->
            ( accepted,
              ("provider " ^ contribution.provider_id
             ^ " rejected: stale document snapshot")
              :: rejections )
        | Ok contribution -> (contribution :: accepted, rejections))
      ([], []) responses
  in
  let accepted = List.rev accepted |> List.sort contribution_compare in
  let duplicate_ids =
    accepted
    |> List.filter_map (fun (contribution : contribution) ->
           let same =
             List.length
               (List.filter
                  (fun (candidate : contribution) ->
                    String.equal candidate.provider_id contribution.provider_id)
                  accepted)
           in
           if same > 1 then Some contribution.provider_id else None)
    |> List.sort_uniq String.compare
  in
  let accepted, rejections =
    List.fold_left
      (fun (accepted, rejections) (contribution : contribution) ->
        if List.mem contribution.provider_id duplicate_ids then
          ( accepted,
            ("provider " ^ contribution.provider_id
           ^ " rejected: duplicate snapshot contribution")
            :: rejections )
        else (contribution :: accepted, rejections))
      ([], rejections) accepted
  in
  let accepted = List.rev accepted in
  let accepted, rejections =
    List.mapi (fun index (contribution : contribution) -> (index, contribution)) accepted
    |> List.fold_left
         (fun (accepted, rejections) (index, (contribution : contribution)) ->
           if index >= maximum_providers then
             ( accepted,
               ("provider " ^ contribution.provider_id
              ^ " rejected: provider limit reached")
               :: rejections )
           else (contribution :: accepted, rejections))
         ([], rejections)
  in
  let accepted = List.rev accepted in
  let resolved, rejections, _, _ =
    List.fold_left
      (fun (resolved, rejections, item_count, text_bytes)
           (contribution : contribution) ->
        contribution.items
        |> List.mapi (fun _ordinal item -> item)
        |> List.fold_left
             (fun (resolved, rejections, item_count, text_bytes) item ->
               let next_item_count = item_count + 1 in
               let next_text_bytes = text_bytes + String.length (item_text item) in
               let anchor = item_anchor item in
               if next_item_count > maximum_items then
                 ( resolved,
                   ("provider " ^ contribution.provider_id
                  ^ " rejected: session item limit reached")
                   :: rejections,
                   item_count,
                   text_bytes )
               else if next_text_bytes > maximum_total_text_bytes then
                 ( resolved,
                   ("provider " ^ contribution.provider_id
                  ^ " rejected: session text limit reached")
                   :: rejections,
                   item_count,
                   text_bytes )
               else if
                 anchor > String.length contents || not (utf8_boundary contents anchor)
               then
                 ( resolved,
                   ("provider " ^ contribution.provider_id
                  ^ " rejected: anchor is not a source UTF-8 boundary")
                   :: rejections,
                   item_count,
                   text_bytes )
               else
                 ( { provider_id = contribution.provider_id;
                     item }
                   :: resolved,
                   rejections,
                   next_item_count,
                   next_text_bytes ))
             (resolved, rejections, item_count, text_bytes))
      ([], rejections, 0, 0) accepted
  in
  { items = List.rev resolved; rejections = List.rev rejections }

let items value = value.items
let rejections value = value.rejections
let provider_id value = value.provider_id
let item value = value.item

let inspection_lines value =
  let providers =
    value.items
    |> List.map provider_id |> List.sort_uniq String.compare
  in
  [
    "Display decorations";
    "providers: " ^ string_of_int (List.length providers);
    "items: " ^ string_of_int (List.length value.items);
    "rejections: " ^ string_of_int (List.length value.rejections);
  ]
  @ List.map (fun reason -> "rejected: " ^ reason) value.rejections
