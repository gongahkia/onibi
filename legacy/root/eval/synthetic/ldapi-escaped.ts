export function handler(req, client, ldap) {
  const escaped = ldap.escape(req.body.user);
  const filter = `(&(uid=${escaped})(objectClass=person))`;
  return client.search("dc=example,dc=com", { filter }, () => {});
}
