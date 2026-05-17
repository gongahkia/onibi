import os

def build_command(path: str) -> str:
    return f"ls {path}"

def show(path: str):
    command = build_command(path)
    return os.system(command)  # SINK
