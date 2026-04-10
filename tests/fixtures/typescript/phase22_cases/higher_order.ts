const db = {
  query(sql: string) {
    return sql;
  },
};

export function higherOrder(req: { body: { items: string[] } }) {
  const items = req.body.items;
  // @piranesi-expect: CWE-89, source=req.body.items, sink=db.query
  return items.map((item) => {
    db.query(item);
    return item;
  });
}
