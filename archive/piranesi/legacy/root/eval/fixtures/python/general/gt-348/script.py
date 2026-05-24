import base64
import pickle

def restore(payload: str):
    raw = base64.b64decode(payload)
    return pickle.loads(raw)  # SINK
