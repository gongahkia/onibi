from flask import request
from jinja2.sandbox import SandboxedEnvironment

env = SandboxedEnvironment()
tpl = env.from_string(request.form["tpl"])
