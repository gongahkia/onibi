const db = {
  async query(_sql: string) {
    return [{ email: "victim@example.com", reset_token: "secret-token" }];
  },
};

export async function forgotPassword(req: { query: { email: string } }) {
  const email = req.query.email;
  const rows = await db.query(`SELECT email, reset_token FROM users WHERE email = '${email}'`); // sink
  return rows[0];
}
