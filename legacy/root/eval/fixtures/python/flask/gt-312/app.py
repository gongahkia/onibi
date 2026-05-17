from flask import Flask, request, render_template_string

app = Flask(__name__)

def wrap_notice(banner: str) -> str:
    return f"<div class='banner'>{banner}</div>"

@app.route("/notice")
def notice():
    banner = request.headers.get("X-Banner", "")
    page = wrap_notice(banner)
    return render_template_string(page)  # SINK
