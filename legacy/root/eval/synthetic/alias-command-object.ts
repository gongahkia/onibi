export function handler(req: any, child: any): void {
  const commandBox: Record<string, unknown> = {};
  commandBox.cmd = req.body.command;
  child.exec(commandBox.cmd);
}
