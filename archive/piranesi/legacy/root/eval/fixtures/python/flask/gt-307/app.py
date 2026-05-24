from flask import Flask, request
import sqlite3

app = Flask(__name__)

@app.route("/users")
def users():
    user_id = request.args.get("id", "")
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute(f"SELECT * FROM users WHERE id = '{user_id}'")  # SINK
    return str(cursor.fetchall())
