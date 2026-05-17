const db = {
  query(sql: string, params: string[]) {
    return { sql, params };
  },
};

export class UserRepository {
  findByName(name: string) {
    return db.query("SELECT * FROM users WHERE name = ?", [name]); // SINK
  }
}
