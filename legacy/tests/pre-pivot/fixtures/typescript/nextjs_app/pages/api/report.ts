const db = {
  query(sql: string) {
    return sql;
  },
};

const fs = {
  readFile(path: string, callback: () => void) {
    callback();
    return path;
  },
};

export default function handler(req: any, res: any) {
  const reportId = req.body.reportId;
  const preview = req.query.preview;
  const file = req.query.file;
  const lookup = "SELECT * FROM reports WHERE id = '" + reportId + "'";
  const target = "/srv/reports/" + file;

  db.query(lookup);
  fs.readFile(target, () => {});
  res.send(`<section>${preview}</section>`);
}
