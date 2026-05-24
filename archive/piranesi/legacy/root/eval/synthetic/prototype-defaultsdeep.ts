import lodash from "lodash";

export function handler(req: any): void {
  const payload = req.body;
  lodash.defaultsDeep({}, payload);
}
