from flask import Flask, request
import subprocess

app = Flask(__name__)

def archive(path: str) -> str:
    result = subprocess.run(f"tar -cf backup.tar {path}", shell=True, capture_output=True)  # SINK
    return result.stdout.decode()

@app.post("/archive")
def archive_view():
    path = request.form["path"]
    return archive(path)
