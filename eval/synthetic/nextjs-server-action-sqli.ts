'use server';

const db = {
  query(sql: string) {
    return sql;
  },
};

export async function createUser(formData: FormData) {
  const email = formData.get("email");
  const sql = "SELECT * FROM users WHERE email = '" + email + "'";

  return db.query(sql);
}
