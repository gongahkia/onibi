const db = {
  query(sql: string) {
    return sql;
  },
};

export class UserRepository {
  findByName(name: string) {
    const sql = `SELECT * FROM users WHERE name = '${name}'`;
    return db.query(sql); // SINK
  }
}
