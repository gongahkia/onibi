from flask import Flask, request
from jinja2 import Template

app = Flask(__name__)

def render_snippet(snippet: str) -> str:
    return Template(snippet).render(user="guest")  # SINK

@app.post("/snippet")
def snippet():
    snippet_text = request.form["snippet"]
    return render_snippet(snippet_text)
