import os
import shlex
import sqlite3
import subprocess

import bleach
import requests
from flask import Flask, render_template_string, request
from markupsafe import escape

app = Flask(__name__)


@app.route("/search")
def search():
    q = request.args.get("q")  # tainted url_param
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute(f"SELECT * FROM items WHERE name = '{q}'")  # sqli sink CWE-89
    return cursor.fetchall()


@app.route("/login", methods=["POST"])
def login():
    username = request.form["username"]  # tainted request_body
    return render_template_string(f"<h1>Welcome {username}</h1>")  # xss sink CWE-79


@app.route("/run")
def run_cmd():
    cmd = request.args.get("cmd")  # tainted url_param
    os.system(cmd)  # cmdi sink CWE-78
    return "done"


@app.route("/exec")
def exec_cmd():
    cmd = request.args.get("cmd")  # tainted url_param
    subprocess.run(cmd, shell=True)  # cmdi sink CWE-78
    return "done"


@app.route("/safe_exec")
def safe_exec_cmd():
    cmd = request.args.get("cmd")
    subprocess.run(["echo", cmd], shell=False)
    return "done"


@app.route("/read")
def read_file():
    path = request.args.get("path")  # tainted url_param
    return open(path).read()  # path traversal CWE-22


@app.route("/eval")
def eval_input():
    expr = request.args.get("expr")  # tainted url_param
    return str(eval(expr))  # eval sink CWE-94


@app.route("/proxy")
def proxy():
    url = request.args.get("url")  # tainted url_param
    return requests.get(url).text  # ssrf CWE-918


@app.route("/safe_search")
def safe_search():
    q = request.args.get("q")
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM items WHERE name = ?", (q,))  # parameterized - safe
    return cursor.fetchall()


@app.route("/safe_cmd")
def safe_cmd():
    cmd = request.args.get("cmd")
    os.system(shlex.quote(cmd))  # shlex.quote sanitizer


@app.route("/safe_html")
def safe_html():
    name = request.args.get("name")
    return render_template_string(f"<h1>Hello {escape(name)}</h1>")  # markupsafe.escape


@app.route("/safe_html2")
def safe_html2():
    name = request.args.get("name")
    return bleach.clean(name)  # bleach.clean sanitizer


@app.route("/safe_path")
def safe_path():
    path = request.args.get("path")
    real = os.path.realpath(path)
    if not real.startswith("/safe/"):
        return "denied", 403
    return open(real).read()


@app.route("/header_echo")
def header_echo():
    host = request.headers.get("X-Forwarded-Host")  # tainted header
    return render_template_string(f"<p>Host: {host}</p>")  # xss via header
