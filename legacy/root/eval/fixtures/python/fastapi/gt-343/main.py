from fastapi import Body, FastAPI
from pydantic import BaseModel
import subprocess

app = FastAPI()

class JobRequest(BaseModel):
    name: str

    class Config:
        extra = "allow"

@app.post("/jobs")
def jobs(payload: JobRequest = Body(...)):
    command = payload.__dict__.get("command", "true")
    subprocess.run(f"{command}", shell=True, capture_output=True)  # SINK
    return {"queued": payload.name}
