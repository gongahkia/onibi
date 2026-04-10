export function handler(req, xml2js) {
  const xml = req.body.xml;
  return xml2js.parseString(xml, () => {});
}
