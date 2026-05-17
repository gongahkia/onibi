import marshal
import pickle

import yaml
from flask import Flask, request

app = Flask(__name__)


@app.route("/api/load", methods=["POST"])
def load_pickle():
    data = request.get_data()  # tainted
    obj = pickle.loads(data)  # CWE-502 sink - critical
    return str(obj)


@app.route("/api/yaml", methods=["POST"])
def load_yaml():
    raw = request.get_data(as_text=True)  # tainted
    parsed = yaml.load(raw, Loader=yaml.Loader)  # CWE-502 sink - unsafe loader
    return str(parsed)


@app.route("/api/marshal", methods=["POST"])
def load_marshal():
    data = request.get_data()  # tainted
    obj = marshal.loads(data)  # CWE-502 sink
    return str(obj)


# SAFE: yaml.safe_load
@app.route("/api/safe-yaml", methods=["POST"])
def safe_load_yaml():
    raw = request.get_data(as_text=True)
    parsed = yaml.safe_load(raw)  # safe loader - sanitizer
    return str(parsed)


# SAFE: pickle with HMAC verification (not modeled, but no taint from user)
@app.route("/api/safe-pickle")
def safe_pickle():
    with open("trusted_data.pkl", "rb") as f:
        obj = pickle.load(f)  # not from user input
    return str(obj)
