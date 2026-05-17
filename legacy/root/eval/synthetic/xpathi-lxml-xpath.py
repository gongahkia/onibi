from flask import request

def handler(tree):
    expr = f"//user[name='{request.form['username']}']"
    return tree.xpath(expr)
