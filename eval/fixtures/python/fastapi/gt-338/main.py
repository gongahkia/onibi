from fastapi import Body, FastAPI
import sqlite3

app = FastAPI()

@app.post("/reports")
def reports(payload: dict = Body(...)):
    order_by = payload["sort"]
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute(f"SELECT * FROM reports ORDER BY {order_by}")  # SINK
    return {"rows": cursor.fetchall()}
