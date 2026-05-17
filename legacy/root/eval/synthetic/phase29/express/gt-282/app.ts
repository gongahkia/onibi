import { loadSort, saveSort } from "./store";

const db = {
  query(sql: string) {
    return sql;
  },
};

export function updatePreference(
  req: { params: { id: string }; body: { sort: string } },
) {
  const raw = req.body.sort;
  const persisted = raw.trim();
  saveSort(req.params.id, persisted);
}

export function runReport(req: { params: { id: string } }) {
  const clause = loadSort(req.params.id);
  const sql = `SELECT * FROM invoices ORDER BY ${clause}`;
  return db.query(sql); // SINK
}
