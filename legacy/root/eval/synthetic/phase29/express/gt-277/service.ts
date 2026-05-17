import { queryUsers } from "./repo";

export function buildLookup(input: string) {
  const normalized = input.replace(/\s+/g, " ");
  return queryUsers(normalized);
}
