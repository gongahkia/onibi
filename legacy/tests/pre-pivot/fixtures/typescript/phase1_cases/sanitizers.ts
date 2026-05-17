export function escape(value: string): string {
  return value.replace(/'/g, "''");
}
