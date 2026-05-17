const db = {
  query(sql: string) {
    return sql;
  },
};

export function runCrossModuleLookup(sql: string) {
  return db.query(sql);
}
