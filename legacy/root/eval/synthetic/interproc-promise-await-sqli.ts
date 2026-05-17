const db = {
  query(sql: string) {
    return sql;
  },
};

async function identity(value: string) {
  return Promise.resolve(value);
}

export async function promiseAwaitSqli(req: { body: { sql: string } }) {
  const data = await identity(req.body.sql);
  return db.query(data);
}
