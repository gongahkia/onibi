export const db = {
  query(sql: string, params?: unknown[]) {
    return { sql, params };
  },
};
