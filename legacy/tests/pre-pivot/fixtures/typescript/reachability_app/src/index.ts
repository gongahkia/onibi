export function publicApi(input) {
  return exportedQuery(input);
}

function exportedQuery(input) {
  return db.query("SELECT * FROM exports WHERE name = '" + input + "'");
}
