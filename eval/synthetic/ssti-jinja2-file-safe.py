from flask import request
from jinja2 import Environment, FileSystemLoader

env = Environment(loader=FileSystemLoader("templates"))
tmpl = env.get_template("page.html")
result = tmpl.render(name=request.form["name"])
