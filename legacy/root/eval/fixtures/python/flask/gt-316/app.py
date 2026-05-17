from flask import Flask, request
import subprocess

app = Flask(__name__)

@app.route("/ping")
def ping():
    host = request.args.get("host", "")
    result = subprocess.run(f"ping -c 1 {host}", shell=True, capture_output=True)  # SINK
    return result.stdout.decode()
