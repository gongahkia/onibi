from fastapi import Body, FastAPI
from pydantic import BaseModel
import sqlite3

app = FastAPI()

class SearchRequest(BaseModel):
    term: str

    class Config:
        extra = "allow"

@app.post("/items")
def items(payload: SearchRequest = Body(...)):
    order_by = payload.__dict__.get("order_by", "id")
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute(f"SELECT * FROM items ORDER BY {order_by}")  # SINK
    return {"rows": cursor.fetchall()}
