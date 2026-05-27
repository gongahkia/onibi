/**
 * Accepts either:
 *   - JSON payload: {"type":"onibi_pairing","baseURL":"...","token":"..."}
 *   - Deep link:   onibi://pair?base=<urlencoded>&token=<urlencoded>
 * Returns null if neither form parses cleanly.
 */
export function parsePairingPayload(raw: string): { baseURL: string; token: string } | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;

  try {
    const parsed = JSON.parse(trimmed);
    if (
      parsed &&
      typeof parsed === "object" &&
      typeof parsed.baseURL === "string" &&
      typeof parsed.token === "string"
    ) {
      return { baseURL: parsed.baseURL.trim(), token: parsed.token.replace(/\s+/g, "") };
    }
  } catch {
    // not JSON, fall through
  }

  if (trimmed.startsWith("onibi://")) {
    try {
      const url = new URL(trimmed);
      const base = url.searchParams.get("base");
      const token = url.searchParams.get("token");
      if (base && token) {
        return { baseURL: base.trim(), token: token.replace(/\s+/g, "") };
      }
    } catch {
      return null;
    }
  }

  return null;
}
