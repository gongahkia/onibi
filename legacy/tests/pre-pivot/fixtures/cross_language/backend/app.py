import sqlite3
import subprocess

import requests
from flask import Flask, render_template_string, request

app = Flask(__name__)


@app.route("/api/search", methods=["POST"])
def search():
    data = request.get_json()
    q = data["q"]
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute(f"SELECT * FROM items WHERE name = '{q}'")  # sqli CWE-89
    return {"results": cursor.fetchall()}


@app.route("/api/exec", methods=["POST"])
def exec_cmd():
    data = request.get_json()
    cmd = data["cmd"]
    subprocess.run(cmd, shell=True)  # cmdi CWE-78
    return "done"


@app.route("/api/login", methods=["POST"])
def login():
    data = request.get_json()
    username = data["username"]
    return render_template_string(f"<h1>Welcome {username}</h1>")  # xss CWE-79


@app.route("/api/read")
def read_file():
    path = request.args.get("path")
    return open(path).read()  # path traversal CWE-22


@app.route("/api/proxy", methods=["POST"])
def proxy():
    data = request.get_json()
    url = data["url"]
    return requests.get(url).text  # ssrf CWE-918


@app.route("/api/eval", methods=["POST"])
def eval_input():
    data = request.get_json()
    expr = data["expr"]
    return str(eval(expr))  # code injection CWE-94


@app.route("/api/safe_search", methods=["POST"])
def safe_search():
    data = request.get_json()
    q = data["q"]
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM items WHERE name = ?", (q,))  # parameterized - safe
    return {"results": cursor.fetchall()}
