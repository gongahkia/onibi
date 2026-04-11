function exec(command: string) {
  return command;
}

export function runShell(command: string) {
  return exec(command); // SINK
}
