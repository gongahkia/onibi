function directSqlFlow(req, db) {
  const userId = req.body.user;
  const queryText = "SELECT * FROM users WHERE id = '" + userId + "'";
  db.query(queryText);
}

function sanitizedSqlFlow(req, db) {
  const userId = req.body.user;
  const safeUserId = escape(userId);
  const queryText = "SELECT * FROM users WHERE id = '" + safeUserId + "'";
  db.query(queryText);
}

function parameterizedSqlFlow(req, db) {
  const userId = req.body.user;
  const queryText = parameterize(userId);
  db.query(queryText);
}

function directCommandFlow(req, child) {
  const cmd = req.query.cmd;
  child.exec(cmd);
}

function normalizedPathFlow(req, fs) {
  const filePath = normalize(req.params.file);
  fs.readFile(filePath);
}
