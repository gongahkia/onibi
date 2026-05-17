const db = {
  query(sql: string) {
    return sql;
  },
};

async function identity(value: string) {
  return Promise.resolve(value);
}

export async function awaitChain(req: { body: { sql: string } }) {
  const data = await identity(req.body.sql);
  // @piranesi-expect: CWE-89, source=req.body.sql, sink=db.query
  return db.query(data);
}
