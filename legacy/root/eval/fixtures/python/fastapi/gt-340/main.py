from fastapi import Body, FastAPI
import subprocess

app = FastAPI()

@app.post("/ping")
def ping(host: str = Body(..., embed=True)):
    result = subprocess.run(f"ping -c 1 {host}", shell=True, capture_output=True)  # SINK
    return {"stdout": result.stdout.decode()}
