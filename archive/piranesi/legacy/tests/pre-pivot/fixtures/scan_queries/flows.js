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

function fullUrlFetchFlow(req) {
  const url = req.query.url;
  fetch(url);
}

function hardcodedBaseTemplateFetchFlow(req) {
  const userId = req.query.userId;
  fetch(`https://internal.service.local/api/users/${userId}`);
}

function hardcodedBaseTemplateAxiosFlow(req, axios) {
  const reportId = req.query.reportId;
  const endpoint = `https://internal.service.local/reports/${reportId}`;
  axios.get(endpoint);
}

function hardcodedBaseTemplateHttpFlow(req, http) {
  const reportId = req.query.reportId;
  const endpoint = `https://internal.service.local/export?report=${reportId}`;
  http.get(endpoint);
}

function hardcodedUrlTaintedBodyFlow(req, axios) {
  axios.post("https://internal.service.local/api/users", req.body.payload);
}
