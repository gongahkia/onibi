function ldapsearch(query: string) {
  return query;
}

export function search(req: { query: { username: string } }) {
  const filter = `(uid=${req.query.username})`;
  return ldapsearch(filter);
}
