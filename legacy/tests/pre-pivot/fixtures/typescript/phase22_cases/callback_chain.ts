declare function fetchRows(
  sql: string,
  cb: (err: Error | null, data: string) => void,
): void;

const db = {
  query(sql: string) {
    return sql;
  },
};

export function callbackChain(req: { body: { sql: string } }) {
  // @piranesi-expect: CWE-89, source=req.body.sql, sink=db.query
  fetchRows(req.body.sql, (_err, data) => {
    db.query(data);
  });
}
