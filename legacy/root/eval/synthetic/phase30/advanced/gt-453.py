from flask import request, render_template_string

templates = {}

def save_template():
    templates[request.form["name"]] = request.form["body"]
    return "saved"

def render(name):
    return render_template_string(templates[name])  # sink
