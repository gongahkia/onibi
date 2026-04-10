'use server';

const db = {
  query(sql: string) {
    return sql;
  },
};

export async function searchOrders(formData: FormData) {
  const sort = formData.get("sort");
  const sql = "SELECT * FROM orders ORDER BY " + sort;

  return db.query(sql);
}
