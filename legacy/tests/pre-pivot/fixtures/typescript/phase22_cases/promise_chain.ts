declare function fetchRows(sql: string): Promise<string>;

const db = {
  query(sql: string) {
    return sql;
  },
};

export function promiseChain(req: { body: { sql: string } }) {
  // @piranesi-expect: CWE-89, source=req.body.sql, sink=db.query
  return fetchRows(req.body.sql).then((data) => {
    db.query(data);
    return data;
  });
}
