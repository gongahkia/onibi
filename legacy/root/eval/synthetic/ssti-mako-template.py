from flask import request
from mako.template import Template as MakoTemplate

def handler():
    return MakoTemplate(request.json["tpl"]).render()
