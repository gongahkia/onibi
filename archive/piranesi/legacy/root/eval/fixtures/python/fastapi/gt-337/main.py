from fastapi import Body, FastAPI
import sqlite3

app = FastAPI()

@app.post("/search")
def search(term: str = Body(..., embed=True)):
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute(f"SELECT * FROM items WHERE name LIKE '%{term}%'")  # SINK
    return {"rows": cursor.fetchall()}
