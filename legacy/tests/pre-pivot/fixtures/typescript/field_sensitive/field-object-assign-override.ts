declare function useAdmin(flag: boolean): void;

export function handler(req: any): void {
  const cfg = Object.assign({}, req.body, { admin: false });
  useAdmin(cfg.admin);
}
