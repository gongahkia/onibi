from flask import Flask, request
from jinja2 import Environment

app = Flask(__name__)
env = Environment()

@app.post("/email")
def email():
    body = request.json["body"]
    template = env.from_string(body)  # SINK
    return template.render(user="guest")
