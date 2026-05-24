const db = {
  query(sql: string) {
    return sql;
  },
};

export class ReportRepository {
  run(orderClause: string) {
    const sql = `SELECT * FROM invoices ORDER BY ${orderClause}`;
    return db.query(sql); // SINK
  }
}
