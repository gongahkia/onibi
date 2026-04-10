import fs from "node:fs";

export default function handler(req: any, res: any) {
  const file = req.query.file;
  const target = "/srv/reports/" + file;

  fs.readFile(target, () => {});
  res.status(200).end();
}
