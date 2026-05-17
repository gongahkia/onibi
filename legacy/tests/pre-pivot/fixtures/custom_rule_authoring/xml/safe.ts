export function handler(req) {
  const xml = req.body.xml;
  return safeXmlParse(xml);
}
