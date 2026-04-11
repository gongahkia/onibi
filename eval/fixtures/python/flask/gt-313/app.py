from flask import Flask, request
from jinja2 import Template

app = Flask(__name__)

@app.route("/render")
def render():
    template_src = request.args.get("template", "")
    return Template(template_src).render()  # SINK
