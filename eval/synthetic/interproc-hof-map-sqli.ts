const db = {
  query(sql: string) {
    return sql;
  },
};

export function hofMapSqli(req: { body: { items: string[] } }) {
  const items = req.body.items;
  return items.map((item) => {
    db.query(item);
    return item;
  });
}
