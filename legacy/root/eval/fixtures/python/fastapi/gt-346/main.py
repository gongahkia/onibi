from fastapi import Depends, FastAPI, Query
import subprocess

app = FastAPI()

def build_command(host: str = Query(...)) -> str:
    return f"ping -c 1 {host}"

@app.get("/diag")
def diag(command: str = Depends(build_command)):
    result = subprocess.run(command, shell=True, capture_output=True)  # SINK
    return {"stdout": result.stdout.decode()}
