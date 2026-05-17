'use server';

const db = {
  query(sql: string) {
    return sql;
  },
};

export async function submitOrder(formData: FormData) {
  const orderId = formData.get("id");
  const lookup = "SELECT * FROM orders WHERE id = '" + orderId + "'";

  db.query(lookup);
}
