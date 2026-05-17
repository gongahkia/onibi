from fastapi import Body, FastAPI
from pydantic import BaseModel
import requests

app = FastAPI()

class ProxyRequest(BaseModel):
    route: str

    class Config:
        extra = "allow"

@app.post("/proxy")
def proxy(payload: ProxyRequest = Body(...)):
    user_url = payload.__dict__.get("url", payload.route)
    return {"body": requests.get(user_url).text}  # SINK
