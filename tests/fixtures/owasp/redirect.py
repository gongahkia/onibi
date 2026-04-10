from flask import Flask, redirect, request

app = Flask(__name__)


@app.route("/login/callback")
def login_callback():
    return_url = request.args.get("return_url")  # tainted
    return redirect(return_url)  # CWE-601 sink


@app.route("/goto")
def goto():
    url = request.args.get("url")  # tainted
    return redirect(url)  # CWE-601 sink


@app.route("/auth/next", methods=["POST"])
def auth_next():
    next_url = request.form["next"]  # tainted
    return redirect(next_url)  # CWE-601 sink


# SAFE: startswith check for relative path
@app.route("/safe-redirect")
def safe_redirect():
    return_url = request.args.get("return_url", "/")
    if not return_url.startswith("/"):  # sanitizer
        return "invalid redirect", 400
    return redirect(return_url)


# SAFE: hardcoded redirect
@app.route("/home")
def home():
    return redirect("/dashboard")
