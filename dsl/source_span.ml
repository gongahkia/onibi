type t = { start_offset : int; stop_offset : int }

let make ~start_offset ~stop_offset =
  {
    start_offset = max 0 start_offset;
    stop_offset = max start_offset stop_offset;
  }

let start_offset value = value.start_offset
let stop_offset value = value.stop_offset

let join left right =
  make
    ~start_offset:(min left.start_offset right.start_offset)
    ~stop_offset:(max left.stop_offset right.stop_offset)

let whole source = make ~start_offset:0 ~stop_offset:(String.length source)
