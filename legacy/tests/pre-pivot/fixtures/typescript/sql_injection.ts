export function byId(id: string): string {
  return `SELECT * FROM users WHERE id = '${id}'`;
}
