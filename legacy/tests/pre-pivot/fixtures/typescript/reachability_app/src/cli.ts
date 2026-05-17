const arg = process.argv[2];

runCli(arg);

function runCli(input) {
  return cliQuery(input);
}

function cliQuery(input) {
  return db.query("SELECT * FROM cli WHERE name = '" + input + "'");
}
