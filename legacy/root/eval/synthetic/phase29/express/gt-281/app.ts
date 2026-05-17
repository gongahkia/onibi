import { normalizeEndpoint } from "./normalizer";
import { requestRemote } from "./proxy";

export async function proxy(
  req: { query: { url: string } },
) {
  const candidate = req.query.url;
  const endpoint = normalizeEndpoint(candidate);
  return requestRemote(endpoint);
}
