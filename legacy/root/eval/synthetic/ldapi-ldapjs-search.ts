export function handler(req, client) {
  const filter = `(&(uid=${req.body.username})(objectClass=person))`;
  return client.search("dc=example,dc=com", { filter }, () => {});
}
