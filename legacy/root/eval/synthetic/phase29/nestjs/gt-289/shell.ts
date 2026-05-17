function exec(command: string) {
  return command;
}

export class ShellRunner {
  run(command: string) {
    return exec(command); // SINK
  }
}
