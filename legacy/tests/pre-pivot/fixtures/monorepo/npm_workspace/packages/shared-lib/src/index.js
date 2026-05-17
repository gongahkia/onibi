export function runQuery(input) {
  const sql = `SELECT * FROM users WHERE id = '${input}'`;
  return db.query(sql);
}
