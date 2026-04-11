const db = {
  query(sql: string) {
    return sql;
  },
};

export function queryUsers(input: string) {
  const sql = `SELECT * FROM users WHERE name LIKE '${input}'`;
  return db.query(sql); // SINK
}
