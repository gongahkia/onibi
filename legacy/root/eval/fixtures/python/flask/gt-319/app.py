from flask import Flask, request
import requests

app = Flask(__name__)

@app.route("/fetch")
def fetch():
    user_url = request.args.get("url", "")
    return requests.get(user_url).text  # SINK
