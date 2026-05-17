from flask import request
from jinja2 import Template

def handler():
    return Template(request.form["tpl"]).render()
