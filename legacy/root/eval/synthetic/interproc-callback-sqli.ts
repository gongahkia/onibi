declare function fetchRows(
  sql: string,
  cb: (err: Error | null, data: string) => void,
): void;

const db = {
  query(sql: string) {
    return sql;
  },
};

export function callbackSqli(req: { body: { sql: string } }) {
  fetchRows(req.body.sql, (_err, data) => {
    db.query(data);
  });
}
