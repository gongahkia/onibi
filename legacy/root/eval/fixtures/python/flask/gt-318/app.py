from flask import Flask, request
import subprocess

app = Flask(__name__)

def inspect_iface(iface: str) -> str:
    result = subprocess.run(f"ifconfig {iface}", shell=True, capture_output=True)  # SINK
    return result.stdout.decode()

@app.route("/diag")
def diag():
    iface = request.headers.get("X-Interface", "")
    return inspect_iface(iface)
