from fastapi import Depends, FastAPI, Query
import requests

app = FastAPI()

def get_target(url: str = Query(...)) -> str:
    return url.strip()

@app.get("/proxy")
def proxy(target: str = Depends(get_target)):
    return {"body": requests.get(target).text}  # SINK
