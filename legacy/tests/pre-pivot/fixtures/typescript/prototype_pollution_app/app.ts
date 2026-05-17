import _ from "lodash";
import lodash from "lodash";

function merge(target: Record<string, any>, source: Record<string, any>): Record<string, any> {
  for (const key in source) {
    if (source[key] && typeof source[key] === "object") {
      target[key] = target[key] || {};
      merge(target[key], source[key]);
    } else {
      target[key] = source[key];
    }
  }
  return target;
}

export function objectAssignPollution(req: any): void {
  const key = req.body.key;
  const payload: Record<string, unknown> = {};
  payload[key] = req.body.value;
  Object.assign({}, payload);
}

export function lodashMergePollution(req: any): void {
  const key = req.body.key;
  const payload: Record<string, unknown> = {};
  payload[key] = req.body.value;
  _.merge({}, payload);
}

export function defaultsDeepPollution(req: any): void {
  const payload = req.body;
  lodash.defaultsDeep({}, payload);
}

export function customMergePollution(req: any): void {
  const key = req.body.key;
  const payload: Record<string, unknown> = {};
  payload[key] = req.body.value;
  merge({}, payload);
}

export function constructorPrototypePollution(req: any): void {
  const root = req.body.root;
  const payload: Record<string, any> = {};
  payload[root]["prototype"] = req.body.value;
  _.merge({}, payload);
}

export function protoTraversal(req: any): void {
  const branch = req.body.branch;
  const payload: Record<string, any> = {};
  payload[branch]["__proto__"] = req.body.value;
  Object.assign({}, payload);
}
