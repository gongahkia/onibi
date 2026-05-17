declare function authenticate(user: string, password: string): boolean;
declare function loadRole(user: string): string;

export function login(req: { body: { user: string; password: string }; session: Record<string, string> }) {
  const { user, password } = req.body;
  if (authenticate(user, password)) {
    req.session.user = user; // sink
    req.session.role = loadRole(user);
    return { ok: true };
  }
  return { ok: false };
}
