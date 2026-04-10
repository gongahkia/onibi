import _ from "lodash";

export function handler(req: any): void {
  const root = req.body.root;
  const payload: Record<string, any> = {};
  payload[root]["prototype"] = req.body.value;
  _.merge({}, payload);
}
