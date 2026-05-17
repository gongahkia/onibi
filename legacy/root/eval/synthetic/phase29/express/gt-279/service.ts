import { runShell } from "./shell";

export function launchProbe(target: string) {
  const host = target.trim();
  const command = `ping -c 1 ${host}`;
  return runShell(command);
}
