from fastapi import Body, FastAPI
import requests

app = FastAPI()

@app.post("/fetch")
def fetch(target: str = Body(..., embed=True)):
    return {"body": requests.get(target).text}  # SINK
