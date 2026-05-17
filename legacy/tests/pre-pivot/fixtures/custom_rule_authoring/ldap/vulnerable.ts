export function handler(req, client) {
  const filter = req.query.filter;
  return client.search("ou=people,dc=example,dc=com", filter);
}
