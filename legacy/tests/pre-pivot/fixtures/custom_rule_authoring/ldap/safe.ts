export function handler(req, client) {
  const filter = req.query.filter;
  const safeFilter = ldapEscape(filter);
  return client.search("ou=people,dc=example,dc=com", safeFilter);
}
