from flask import Flask, request
import requests

app = Flask(__name__)

def normalize_target(user_url: str) -> str:
    return user_url.strip()

@app.post("/mirror")
def mirror():
    user_url = request.json["target"]
    target = normalize_target(user_url)
    return requests.get(target).text  # SINK
