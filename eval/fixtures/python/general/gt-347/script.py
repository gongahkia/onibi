import pickle

def load_blob(blob: bytes):
    return pickle.loads(blob)  # SINK
