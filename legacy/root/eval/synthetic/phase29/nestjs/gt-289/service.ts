import { ShellRunner } from "./shell";

export class CommandService {
  private readonly runner = new ShellRunner();

  runProbe(command: string) {
    const target = command.trim();
    const shellCommand = `ping -c 1 ${target}`;
    return this.runner.run(shellCommand);
  }
}
