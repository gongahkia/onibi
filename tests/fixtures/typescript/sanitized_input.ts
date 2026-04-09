export function safe(input: string): string {
  return input.replace(/[<>]/g, "");
}
