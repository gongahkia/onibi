from fastapi import Depends, FastAPI, Query
import sqlite3

app = FastAPI()

def get_order(order_by: str = Query("id")) -> str:
    return order_by

@app.get("/audit")
def audit(order_clause: str = Depends(get_order)):
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute(f"SELECT * FROM audit_log ORDER BY {order_clause}")  # SINK
    return {"rows": cursor.fetchall()}
