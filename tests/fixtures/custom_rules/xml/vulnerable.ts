function parseXml(payload: string) {
  return payload;
}

export function importFeed(req: { body: { xml: string } }) {
  return parseXml(req.body.xml);
}
