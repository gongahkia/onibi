from flask import Flask, request
import requests

app = Flask(__name__)

def load_profile(user_url: str) -> str:
    return requests.get(user_url).text  # SINK

@app.post("/profile")
def profile():
    user_url = request.form["endpoint"]
    return load_profile(user_url)
