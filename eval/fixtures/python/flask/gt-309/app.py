from flask import Flask, request
import sqlite3

app = Flask(__name__)

def build_clause(sort_key: str) -> str:
    return f"ORDER BY {sort_key}"

@app.route("/reports")
def reports():
    sort_key = request.args.get("sort", "id")
    clause = build_clause(sort_key)
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute(f"SELECT * FROM audit_log {clause}")  # SINK
    return str(cursor.fetchall())
