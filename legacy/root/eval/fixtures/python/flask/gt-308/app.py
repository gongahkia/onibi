from flask import Flask, request
import sqlite3

app = Flask(__name__)

def run_lookup(email: str):
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute(f"SELECT * FROM customers WHERE email = '{email}'")  # SINK
    return cursor.fetchall()

@app.post("/lookup")
def lookup():
    email = request.form["email"]
    return str(run_lookup(email.strip()))
