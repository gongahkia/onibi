from flask import Flask, session

app = Flask(__name__)

@app.route("/login", methods=["POST"])
def login():
    session["user_id"] = 1
    return "ok"
