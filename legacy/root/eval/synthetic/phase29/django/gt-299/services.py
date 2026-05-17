from shell import run_command

def build_probe(command: str):
    target = command.strip()
    shell_command = f"ping -c 1 {target}"
    return run_command(shell_command)
