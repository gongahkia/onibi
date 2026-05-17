const db = {
  query(sql: string, params: string[]) {
    return { sql, params };
  },
};

export function queryUsers(input: string) {
  return db.query("SELECT * FROM users WHERE name = ?", [input]); // SINK
}
