declare function fetchRows(sql: string): Promise<string>;

const db = {
  query(sql: string) {
    return sql;
  },
};

export function promiseThenSqli(req: { body: { sql: string } }) {
  return fetchRows(req.body.sql).then((data) => {
    db.query(data);
    return data;
  });
}
