import os

def ping(host: str):
    return os.system(f"ping -c 1 {host}")  # SINK
