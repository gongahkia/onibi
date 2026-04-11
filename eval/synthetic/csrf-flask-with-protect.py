from flask import Flask
from flask_wtf.csrf import CSRFProtect

app = Flask(__name__)
CSRFProtect(app)

@app.route("/transfer", methods=["POST"])
def transfer():
    return "ok"
